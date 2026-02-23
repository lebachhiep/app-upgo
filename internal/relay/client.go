package relay

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"relay-app/pkg/relayleaf"
)

const (
	batchSize       = 500 // all proxies in a single SDK client (one NATS heartbeat)
	maxFastRestarts = 3  // escalate to full restart after N consecutive fast restarts
)

type Stats struct {
	BytesSent         int64  `json:"bytes_sent"`
	BytesRecv         int64  `json:"bytes_recv"`
	Uptime            int64  `json:"uptime"`
	Connections       int32  `json:"connections"`
	TotalStreams      int64  `json:"total_streams"`
	ReconnectCount    int64  `json:"reconnect_count"`
	ActiveStreams     int32  `json:"active_streams"`
	ConnectedNodes    int32  `json:"connected_nodes"`
	Timestamp         int64  `json:"timestamp"`
	ExitPointsJSON    string `json:"exit_points_json,omitempty"`
	NodeAddressesJSON string `json:"node_addresses_json,omitempty"`
}

type Status struct {
	Connected bool
	DeviceId  string
	Stats     *Stats
	Version   string
}

type RelayManager struct {
	clients         []*relayleaf.Client // parallel SDK clients (one per batch)
	batchProxies    [][]string          // proxy URLs per batch, same index as clients
	running         bool
	partnerId       string
	verbose         bool
	discoveryUrl    string
	proxies         []string // stored proxy URLs for fast restart
	mu              sync.RWMutex
	stopPoll        chan struct{}
	OnStatsUpdate   func(*Stats)
	OnStatusChange  func(bool)
	OnLog           func(string)
	OnLibraryStatus func(status, detail string)
	OnNeedRestart   func()     // called when disconnected too long (SDK backoff stuck)
	OnNetworkStatus func(bool) // called with false when no internet, true when restored
	lastConnected   bool
	cachedDeviceId  string
	disconnectSince     time.Time // when connection was lost (zero = connected)
	lastRestart         time.Time // when last Restart() happened (grace period)
	consecutiveRestarts int       // fast restarts without reconnecting; triggers escalation
}

// LastConnected returns the cached connection status (no DLL call).
func (rm *RelayManager) LastConnected() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.lastConnected
}

// CachedDeviceId returns the cached device ID (no DLL call).
func (rm *RelayManager) CachedDeviceId() string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.cachedDeviceId
}

func NewRelayManager() *RelayManager {
	return &RelayManager{
		stopPoll: make(chan struct{}),
	}
}

func (rm *RelayManager) emitLibStatus(status, detail string) {
	if rm.OnLibraryStatus != nil {
		rm.OnLibraryStatus(status, detail)
	}
}

func (rm *RelayManager) Init(verbose bool) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for _, c := range rm.clients {
		c.Close()
	}
	rm.clients = nil

	// Pre-load DLL by creating a test client
	test, err := relayleaf.NewClient(false)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	test.Close()

	rm.verbose = verbose
	rm.log("BNC node initialized")
	return nil
}

func (rm *RelayManager) SetDiscoveryURL(url string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.discoveryUrl = url
	return nil
}

func (rm *RelayManager) AddProxy(proxyURL string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.proxies = append(rm.proxies, proxyURL)
	return nil
}

func (rm *RelayManager) Start(partnerId string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.running {
		return fmt.Errorf("node already running")
	}

	clients, err := rm.startBatches(partnerId)
	if err != nil {
		return fmt.Errorf("failed to start node: %w", err)
	}

	rm.clients = clients
	rm.running = true
	rm.partnerId = partnerId
	rm.cachedDeviceId = clients[0].GetDeviceID()
	rm.stopPoll = make(chan struct{})
	rm.lastRestart = time.Now() // grace period for initial start
	rm.log(fmt.Sprintf("Node started with partner ID: %s (%d parallel clients)", partnerId, len(clients)))

	go rm.pollStats()

	return nil
}

func (rm *RelayManager) Stop() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.running {
		return fmt.Errorf("node not running")
	}

	close(rm.stopPoll)

	for _, c := range rm.clients {
		if c != nil {
			_ = c.Stop()
		}
	}

	rm.running = false
	rm.log("Node stopped")
	return nil
}

// Restart recreates all SDK clients to reset exponential backoff.
// This is a fast path — reuses stored proxies, no health checks.
func (rm *RelayManager) Restart() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.running {
		return fmt.Errorf("node not running")
	}

	partnerId := rm.partnerId

	// Stop polling and old clients
	close(rm.stopPoll)
	for _, c := range rm.clients {
		if c != nil {
			_ = c.Stop()
			c.Close()
		}
	}
	rm.clients = nil
	rm.running = false

	// Create fresh clients in parallel
	clients, err := rm.startBatches(partnerId)
	if err != nil {
		return fmt.Errorf("restart: %w", err)
	}

	rm.clients = clients
	rm.running = true
	rm.cachedDeviceId = clients[0].GetDeviceID()
	rm.stopPoll = make(chan struct{})
	rm.lastConnected = false
	rm.disconnectSince = time.Time{}
	rm.lastRestart = time.Now()
	rm.consecutiveRestarts++

	rm.log(fmt.Sprintf("Fast restart completed (%d clients, partner=%s, proxies=%d, attempt=%d)", len(clients), partnerId, len(rm.proxies), rm.consecutiveRestarts))

	go rm.pollStats()
	return nil
}

func (rm *RelayManager) Close() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.running {
		close(rm.stopPoll)
		rm.running = false
	}

	for _, c := range rm.clients {
		if c != nil {
			c.Close()
		}
	}
	rm.clients = nil
}

// HotAddProxies adds revived proxies as a NEW batch — existing clients continue
// uninterrupted so exit point detection is not reset for the main batch.
func (rm *RelayManager) HotAddProxies(proxyURLs []string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.running || len(proxyURLs) == 0 {
		return nil
	}

	rm.proxies = append(rm.proxies, proxyURLs...)

	// Always create a separate batch for revived proxies.
	// This avoids stopping/recreating the main batch which would
	// reset exit point detection for all ~100+ existing proxies.
	newBatches := splitBatches(proxyURLs, batchSize)
	for _, batch := range newBatches {
		newClient, err := createBatchClient(rm.verbose, rm.discoveryUrl, rm.partnerId, batch)
		if err != nil {
			rm.log(fmt.Sprintf("HotAddProxies: failed to create batch: %v", err))
			continue
		}
		rm.clients = append(rm.clients, newClient)
		rm.batchProxies = append(rm.batchProxies, batch)
		rm.log(fmt.Sprintf("New batch %d created with %d revived proxies", len(rm.clients)-1, len(batch)))
	}

	return nil
}

// RemoveProxy hot-removes a single proxy, restarting only the affected batch.
// All other SDK clients continue running uninterrupted.
func (rm *RelayManager) RemoveProxy(proxyURL string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.running {
		return nil
	}

	// Find which batch contains this proxy
	batchIdx, proxyIdx := rm.findProxy(proxyURL)
	if batchIdx < 0 {
		rm.log(fmt.Sprintf("RemoveProxy: %s not found in any batch, skipping", proxyURL))
		return nil
	}

	// Remove from flat proxy list
	newProxies := make([]string, 0, len(rm.proxies))
	needle := stripScheme(proxyURL)
	for _, p := range rm.proxies {
		if stripScheme(p) != needle {
			newProxies = append(newProxies, p)
		}
	}
	rm.proxies = newProxies

	// Remove from batch
	batch := make([]string, 0, len(rm.batchProxies[batchIdx])-1)
	batch = append(batch, rm.batchProxies[batchIdx][:proxyIdx]...)
	batch = append(batch, rm.batchProxies[batchIdx][proxyIdx+1:]...)

	// Stop old client for this batch
	if rm.clients[batchIdx] != nil {
		_ = rm.clients[batchIdx].Stop()
		rm.clients[batchIdx].Close()
	}

	if len(batch) > 0 {
		// Recreate client with remaining proxies in the same batch
		newClient, err := createBatchClient(rm.verbose, rm.discoveryUrl, rm.partnerId, batch)
		if err != nil {
			rm.log(fmt.Sprintf("RemoveProxy: failed to recreate batch %d: %v", batchIdx, err))
			rm.clients = append(rm.clients[:batchIdx], rm.clients[batchIdx+1:]...)
			rm.batchProxies = append(rm.batchProxies[:batchIdx], rm.batchProxies[batchIdx+1:]...)
		} else {
			rm.clients[batchIdx] = newClient
			rm.batchProxies[batchIdx] = batch
			rm.log(fmt.Sprintf("Batch %d restarted with %d proxies (removed %s)", batchIdx, len(batch), proxyURL))
		}
	} else {
		// Batch is now empty — remove it
		rm.clients = append(rm.clients[:batchIdx], rm.clients[batchIdx+1:]...)
		rm.batchProxies = append(rm.batchProxies[:batchIdx], rm.batchProxies[batchIdx+1:]...)
		rm.log(fmt.Sprintf("Batch %d removed (last proxy removed: %s)", batchIdx, proxyURL))
	}

	// If no clients left, switch to direct mode
	if len(rm.clients) == 0 {
		directClient, err := createBatchClient(rm.verbose, rm.discoveryUrl, rm.partnerId, nil)
		if err != nil {
			rm.log(fmt.Sprintf("RemoveProxy: failed to create direct-mode client: %v", err))
			return err
		}
		rm.clients = []*relayleaf.Client{directClient}
		rm.batchProxies = [][]string{nil}
		rm.log("Switched to direct mode (all proxies removed from relay)")
	}

	return nil
}

// findProxy locates a proxy URL across all batches.
// Returns (batchIdx, proxyIdx) or (-1, -1) if not found.
func (rm *RelayManager) findProxy(proxyURL string) (int, int) {
	// Exact match first
	for bi, batch := range rm.batchProxies {
		for pi, p := range batch {
			if p == proxyURL {
				return bi, pi
			}
		}
	}
	// Fallback: match ignoring scheme prefix
	needle := stripScheme(proxyURL)
	for bi, batch := range rm.batchProxies {
		for pi, p := range batch {
			if stripScheme(p) == needle {
				return bi, pi
			}
		}
	}
	return -1, -1
}

// stripScheme removes the protocol scheme (e.g. "socks5://") from a URL.
func stripScheme(url string) string {
	if idx := strings.Index(url, "://"); idx >= 0 {
		return url[idx+3:]
	}
	return url
}

func (rm *RelayManager) IsRunning() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.running
}

func (rm *RelayManager) GetStatus() *Status {
	rm.mu.RLock()
	clients := make([]*relayleaf.Client, len(rm.clients))
	copy(clients, rm.clients)
	rm.mu.RUnlock()

	status := &Status{
		Version: relayleaf.Version(),
	}

	if len(clients) == 0 {
		return status
	}

	status.DeviceId = clients[0].GetDeviceID()

	// Aggregate stats from all parallel clients
	connected, stats := aggregateStats(clients)
	// Functionally connected if exit points detected with uptime
	if !connected && stats.Uptime > 0 && stats.ExitPointsJSON != "" && stats.ExitPointsJSON != "[]" {
		connected = true
	}
	status.Connected = connected
	status.Stats = stats

	return status
}

func (rm *RelayManager) pollStats() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopPoll:
			return
		case <-ticker.C:
			// Grab client refs under lock, then release before DLL calls
			rm.mu.RLock()
			clients := make([]*relayleaf.Client, len(rm.clients))
			copy(clients, rm.clients)
			rm.mu.RUnlock()

			if len(clients) == 0 {
				return
			}

			// Aggregate stats from all parallel clients
			connected, stats := aggregateStats(clients)

			// Consider functionally connected if exit points detected and relay has uptime,
			// even when the NATS control plane is momentarily disconnected between polls.
			if !connected && stats.Uptime > 0 && stats.ExitPointsJSON != "" && stats.ExitPointsJSON != "[]" {
				connected = true
			}

			// Check status change under minimal lock
			rm.mu.Lock()
			statusChanged := connected != rm.lastConnected
			if statusChanged {
				rm.lastConnected = connected
			}
			// Track disconnect duration for watchdog
			needRestart := false
			needEscalation := false
			if connected {
				rm.disconnectSince = time.Time{} // reset
				rm.consecutiveRestarts = 0       // connected OK — reset escalation counter
			} else {
				// Grace period: each batch has batchSize proxies (~2s each),
				// all batches run in parallel, so only need to wait for one batch.
				// Base 30s + 3s per proxy in a single batch, capped at 2 minutes.
				graceDuration := 30*time.Second + time.Duration(batchSize)*3*time.Second
				if graceDuration > 2*time.Minute {
					graceDuration = 2 * time.Minute
				}
				gracePeriod := !rm.lastRestart.IsZero() && time.Since(rm.lastRestart) < graceDuration
				if gracePeriod {
					// Don't track disconnect during grace period (exit point detection)
				} else if rm.disconnectSince.IsZero() {
					rm.disconnectSince = time.Now()
				} else if time.Since(rm.disconnectSince) > 30*time.Second {
					if rm.consecutiveRestarts >= maxFastRestarts {
						needEscalation = true
					} else {
						needRestart = true
					}
					rm.disconnectSince = time.Time{} // reset to avoid repeated restarts
				}
			}
			rm.mu.Unlock()

			// Emit callbacks outside the lock
			if statusChanged && rm.OnStatusChange != nil {
				rm.OnStatusChange(connected)
				// Notify network is back when connection restored
				if connected && rm.OnNetworkStatus != nil {
					rm.OnNetworkStatus(true)
				}
			}
			if rm.OnStatsUpdate != nil {
				rm.OnStatsUpdate(stats)
			}

			// Watchdog: if disconnected too long, restart or escalate to full restart
			if (needRestart || needEscalation) && rm.running {
				// Check internet connectivity before attempting restart
				if !checkConnectivity() {
					rm.log("No internet connectivity, deferring restart")
					if rm.OnNetworkStatus != nil {
						rm.OnNetworkStatus(false)
					}
					continue
				}
				// Internet is reachable — notify UI
				if rm.OnNetworkStatus != nil {
					rm.OnNetworkStatus(true)
				}

				if needEscalation {
					// Set grace period and reset counter to avoid re-triggering during full restart
					rm.mu.Lock()
					rm.lastRestart = time.Now()
					rm.consecutiveRestarts = 0
					rm.mu.Unlock()

					rm.log(fmt.Sprintf("Still disconnected after %d fast restarts, escalating to full restart with health checks", maxFastRestarts))
					if rm.OnNeedRestart != nil {
						go rm.OnNeedRestart()
					}
					continue // keep polling — OnNeedRestart will swap out this manager if it succeeds
				}

				rm.log("Disconnected for >30s, restarting to reset SDK backoff")
				go func() {
					if err := rm.Restart(); err != nil {
						rm.log(fmt.Sprintf("Watchdog restart failed: %v", err))
						if rm.OnNeedRestart != nil {
							rm.OnNeedRestart()
						}
					}
				}()
				return // stop polling — Restart() will start new poll goroutine
			}
		}
	}
}

func (rm *RelayManager) log(msg string) {
	if rm.OnLog != nil {
		rm.OnLog(msg)
	}
}

// checkConnectivity does a quick TCP dial to verify internet is reachable.
func checkConnectivity() bool {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// EnsureLibrary checks and downloads the relay library if needed.
// Non-fatal: logs warnings but always emits "ready" at the end
// so the UI doesn't show a permanent error (stub mode works without DLL).
func (rm *RelayManager) EnsureLibrary() bool {
	rm.emitLibStatus("checking", "Checking library...")

	// Wire up download logging
	relayleaf.LogFunc = func(msg string) {
		rm.log(msg)
		rm.emitLibStatus("checking", msg)
	}

	ok := relayleaf.EnsureLibrary("")
	if ok {
		rm.log("Library ready")
	} else {
		rm.log("Library update unavailable, using built-in stub")
	}
	// Always clear the status tag — stub mode works without the DLL
	rm.emitLibStatus("ready", "")
	return ok
}

// ── internal helpers ──────────────────────────────────────

// startBatches creates parallel SDK clients, each with a subset of proxies.
// Must be called with rm.mu held.
func (rm *RelayManager) startBatches(partnerId string) ([]*relayleaf.Client, error) {
	batches := splitBatches(rm.proxies, batchSize)
	// Always create at least one client (direct mode, no proxies)
	if len(batches) == 0 {
		batches = [][]string{nil}
	}

	rm.log(fmt.Sprintf("Starting %d parallel clients (batch=%d, total_proxies=%d)", len(batches), batchSize, len(rm.proxies)))

	type result struct {
		client *relayleaf.Client
		err    error
		index  int
	}

	ch := make(chan result, len(batches))
	for i, batch := range batches {
		go func(idx int, proxyBatch []string) {
			c, err := createBatchClient(rm.verbose, rm.discoveryUrl, partnerId, proxyBatch)
			ch <- result{c, err, idx}
		}(i, batch)
	}

	// Collect results in original order so batch indices stay aligned
	results := make([]result, len(batches))
	for range batches {
		r := <-ch
		results[r.index] = r
	}

	var liveClients []*relayleaf.Client
	var liveBatches [][]string
	var firstErr error
	for i, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			rm.log(fmt.Sprintf("Batch %d failed: %v", i, r.err))
		} else {
			liveClients = append(liveClients, r.client)
			liveBatches = append(liveBatches, batches[i])
		}
	}

	if len(liveClients) == 0 {
		return nil, fmt.Errorf("all %d clients failed: %w", len(batches), firstErr)
	}

	rm.batchProxies = liveBatches
	rm.log(fmt.Sprintf("%d/%d clients started successfully", len(liveClients), len(batches)))
	return liveClients, nil
}

// createBatchClient creates a single SDK client with the given proxy batch.
func createBatchClient(verbose bool, discoveryUrl, partnerId string, proxies []string) (*relayleaf.Client, error) {
	client, err := relayleaf.NewClient(verbose)
	if err != nil {
		return nil, err
	}

	if discoveryUrl != "" {
		_ = client.SetDiscoveryURL(discoveryUrl)
	}

	for _, p := range proxies {
		_ = client.AddProxy(p)
	}

	if err := client.SetPartnerID(partnerId); err != nil {
		client.Close()
		return nil, fmt.Errorf("set partner ID: %w", err)
	}

	if err := client.Start(); err != nil {
		client.Close()
		return nil, fmt.Errorf("start: %w", err)
	}

	return client, nil
}

// aggregateStats collects and merges stats from all parallel clients.
func aggregateStats(clients []*relayleaf.Client) (connected bool, stats *Stats) {
	stats = &Stats{Timestamp: time.Now().Unix()}
	for _, c := range clients {
		if c == nil {
			continue
		}
		sdkStats, err := c.GetStats()
		if err != nil || sdkStats == nil {
			continue
		}
		if sdkStats.Connected {
			connected = true
		}
		stats.BytesSent += sdkStats.BytesSent
		stats.BytesRecv += sdkStats.BytesReceived
		if sdkStats.UptimeSeconds > stats.Uptime {
			stats.Uptime = sdkStats.UptimeSeconds
		}
		stats.Connections += sdkStats.ConnectedNodes
		stats.TotalStreams += sdkStats.TotalStreams
		stats.ReconnectCount += sdkStats.ReconnectCount
		stats.ActiveStreams += sdkStats.ActiveStreams
		stats.ConnectedNodes += sdkStats.ConnectedNodes
		if sdkStats.ExitPointsJSON != "" && sdkStats.ExitPointsJSON != "[]" {
			stats.ExitPointsJSON = mergeExitPointsJSON(stats.ExitPointsJSON, sdkStats.ExitPointsJSON)
		}
		if sdkStats.NodeAddressesJSON != "" && sdkStats.NodeAddressesJSON != "[]" {
			stats.NodeAddressesJSON = mergeJSONArrays(stats.NodeAddressesJSON, sdkStats.NodeAddressesJSON)
		}
	}
	return
}

// splitBatches divides a slice into chunks of the given size.
func splitBatches(items []string, size int) [][]string {
	if len(items) == 0 {
		return nil
	}
	var batches [][]string
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

// exitPointEntry is used for JSON dedup of exit points by ip_address.
type exitPointEntry struct {
	Type      string `json:"type"`
	Country   string `json:"country"`
	IPAddress string `json:"ip_address"`
}

// mergeExitPointsJSON merges two exit-point JSON arrays, deduplicating by ip_address.
func mergeExitPointsJSON(a, b string) string {
	if a == "" || a == "[]" {
		return b
	}
	if b == "" || b == "[]" {
		return a
	}

	var epsA, epsB []exitPointEntry
	_ = json.Unmarshal([]byte(a), &epsA)
	_ = json.Unmarshal([]byte(b), &epsB)

	seen := make(map[string]struct{}, len(epsA))
	for _, ep := range epsA {
		seen[ep.IPAddress] = struct{}{}
	}
	for _, ep := range epsB {
		if _, exists := seen[ep.IPAddress]; !exists {
			epsA = append(epsA, ep)
			seen[ep.IPAddress] = struct{}{}
		}
	}

	out, _ := json.Marshal(epsA)
	return string(out)
}

// mergeJSONArrays concatenates two JSON arrays into one.
func mergeJSONArrays(a, b string) string {
	if a == "" || a == "[]" {
		return b
	}
	if b == "" || b == "[]" {
		return a
	}
	aInner := strings.TrimSpace(a)
	bInner := strings.TrimSpace(b)
	aInner = strings.TrimPrefix(aInner, "[")
	aInner = strings.TrimSuffix(aInner, "]")
	bInner = strings.TrimPrefix(bInner, "[")
	bInner = strings.TrimSuffix(bInner, "]")
	aInner = strings.TrimSpace(aInner)
	bInner = strings.TrimSpace(bInner)
	if aInner == "" {
		return "[" + bInner + "]"
	}
	if bInner == "" {
		return "[" + aInner + "]"
	}
	return "[" + aInner + "," + bInner + "]"
}
