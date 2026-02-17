package relay

import (
	"fmt"
	"sync"
	"time"

	"relay-app/pkg/relayleaf"
)

type Stats struct {
	BytesSent      int64  `json:"bytes_sent"`
	BytesRecv      int64  `json:"bytes_recv"`
	Uptime         int64  `json:"uptime"`
	Connections    int32  `json:"connections"`
	TotalStreams   int64  `json:"total_streams"`
	ReconnectCount int64  `json:"reconnect_count"`
	ActiveStreams  int32  `json:"active_streams"`
	ConnectedNodes int32  `json:"connected_nodes"`
	Timestamp      int64  `json:"timestamp"`
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
	client          *relayleaf.Client
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
	OnNeedRestart   func() // called when disconnected too long (SDK backoff stuck)
	lastConnected   bool
	cachedDeviceId  string
	disconnectSince time.Time // when connection was lost (zero = connected)
	lastRestart     time.Time // when last Restart() happened (grace period)
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

	if rm.client != nil {
		rm.client.Close()
	}

	client, err := relayleaf.NewClient(verbose)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	rm.client = client
	rm.verbose = verbose
	rm.log("BNC node initialized")
	return nil
}

func (rm *RelayManager) SetDiscoveryURL(url string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.client == nil {
		return fmt.Errorf("client not initialized")
	}
	rm.discoveryUrl = url
	return rm.client.SetDiscoveryURL(url)
}

func (rm *RelayManager) AddProxy(proxyURL string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.client == nil {
		return fmt.Errorf("client not initialized")
	}
	if err := rm.client.AddProxy(proxyURL); err != nil {
		return err
	}
	rm.proxies = append(rm.proxies, proxyURL)
	return nil
}

func (rm *RelayManager) Start(partnerId string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if rm.client == nil {
		return fmt.Errorf("client not initialized")
	}

	if rm.running {
		return fmt.Errorf("node already running")
	}

	if err := rm.client.SetPartnerID(partnerId); err != nil {
		return fmt.Errorf("failed to set partner ID: %w", err)
	}

	if err := rm.client.Start(); err != nil {
		return fmt.Errorf("failed to start node: %w", err)
	}

	rm.running = true
	rm.partnerId = partnerId
	rm.cachedDeviceId = rm.client.GetDeviceID()
	rm.stopPoll = make(chan struct{})
	rm.log(fmt.Sprintf("Node started with partner ID: %s", partnerId))

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

	if rm.client != nil {
		if err := rm.client.Stop(); err != nil {
			return fmt.Errorf("failed to stop node: %w", err)
		}
	}

	rm.running = false
	rm.log("Node stopped")
	return nil
}

// Restart recreates the SDK client to reset exponential backoff.
// This is a fast path — reuses stored proxies, no health checks.
func (rm *RelayManager) Restart() error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if !rm.running {
		return fmt.Errorf("node not running")
	}

	partnerId := rm.partnerId
	verbose := rm.verbose
	discoveryUrl := rm.discoveryUrl
	proxies := make([]string, len(rm.proxies))
	copy(proxies, rm.proxies)

	// Stop polling and old client
	close(rm.stopPoll)
	if rm.client != nil {
		_ = rm.client.Stop()
		rm.client.Close()
		rm.client = nil
	}
	rm.running = false

	// Create fresh client
	client, err := relayleaf.NewClient(verbose)
	if err != nil {
		return fmt.Errorf("restart: failed to create client: %w", err)
	}

	if discoveryUrl != "" {
		_ = client.SetDiscoveryURL(discoveryUrl)
	}

	for _, p := range proxies {
		_ = client.AddProxy(p)
	}

	if err := client.SetPartnerID(partnerId); err != nil {
		client.Close()
		return fmt.Errorf("restart: failed to set partner ID: %w", err)
	}

	if err := client.Start(); err != nil {
		client.Close()
		return fmt.Errorf("restart: failed to start: %w", err)
	}

	rm.client = client
	rm.running = true
	rm.cachedDeviceId = client.GetDeviceID()
	rm.stopPoll = make(chan struct{})
	rm.lastConnected = false
	rm.disconnectSince = time.Time{}
	rm.lastRestart = time.Now()

	rm.log(fmt.Sprintf("Fast restart completed (partner=%s, proxies=%d)", partnerId, len(proxies)))

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

	if rm.client != nil {
		rm.client.Close()
		rm.client = nil
	}
}

func (rm *RelayManager) IsRunning() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.running
}

func (rm *RelayManager) GetStatus() *Status {
	rm.mu.RLock()
	client := rm.client
	rm.mu.RUnlock()

	status := &Status{
		Version: relayleaf.Version(),
	}

	if client == nil {
		return status
	}

	status.DeviceId = client.GetDeviceID()

	// Single GetStats call — derive Connected from it (avoids double DLL call)
	if sdkStats, err := client.GetStats(); err == nil && sdkStats != nil {
		status.Connected = sdkStats.Connected
		status.Stats = &Stats{
			BytesSent:      sdkStats.BytesSent,
			BytesRecv:      sdkStats.BytesReceived,
			Uptime:         sdkStats.UptimeSeconds,
			Connections:    sdkStats.ConnectedNodes,
			TotalStreams:   sdkStats.TotalStreams,
			ReconnectCount: sdkStats.ReconnectCount,
			ActiveStreams:  sdkStats.ActiveStreams,
			ConnectedNodes: sdkStats.ConnectedNodes,
			Timestamp:         time.Now().Unix(),
			ExitPointsJSON:    sdkStats.ExitPointsJSON,
			NodeAddressesJSON: sdkStats.NodeAddressesJSON,
		}
	}

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
			// Grab client ref under lock, then release before DLL calls
			rm.mu.RLock()
			client := rm.client
			rm.mu.RUnlock()

			if client == nil {
				return
			}

			// Single DLL call — derive connected from stats (avoids double GetStats)
			sdkStats, err := client.GetStats()
			if err != nil || sdkStats == nil {
				continue
			}

			connected := sdkStats.Connected
			stats := &Stats{
				BytesSent:      sdkStats.BytesSent,
				BytesRecv:      sdkStats.BytesReceived,
				Uptime:         sdkStats.UptimeSeconds,
				Connections:    sdkStats.ConnectedNodes,
				TotalStreams:   sdkStats.TotalStreams,
				ReconnectCount: sdkStats.ReconnectCount,
				ActiveStreams:  sdkStats.ActiveStreams,
				ConnectedNodes: sdkStats.ConnectedNodes,
				Timestamp:         time.Now().Unix(),
				ExitPointsJSON:    sdkStats.ExitPointsJSON,
				NodeAddressesJSON: sdkStats.NodeAddressesJSON,
			}

			// Check status change under minimal lock
			rm.mu.Lock()
			statusChanged := connected != rm.lastConnected
			if statusChanged {
				rm.lastConnected = connected
			}
			// Track disconnect duration for watchdog
			needRestart := false
			if connected {
				rm.disconnectSince = time.Time{} // reset
			} else {
				// Skip watchdog for 30s after a restart (exit point detection takes time)
				gracePeriod := !rm.lastRestart.IsZero() && time.Since(rm.lastRestart) < 30*time.Second
				if gracePeriod {
					// Don't track disconnect during grace period
				} else if rm.disconnectSince.IsZero() {
					rm.disconnectSince = time.Now()
				} else if time.Since(rm.disconnectSince) > 5*time.Second {
					needRestart = true
					rm.disconnectSince = time.Time{} // reset to avoid repeated restarts
				}
			}
			rm.mu.Unlock()

			// Emit callbacks outside the lock
			if statusChanged && rm.OnStatusChange != nil {
				rm.OnStatusChange(connected)
			}
			if rm.OnStatsUpdate != nil {
				rm.OnStatsUpdate(stats)
			}

			// Watchdog: if disconnected too long, trigger restart to reset SDK backoff
			if needRestart {
				rm.log("Disconnected for >5s, restarting to reset SDK backoff")
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
