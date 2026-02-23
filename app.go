package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog/log"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"relay-app/internal/autostart"
	"relay-app/internal/cli"
	"relay-app/internal/config"
	"relay-app/internal/proxy"
	"relay-app/internal/relay"
	"relay-app/internal/selfinstall"
	"relay-app/internal/window"
)

type App struct {
	ctx           context.Context
	version       string
	manager       *relay.RelayManager // control manager (EnsureLibrary only, never Started)
	relayMgr      *relay.RelayManager // parallel SDK clients (proxies batched)
	relayMu       sync.RWMutex
	relayStarting bool                        // true while StartRelay is in progress
	lastStats     atomic.Pointer[relay.Stats] // latest aggregated stats from all clients
	mu            sync.RWMutex
	logs          []string
	logMu         sync.RWMutex
	silentMode    bool
	proxyStatuses []proxy.Status
	proxyStatusMu sync.RWMutex
	userStopped     atomic.Bool        // true when user explicitly stopped — blocks watchdog/auto-restart
	startCancel     context.CancelFunc // cancel in-progress StartRelay (health checks)
	retryCancel     context.CancelFunc // cancel dead proxy retry goroutine
	lastFullRestart time.Time          // throttles OnNeedRestart escalations
	exitPointsMu       sync.Mutex
	seenExitPoints     map[string]seenEP // all ever-seen exit points (keyed by ip_address)
	detectionStartTime time.Time         // when SDK start began (for measuring detection time)
	detectionLogged    bool              // true after first detection time log
}

type seenEP struct {
	Type     string `json:"Type"`
	Country  string `json:"Country"`
	LastSeen int64  `json:"LastSeen,omitempty"` // unix timestamp — prune entries older than 7 days
}

func NewApp() *App {
	return &App{
		logs: make([]string, 0, 500),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Control manager — used only for EnsureLibrary, never Started
	a.manager = relay.NewRelayManager()
	a.manager.OnLog = func(msg string) {
		a.addLog(msg)
		runtime.EventsEmit(a.ctx, "log:new", msg)
	}
	a.manager.OnLibraryStatus = func(status, detail string) {
		runtime.EventsEmit(a.ctx, "library:status", map[string]string{
			"status": status,
			"detail": detail,
		})
	}

	// Ensure autostart + desktop shortcut on every startup
	go func() {
		defer sentry.Recover()
		cfg := config.Get()
		if !cfg.GetBool("autostart_initialized") {
			// First run — enable autostart by default
			if err := autostart.Enable(); err != nil {
				log.Warn().Err(err).Msg("Failed to enable autostart on first run")
			} else {
				log.Info().Msg("Autostart enabled on first run")
			}
			cfg.Set("launch_on_startup", true)
			cfg.Set("auto_start", true)
			cfg.Set("autostart_initialized", true)
			config.Save()
		} else if cfg.GetBool("launch_on_startup") {
			// Ensure autostart points to current exe
			if err := autostart.Enable(); err != nil {
				log.Warn().Err(err).Msg("Failed to ensure autostart registry entry")
			} else {
				log.Info().Msg("Autostart registry entry ensured")
			}
		}

		// Always ensure desktop shortcut exists (recreate if user deleted it)
		if err := selfinstall.CreateDesktopShortcut(); err != nil {
			log.Warn().Err(err).Msg("Failed to ensure desktop shortcut")
		}
	}()

	// Ensure relay library is ready at startup (download if hash mismatch)
	// Then auto-start relay if configured
	go func() {
		defer sentry.Recover()
		time.Sleep(500 * time.Millisecond)
		a.manager.EnsureLibrary()

		cfg := config.Get()
		partnerId := cfg.GetString("partner_id")

		// Always auto-start relay on startup
		if err := a.StartRelay(partnerId); err != nil {
			log.Error().Err(err).Msg("Auto-start relay failed")
		}
	}()

	// Constrain window to screen, then set initial state
	go func() {
		defer sentry.Recover()
		// Install WM_GETMINMAXINFO handler first (retry until window is ready)
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			if err := window.ConstrainToScreen("UPGO Node"); err == nil {
				log.Info().Msg("Window constrained to screen")
				break
			}
		}

		if a.silentMode {
			// Window was started hidden via StartHidden option — nothing to do.
			log.Info().Msg("Silent mode: window hidden at launch")
		} else {
			// Cross-platform: use Wails runtime to get screen size, resize to 50%, center
			a.centerAndResize50()
			time.Sleep(300 * time.Millisecond)
			a.centerAndResize50()
			runtime.WindowShow(a.ctx)
			log.Info().Msg("Window centered and resized to 50%")
		}
	}()
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	// If relay not running and user didn't explicitly stop, start it before hiding
	if !a.isRelayRunning() && !a.userStopped.Load() {
		cfg := config.Get()
		go func() {
			defer sentry.Recover()
			if err := a.StartRelay(cfg.GetString("partner_id")); err != nil {
				log.Error().Err(err).Msg("Auto-start relay on close failed")
			}
		}()
	}
	// Hide async to avoid deadlock with Wails message pump
	go func() {
		time.Sleep(50 * time.Millisecond)
		runtime.WindowHide(a.ctx)
	}()
	return true // prevent close — app must run in background permanently
}

func (a *App) shutdown(ctx context.Context) {
	a.stopRelay()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager != nil {
		a.manager.Close()
	}
	// sentry.Flush handled by defer in main()
}

func (a *App) addLog(msg string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.logs = append(a.logs, msg)
	if len(a.logs) > 1000 {
		a.logs = a.logs[len(a.logs)-500:]
	}
}

func (a *App) StartRelay(partnerId string) error {
	// Clear userStopped — this is a deliberate start
	a.userStopped.Store(false)
	// Load historical exit points for local UI display.
	a.loadSeenExitPoints()

	// Cancel any previous in-progress StartRelay — brief lock only
	a.mu.Lock()
	if a.startCancel != nil {
		a.startCancel()
	}
	startCtx, startCancel := context.WithCancel(context.Background())
	a.startCancel = startCancel
	a.mu.Unlock() // Release BEFORE health checks so StopRelay is never blocked

	// Mark as starting so isRelayRunning() returns true during proxy checks
	a.relayMu.Lock()
	a.relayStarting = true
	a.relayMu.Unlock()
	defer func() {
		a.relayMu.Lock()
		a.relayStarting = false
		a.relayMu.Unlock()
	}()

	cfg := config.Get()
	verbose := cfg.GetBool("verbose")
	discoveryUrl := cfg.GetString("discovery_url")

	// ── Health checks run WITHOUT holding a.mu — StopRelay can proceed instantly ──
	proxies := cfg.GetStringSlice("proxies")
	var allStatuses []proxy.Status
	startTime := time.Now()

	// Progress helper
	emitProgress := func(step string, detail string, pct int) {
		runtime.EventsEmit(a.ctx, "relay:progress", map[string]interface{}{
			"step":    step,
			"detail":  detail,
			"percent": pct,
			"elapsed": int(time.Since(startTime).Seconds()),
		})
	}

	if len(proxies) > 0 {
		emitProgress("checking_proxies", fmt.Sprintf("Checking %d proxies...", len(proxies)), 5)

		allStatuses = make([]proxy.Status, len(proxies))
		for i, p := range proxies {
			allStatuses[i] = proxy.Status{URL: p, Error: "checking"}
		}
		runtime.EventsEmit(a.ctx, "proxy:status", allStatuses)

		// Check in parallel — auto-detects protocol
		var checked int64
		var wg sync.WaitGroup
		for i, p := range proxies {
			wg.Add(1)
			go func(idx int, proxyUrl string) {
				defer wg.Done()
				allStatuses[idx] = proxy.CheckHealth(proxyUrl)
				runtime.EventsEmit(a.ctx, "proxy:status", allStatuses)
				n := atomic.AddInt64(&checked, 1)
				alive := 0
				for _, s := range allStatuses {
					if s.Alive {
						alive++
					}
				}
				pct := 5 + int(n*20/int64(len(proxies)))
				emitProgress("checking_proxies", fmt.Sprintf("Checked %d/%d proxies (%d alive)", n, len(proxies), alive), pct)
			}(i, p)
		}
		wg.Wait()

		// Bail out if user stopped during health checks
		if startCtx.Err() != nil || a.userStopped.Load() {
			emitProgress("cancelled", "Cancelled by user", 0)
			return fmt.Errorf("start cancelled")
		}

		aliveCount := 0
		now := time.Now().Unix()
		for i, ps := range allStatuses {
			if ps.Alive {
				allStatuses[i].Since = now
				aliveCount++
			} else {
				log.Warn().Str("proxy", ps.URL).Str("error", ps.Error).Msg("Proxy dead, skipping")
			}
		}

		deadCount := len(proxies) - aliveCount
		log.Warn().Int("alive", aliveCount).Int("dead", deadCount).Int("total", len(proxies)).Msg("HEALTH CHECK RESULT")
		emitProgress("proxies_ready", fmt.Sprintf("%d/%d proxies alive", aliveCount, len(proxies)), 25)

		// Add dead proxies to seenExitPoints as offline so they show on the web.
		// We use the proxy IP as the exit IP (real exit IP is unknown for dead proxies).
		a.exitPointsMu.Lock()
		for _, ps := range allStatuses {
			if ps.Alive {
				continue
			}
			proxyIP := extractProxyIP(ps.URL)
			if proxyIP == "" {
				log.Warn().Str("url", ps.URL).Msg("dead proxy: extractProxyIP returned empty")
				continue
			}
			proto := ps.Protocol
			if proto == "" {
				proto = "http"
			}
			a.seenExitPoints[proxyIP] = seenEP{
				Type:     proto,
				Country:  "",
				LastSeen: now,
			}
			log.Warn().Str("ip", proxyIP).Str("proto", proto).Msg("dead proxy added as offline exit")
		}
		deadAdded := len(a.seenExitPoints)
		a.exitPointsMu.Unlock()
		log.Warn().Int("dead_exits_added", deadAdded).Msg("seenExitPoints after dead proxies")

		// Persist statuses for dashboard
		a.proxyStatusMu.Lock()
		a.proxyStatuses = allStatuses
		a.proxyStatusMu.Unlock()
		runtime.EventsEmit(a.ctx, "proxy:status", allStatuses)
	}

	// ── Acquire lock for relay creation/start phase (fast) ──
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check cancellation after re-acquiring lock
	if startCtx.Err() != nil || a.userStopped.Load() {
		return fmt.Errorf("start cancelled")
	}

	// Create parallel SDK clients (proxies batched internally)
	mgr := relay.NewRelayManager()
	mgr.OnLog = func(msg string) {
		a.addLog(msg)
		runtime.EventsEmit(a.ctx, "log:new", msg)
	}
	mgr.OnStatsUpdate = func(stats *relay.Stats) {
		a.lastStats.Store(stats)
		runtime.EventsEmit(a.ctx, "stats:update", stats)
		// Emit exit detection progress while not yet connected
		if stats.ExitPointsJSON != "" && stats.ExitPointsJSON != "[]" {
			count := strings.Count(stats.ExitPointsJSON, "ip_address")
			pct := 50 + count/3 // gradually increase from 50 toward ~90
			if pct > 90 {
				pct = 90
			}
			emitProgress("detecting_exits", fmt.Sprintf("Detecting exit points... %d found", count), pct)

			// Log detection time once when first exit points appear
			if !a.detectionLogged && !a.detectionStartTime.IsZero() {
				elapsed := time.Since(a.detectionStartTime)
				a.detectionLogged = true
				log.Warn().
					Int("exit_count", count).
					Str("detection_time", fmt.Sprintf("%.1fs", elapsed.Seconds())).
					Msg("EXIT DETECTION: first exit points detected")
				runtime.EventsEmit(a.ctx, "detection:time", map[string]interface{}{
					"elapsed_seconds": elapsed.Seconds(),
					"exit_count":      count,
				})
			}
		}
	}
	mgr.OnStatusChange = func(connected bool) {
		runtime.EventsEmit(a.ctx, "status:change", connected)
		if connected {
			emitProgress("connected", "Connected!", 100)
		} else {
			emitProgress("reconnecting", "Reconnecting...", 50)
		}
	}
	mgr.OnNetworkStatus = func(online bool) {
		runtime.EventsEmit(a.ctx, "network:status", online)
	}
	mgr.OnNeedRestart = func() {
		// Skip restart if user explicitly stopped
		if a.userStopped.Load() {
			log.Info().Msg("Watchdog fallback: skipped (user stopped)")
			return
		}
		// Throttle: at most one full restart every 2 minutes
		if !a.lastFullRestart.IsZero() && time.Since(a.lastFullRestart) < 2*time.Minute {
			log.Info().Msg("Watchdog fallback: throttled (last full restart < 2m ago)")
			return
		}
		a.lastFullRestart = time.Now()
		// Fallback: fast restarts failed repeatedly, do a full StartRelay with health checks
		cfg := config.Get()
		pid := cfg.GetString("partner_id")
		if pid != "" {
			log.Info().Msg("Watchdog fallback: full relay restart with health checks")
			if err := a.StartRelay(pid); err != nil {
				log.Error().Err(err).Msg("Watchdog fallback: relay restart failed")
			}
		}
	}

	emitProgress("initializing", "Initializing SDK...", 30)

	if err := mgr.Init(verbose); err != nil {
		emitProgress("error", fmt.Sprintf("Init failed: %v", err), 0)
		return fmt.Errorf("failed to init node: %w", err)
	}

	// Check cancellation after slow DLL init
	if a.userStopped.Load() {
		mgr.Close()
		return fmt.Errorf("start cancelled")
	}

	if discoveryUrl != "" {
		if err := mgr.SetDiscoveryURL(discoveryUrl); err != nil {
			log.Warn().Err(err).Msg("Failed to set discovery URL")
		}
	}

	// Add all alive proxies (will be batched into parallel clients on Start)
	addedCount := 0
	for _, ps := range allStatuses {
		if !ps.Alive {
			continue
		}
		proxyURL := proxy.BuildProxyURL(ps.URL, ps.Protocol)
		if err := mgr.AddProxy(proxyURL); err != nil {
			log.Warn().Err(err).Str("proxy", ps.URL).Msg("Failed to add proxy")
		} else {
			addedCount++
		}
	}

	emitProgress("connecting", fmt.Sprintf("Connecting with %d proxies...", addedCount), 40)

	// Check cancellation before slow DLL start
	if a.userStopped.Load() {
		mgr.Close()
		return fmt.Errorf("start cancelled")
	}

	if err := mgr.Start(partnerId); err != nil {
		emitProgress("error", fmt.Sprintf("Start failed: %v", err), 0)
		mgr.Close()
		return fmt.Errorf("failed to start node: %w", err)
	}

	// Check cancellation after slow DLL start
	if a.userStopped.Load() {
		_ = mgr.Stop()
		mgr.Close()
		return fmt.Errorf("start cancelled")
	}

	emitProgress("detecting_exits", "Detecting exit points...", 50)
	a.detectionStartTime = time.Now()
	a.detectionLogged = false

	// Atomic swap: stop old relay, install new one
	a.relayMu.Lock()
	old := a.relayMgr
	a.relayMgr = mgr
	a.relayMu.Unlock()

	// Clean up old relay (if any) outside the lock
	if old != nil {
		_ = old.Stop()
		old.Close()
	}

	log.Info().Int("proxies_added", addedCount).Int("proxies_total", len(proxies)).Msg("Parallel SDK clients started")

	// Auto-enable launch_on_startup + auto_start on first Partner ID
	oldPartnerId := cfg.GetString("partner_id")
	firstPartner := oldPartnerId == "" && partnerId != ""

	cfg.Set("partner_id", partnerId)
	if firstPartner {
		cfg.Set("auto_start", true)
		cfg.Set("launch_on_startup", true)
		go func() {
			if err := autostart.Enable(); err != nil {
				log.Warn().Err(err).Msg("Failed to auto-enable startup")
			} else {
				log.Info().Msg("Auto-enabled launch on startup for first Partner ID")
			}
		}()
	}
	config.Save()

	runtime.EventsEmit(a.ctx, "relay:started", true)
	if firstPartner {
		runtime.EventsEmit(a.ctx, "config:updated", a.GetConfig())
	}

	// Start dead proxy retry goroutine (cancel any previous one)
	if a.retryCancel != nil {
		a.retryCancel()
	}
	retryCtx, retryCancel := context.WithCancel(context.Background())
	a.retryCancel = retryCancel
	go a.retryDeadProxies(retryCtx)

	return nil
}

func (a *App) StopRelay() error {
	// Set userStopped BEFORE acquiring lock — blocks watchdog/auto-restart immediately
	a.userStopped.Store(true)

	// Cancel any in-progress StartRelay (health checks)
	if a.startCancel != nil {
		a.startCancel()
	}
	// Stop dead proxy retry goroutine
	if a.retryCancel != nil {
		a.retryCancel()
		a.retryCancel = nil
	}

	// Emit stopped event BEFORE the lock so UI never freezes
	runtime.EventsEmit(a.ctx, "relay:stopped", true)

	a.mu.Lock()
	defer a.mu.Unlock()

	a.stopRelay()
	return nil
}

type RelayStatusResponse struct {
	IsConnected bool         `json:"IsConnected"`
	DeviceId    string       `json:"DeviceId"`
	Stats       *relay.Stats `json:"Stats"`
	Version     string       `json:"Version"`
	PartnerId   string       `json:"PartnerId"`
	Proxies     []string     `json:"Proxies"`
}

func (a *App) GetStatus() (*RelayStatusResponse, error) {
	cfg := config.Get()
	resp := &RelayStatusResponse{
		PartnerId: cfg.GetString("partner_id"),
		Proxies:   cfg.GetStringSlice("proxies"),
		Version:   relay.GetLibraryVersion(),
	}

	a.relayMu.RLock()
	mgr := a.relayMgr
	a.relayMu.RUnlock()

	if mgr == nil {
		return resp, nil
	}

	resp.IsConnected = mgr.LastConnected()
	resp.DeviceId = mgr.CachedDeviceId()

	if stats := a.lastStats.Load(); stats != nil {
		resp.Stats = stats
	}

	return resp, nil
}

func (a *App) IsRelayRunning() bool {
	return a.isRelayRunning()
}

func (a *App) isRelayRunning() bool {
	a.relayMu.RLock()
	defer a.relayMu.RUnlock()
	return a.relayMgr != nil || a.relayStarting
}

func (a *App) GetConfig() map[string]interface{} {
	cfg := config.Get()
	return map[string]interface{}{
		"partner_id":        cfg.GetString("partner_id"),
		"discovery_url":     cfg.GetString("discovery_url"),
		"proxies":           cfg.GetStringSlice("proxies"),
		"verbose":           cfg.GetBool("verbose"),
		"auto_start":        cfg.GetBool("auto_start"),
		"launch_on_startup": cfg.GetBool("launch_on_startup"),
		"log_level":         cfg.GetString("log_level"),
		"api_url":           cfg.GetString("api_url"),
	}
}

// allowedConfigKeys restricts which config keys the frontend may modify.
var allowedConfigKeys = map[string]bool{
	"partner_id":        true,
	"discovery_url":     true,
	"verbose":           true,
	"auto_start":        true,
	"launch_on_startup": true,
	"log_level":         true,
	"api_url":           true,
}

func (a *App) SetConfigValue(key, value string) error {
	normalized := config.NormalizeKey(key)
	if !allowedConfigKeys[normalized] {
		return fmt.Errorf("config key not allowed: %s", key)
	}
	cfg := config.Get()
	cfg.Set(normalized, value)
	if err := config.Save(); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "config:updated", a.GetConfig())
	return nil
}

func (a *App) GetConfigValue(key string) (string, error) {
	cfg := config.Get()
	return cfg.GetString(config.NormalizeKey(key)), nil
}

// mergeExitPoints merges SDK's current exit points with all historically seen IPs.
// Returns JSON with online/offline flag so offline IPs are preserved in the web dashboard.
func (a *App) mergeExitPoints(currentJSON string) string {
	type rawEP struct {
		Type      string `json:"type"`
		Country   string `json:"country"`
		IPAddress string `json:"ip_address"`
	}
	var current []rawEP
	if currentJSON != "" && currentJSON != "[]" {
		_ = json.Unmarshal([]byte(currentJSON), &current)
	}

	activeSet := make(map[string]bool, len(current))

	a.exitPointsMu.Lock()
	if a.seenExitPoints == nil {
		a.seenExitPoints = make(map[string]seenEP)
	}

	now := time.Now().Unix()
	for _, ep := range current {
		if ep.IPAddress == "" {
			continue
		}
		activeSet[ep.IPAddress] = true
		a.seenExitPoints[ep.IPAddress] = seenEP{
			Type:     ep.Type,
			Country:  ep.Country,
			LastSeen: now,
		}
	}

	type syncEP struct {
		Type      string `json:"type"`
		Country   string `json:"country"`
		IPAddress string `json:"ip_address"`
		Online    bool   `json:"online"`
	}
	merged := make([]syncEP, 0, len(a.seenExitPoints))
	for ip, ep := range a.seenExitPoints {
		merged = append(merged, syncEP{
			Type:      ep.Type,
			Country:   ep.Country,
			IPAddress: ip,
			Online:    activeSet[ip],
		})
	}
	// Save snapshot for persistence across restarts
	data, _ := json.Marshal(a.seenExitPoints)
	needSave := len(a.seenExitPoints) > 0
	a.exitPointsMu.Unlock()

	if needSave {
		fp := filepath.Join(config.GetConfigDir(), "seen_exits.json")
		if err := os.WriteFile(fp, data, 0644); err != nil {
			log.Error().Err(err).Str("path", fp).Msg("failed to save seen_exits.json")
		}
	}

	out, _ := json.Marshal(merged)
	if len(out) == 0 {
		return "[]"
	}
	return string(out)
}

func (a *App) loadSeenExitPoints() {
	a.exitPointsMu.Lock()
	defer a.exitPointsMu.Unlock()

	fp := filepath.Join(config.GetConfigDir(), "seen_exits.json")
	data, err := os.ReadFile(fp)
	if err != nil {
		a.seenExitPoints = make(map[string]seenEP)
		return
	}
	m := make(map[string]seenEP)
	if err := json.Unmarshal(data, &m); err != nil {
		a.seenExitPoints = make(map[string]seenEP)
		return
	}
	// Prune entries not seen in >7 days (from removed proxies)
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	for ip, ep := range m {
		if ep.LastSeen > 0 && ep.LastSeen < cutoff {
			delete(m, ip)
		}
	}
	log.Info().Int("loaded", len(m)).Msg("loaded seen exit points from disk")
	a.seenExitPoints = m
}

// extractProxyIP extracts the IP address from a proxy URL like "user:pass@IP:port" or "IP:port".
func extractProxyIP(raw string) string {
	raw = strings.TrimSpace(raw)
	// Strip scheme if present
	if idx := strings.Index(raw, "://"); idx >= 0 {
		raw = raw[idx+3:]
	}
	// Strip user:pass@ prefix
	if idx := strings.LastIndex(raw, "@"); idx >= 0 {
		raw = raw[idx+1:]
	}
	// Now raw is "IP:port" — strip port
	host := raw
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return strings.TrimSpace(host)
}

func (a *App) AddProxy(proxyUrl string) error {
	normalized := proxy.NormalizeURL(proxyUrl)

	cfg := config.Get()
	proxies := cfg.GetStringSlice("proxies")
	for _, p := range proxies {
		if p == normalized {
			return fmt.Errorf("proxy already exists: %s", normalized)
		}
	}
	proxies = append(proxies, normalized)
	cfg.Set("proxies", proxies)
	if err := config.Save(); err != nil {
		return err
	}

	runtime.EventsEmit(a.ctx, "proxies:updated", proxies)
	return nil
}

func (a *App) RemoveProxy(proxyUrl string) error {
	cfg := config.Get()
	proxies := cfg.GetStringSlice("proxies")
	newProxies := make([]string, 0, len(proxies))
	for _, p := range proxies {
		if p != proxyUrl {
			newProxies = append(newProxies, p)
		}
	}
	cfg.Set("proxies", newProxies)
	if err := config.Save(); err != nil {
		return err
	}

	// Remove only the specific proxy status (keep others intact)
	var relayURL string
	a.proxyStatusMu.Lock()
	if a.proxyStatuses != nil {
		newStatuses := make([]proxy.Status, 0, len(a.proxyStatuses))
		for _, s := range a.proxyStatuses {
			if s.URL == proxyUrl {
				// Build the relay-format URL for hot-removal
				relayURL = proxy.BuildProxyURL(s.URL, s.Protocol)
			} else {
				newStatuses = append(newStatuses, s)
			}
		}
		a.proxyStatuses = newStatuses
	}
	a.proxyStatusMu.Unlock()

	runtime.EventsEmit(a.ctx, "proxy:status", a.proxyStatuses)
	runtime.EventsEmit(a.ctx, "proxies:updated", newProxies)

	// Hot-remove proxy from running relay — only restarts the affected batch
	if a.isRelayRunning() {
		a.relayMu.RLock()
		mgr := a.relayMgr
		a.relayMu.RUnlock()
		if mgr != nil {
			if relayURL == "" {
				// Fallback: try raw URL (stripScheme matching in manager handles format mismatch)
				relayURL = proxyUrl
			}
			go func() {
				if err := mgr.RemoveProxy(relayURL); err != nil {
					log.Error().Err(err).Msg("Failed to hot-remove proxy from relay")
				}
			}()
		}
	}
	return nil
}

func (a *App) RemoveAllProxies() error {
	cfg := config.Get()
	cfg.Set("proxies", []string{})
	if err := config.Save(); err != nil {
		return err
	}

	// Clear proxy statuses
	a.proxyStatusMu.Lock()
	a.proxyStatuses = nil
	a.proxyStatusMu.Unlock()

	runtime.EventsEmit(a.ctx, "proxy:status", []proxy.Status{})
	runtime.EventsEmit(a.ctx, "proxies:updated", []string{})

	// Restart relay (direct only, no proxies)
	partnerId := cfg.GetString("partner_id")
	if partnerId != "" && a.isRelayRunning() {
		go func() {
			if err := a.StartRelay(partnerId); err != nil {
				log.Error().Err(err).Msg("Failed to restart relay after removing all proxies")
			}
		}()
	}
	return nil
}

func (a *App) GetProxies() []string {
	cfg := config.Get()
	return cfg.GetStringSlice("proxies")
}

// CheckProxy tests a single proxy by connecting through it to a known host.
func (a *App) CheckProxy(proxyUrl string) proxy.Status {
	result := proxy.CheckHealth(proxyUrl)
	if result.Alive {
		result.Since = time.Now().Unix()
	}

	// Update in persisted statuses — preserve accumulated bandwidth
	a.proxyStatusMu.Lock()
	for i, ps := range a.proxyStatuses {
		if ps.URL == proxyUrl {
			result.BytesSent = ps.BytesSent
			result.BytesRecv = ps.BytesRecv
			if result.Alive && ps.Alive && ps.Since > 0 {
				result.Since = ps.Since // keep original alive-since if still alive
			}
			a.proxyStatuses[i] = result
			break
		}
	}
	a.proxyStatusMu.Unlock()

	return result
}

// CheckAllProxies tests all configured proxies and returns their status.
func (a *App) CheckAllProxies() []proxy.Status {
	cfg := config.Get()
	proxies := cfg.GetStringSlice("proxies")
	results := make([]proxy.Status, len(proxies))
	now := time.Now().Unix()

	var wg sync.WaitGroup
	for i, p := range proxies {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			r := proxy.CheckHealth(url)
			if r.Alive {
				r.Since = now
			}
			results[idx] = r
		}(i, p)
	}
	wg.Wait()

	// Persist — preserve accumulated bandwidth from previous statuses
	a.proxyStatusMu.Lock()
	oldMap := make(map[string]proxy.Status, len(a.proxyStatuses))
	for _, ps := range a.proxyStatuses {
		oldMap[ps.URL] = ps
	}
	for i, r := range results {
		if old, ok := oldMap[r.URL]; ok {
			results[i].BytesSent = old.BytesSent
			results[i].BytesRecv = old.BytesRecv
			if r.Alive && old.Alive && old.Since > 0 {
				results[i].Since = old.Since
			}
		}
	}
	a.proxyStatuses = results
	a.proxyStatusMu.Unlock()

	return results
}

func (a *App) ExecuteCommand(cmdStr string) string {
	args := strings.Fields(cmdStr)
	if len(args) == 0 {
		return ""
	}

	var buf bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		return fmt.Sprintf("Error: %s", err.Error())
	}

	return buf.String()
}

func (a *App) GetLogs() []string {
	a.logMu.RLock()
	defer a.logMu.RUnlock()
	result := make([]string, len(a.logs))
	copy(result, a.logs)
	return result
}

func (a *App) ClearLogs() {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.logs = a.logs[:0]
	runtime.EventsEmit(a.ctx, "logs:cleared", true)
}

func (a *App) GetPlatformInfo() map[string]interface{} {
	info := relay.GetPlatformInfo()
	return map[string]interface{}{
		"os":        info.OS,
		"arch":      info.Arch,
		"library":   info.LibraryName,
		"supported": info.Supported,
	}
}

func (a *App) GetVersion() map[string]interface{} {
	libVersion := relay.GetLibraryVersion()
	return map[string]interface{}{
		"app":     a.version,
		"library": libVersion,
	}
}

// ── Auto-start methods ──────────────────────────────────

func (a *App) SetLaunchOnStartup(enabled bool) error {
	cfg := config.Get()
	cfg.Set("launch_on_startup", enabled)
	cfg.Set("auto_start", enabled)
	if err := config.Save(); err != nil {
		return err
	}

	if enabled {
		if err := autostart.Enable(); err != nil {
			return fmt.Errorf("failed to enable autostart: %w", err)
		}
	} else {
		if err := autostart.Disable(); err != nil {
			return fmt.Errorf("failed to disable autostart: %w", err)
		}
	}

	runtime.EventsEmit(a.ctx, "config:updated", a.GetConfig())
	return nil
}

func (a *App) GetLaunchOnStartup() bool {
	enabled, err := autostart.IsEnabled()
	if err != nil {
		return false
	}
	return enabled
}

func (a *App) IsWindowMaximised() bool {
	return runtime.WindowIsMaximised(a.ctx)
}

// CloseWindow handles the X button: hide to background, relay keeps running
func (a *App) CloseWindow() {
	// If relay not running and user didn't explicitly stop, start it before hiding
	if !a.isRelayRunning() && !a.userStopped.Load() {
		cfg := config.Get()
		go func() {
			defer sentry.Recover()
			if err := a.StartRelay(cfg.GetString("partner_id")); err != nil {
				log.Error().Err(err).Msg("Auto-start relay on close failed")
			}
		}()
	}
	// Win32 direct hide (Windows), then Wails runtime fallback (macOS/Linux)
	window.HideWindow("UPGO Node")
	runtime.WindowHide(a.ctx)
}

// ShowWindow shows the hidden window (called from second instance signal)
func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}

// QuitApp hides the window — app never exits, always runs in background
func (a *App) QuitApp() {
	window.HideWindow("UPGO Node")
	runtime.WindowHide(a.ctx)
}

// centerAndResize50 sets window to 50% of screen, centered. Cross-platform via Wails runtime.
func (a *App) centerAndResize50() {
	screens, err := runtime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		// Fallback: just center with default size
		runtime.WindowCenter(a.ctx)
		return
	}

	// Use primary screen (first one, or current)
	screen := screens[0]
	for _, s := range screens {
		if s.IsCurrent || s.IsPrimary {
			screen = s
			break
		}
	}

	w := screen.Size.Width * 50 / 100
	h := screen.Size.Height * 50 / 100
	if w < 900 {
		w = 900
	}
	if h < 600 {
		h = 600
	}
	if w > screen.Size.Width {
		w = screen.Size.Width
	}
	if h > screen.Size.Height {
		h = screen.Size.Height
	}

	runtime.WindowSetSize(a.ctx, w, h)
	runtime.WindowCenter(a.ctx)
}

// stopRelay stops and closes the relay manager (all parallel clients).
func (a *App) stopRelay() {
	a.relayMu.Lock()
	defer a.relayMu.Unlock()

	if a.relayMgr != nil {
		_ = a.relayMgr.Stop()
		a.relayMgr.Close()
		a.relayMgr = nil
	}
}

// retryDeadProxies runs every 60s, re-checks dead proxies, and hot-adds any that revive.
func (a *App) retryDeadProxies(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.userStopped.Load() {
				return
			}
			a.doRetryDeadProxies()
		}
	}
}

func (a *App) doRetryDeadProxies() {
	// Collect dead proxies
	a.proxyStatusMu.RLock()
	var deadProxies []proxy.Status
	for _, ps := range a.proxyStatuses {
		if !ps.Alive && ps.Error != "" && ps.Error != "checking" {
			deadProxies = append(deadProxies, ps)
		}
	}
	a.proxyStatusMu.RUnlock()

	if len(deadProxies) == 0 {
		return
	}

	log.Info().Int("count", len(deadProxies)).Msg("Retrying dead proxies")

	// Check dead proxies in parallel
	results := make([]proxy.Status, len(deadProxies))
	var wg sync.WaitGroup
	for i, dp := range deadProxies {
		wg.Add(1)
		go func(idx int, ps proxy.Status) {
			defer wg.Done()
			results[idx] = proxy.CheckHealth(ps.URL)
		}(i, dp)
	}
	wg.Wait()

	// Find revived proxies and update statuses
	var revived []proxy.Status
	now := time.Now().Unix()

	a.proxyStatusMu.Lock()
	for i, r := range results {
		if !r.Alive {
			continue
		}
		r.Since = now
		// Find and update in proxyStatuses by URL
		for j, ps := range a.proxyStatuses {
			if ps.URL == deadProxies[i].URL {
				r.BytesSent = ps.BytesSent
				r.BytesRecv = ps.BytesRecv
				a.proxyStatuses[j] = r
				break
			}
		}
		revived = append(revived, r)
		log.Info().Str("proxy", r.URL).Str("protocol", r.Protocol).Msg("Dead proxy revived")
	}
	statuses := make([]proxy.Status, len(a.proxyStatuses))
	copy(statuses, a.proxyStatuses)
	a.proxyStatusMu.Unlock()

	if len(revived) == 0 {
		log.Debug().Int("dead", len(deadProxies)).Msg("No dead proxies revived")
		return
	}

	// Emit updated statuses to UI
	runtime.EventsEmit(a.ctx, "proxy:status", statuses)

	// Hot-add revived proxies to running relay (skip if user stopped)
	if a.userStopped.Load() {
		return
	}

	a.relayMu.RLock()
	mgr := a.relayMgr
	a.relayMu.RUnlock()

	if mgr != nil {
		proxyURLs := make([]string, 0, len(revived))
		for _, r := range revived {
			proxyURLs = append(proxyURLs, proxy.BuildProxyURL(r.URL, r.Protocol))
		}
		if err := mgr.HotAddProxies(proxyURLs); err != nil {
			log.Error().Err(err).Int("count", len(proxyURLs)).Msg("Failed to hot-add revived proxies")
		} else {
			log.Info().Int("count", len(proxyURLs)).Msg("Revived proxies hot-added to relay")
		}
	}
}
