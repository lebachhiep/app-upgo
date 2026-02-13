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
	mu              sync.RWMutex
	stopPoll        chan struct{}
	OnStatsUpdate   func(*Stats)
	OnStatusChange  func(bool)
	OnLog           func(string)
	OnLibraryStatus func(status, detail string)
	lastConnected   bool
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
	rm.log("BNC node initialized")
	return nil
}

func (rm *RelayManager) SetDiscoveryURL(url string) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rm.client == nil {
		return fmt.Errorf("client not initialized")
	}
	return rm.client.SetDiscoveryURL(url)
}

func (rm *RelayManager) AddProxy(proxyURL string) error {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if rm.client == nil {
		return fmt.Errorf("client not initialized")
	}
	return rm.client.AddProxy(proxyURL)
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
	defer rm.mu.RUnlock()

	status := &Status{
		Version: relayleaf.Version(),
	}

	if rm.client == nil {
		return status
	}

	status.DeviceId = rm.client.GetDeviceID()
	status.Connected = rm.client.IsConnected()

	if sdkStats, err := rm.client.GetStats(); err == nil && sdkStats != nil {
		status.Stats = &Stats{
			BytesSent:      sdkStats.BytesSent,
			BytesRecv:      sdkStats.BytesReceived,
			Uptime:         sdkStats.UptimeSeconds,
			Connections:    sdkStats.ConnectedNodes,
			TotalStreams:   sdkStats.TotalStreams,
			ReconnectCount: sdkStats.ReconnectCount,
			ActiveStreams:  sdkStats.ActiveStreams,
			ConnectedNodes: sdkStats.ConnectedNodes,
			Timestamp:      time.Now().Unix(),
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
			rm.mu.Lock()
			if rm.client == nil {
				rm.mu.Unlock()
				return
			}

			connected := rm.client.IsConnected()
			statusChanged := connected != rm.lastConnected
			if statusChanged {
				rm.lastConnected = connected
			}

			var stats *Stats
			if sdkStats, err := rm.client.GetStats(); err == nil && sdkStats != nil {
				stats = &Stats{
					BytesSent:      sdkStats.BytesSent,
					BytesRecv:      sdkStats.BytesReceived,
					Uptime:         sdkStats.UptimeSeconds,
					Connections:    sdkStats.ConnectedNodes,
					TotalStreams:   sdkStats.TotalStreams,
					ReconnectCount: sdkStats.ReconnectCount,
					ActiveStreams:  sdkStats.ActiveStreams,
					ConnectedNodes: sdkStats.ConnectedNodes,
					Timestamp:      time.Now().Unix(),
				}
			}
			rm.mu.Unlock()

			// Emit callbacks outside the lock to avoid holding it during callbacks
			if statusChanged && rm.OnStatusChange != nil {
				rm.OnStatusChange(connected)
			}
			if stats != nil && rm.OnStatsUpdate != nil {
				rm.OnStatsUpdate(stats)
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
