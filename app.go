package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// proxyMgrEntry wraps a per-proxy RelayManager with its index and latest stats.
type proxyMgrEntry struct {
	mgr       *relay.RelayManager
	proxyIdx  int                         // index in proxyStatuses[], -1 if no proxy
	lastStats atomic.Pointer[relay.Stats] // latest stats from this manager (atomic for concurrent access)
}

type App struct {
	ctx           context.Context
	version       string
	manager       *relay.RelayManager // control manager (EnsureLibrary only, never Started)
	directEntry   *proxyMgrEntry      // always-on direct (no proxy) SDK instance
	proxyMgrs     []*proxyMgrEntry    // one per alive proxy (each has own SDK client)
	proxyMgrsMu   sync.RWMutex
	mu            sync.RWMutex
	logs          []string
	logMu         sync.RWMutex
	silentMode    bool
	proxyStatuses []proxy.Status
	proxyStatusMu sync.RWMutex
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
		// Install WM_GETMINMAXINFO handler first (retry until window is ready)
		for i := 0; i < 10; i++ {
			time.Sleep(100 * time.Millisecond)
			if err := window.ConstrainToScreen("UPGO Node"); err == nil {
				log.Info().Msg("Window constrained to screen")
				break
			}
		}

		if a.silentMode {
			// Wait for Wails message loop + WebView2 to initialize, then hide.
			time.Sleep(500 * time.Millisecond)
			runtime.WindowHide(a.ctx)
		} else {
			// Cross-platform: use Wails runtime to get screen size, resize to 50%, center
			a.centerAndResize50()
			time.Sleep(300 * time.Millisecond)
			a.centerAndResize50()
			log.Info().Msg("Window centered and resized to 50%")
		}
	}()
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	// If relay not running, start it before hiding
	if !a.isRelayRunning() {
		cfg := config.Get()
		go func() {
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
	a.stopAllProxyMgrs()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.manager != nil {
		a.manager.Close()
	}
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
	a.mu.Lock()
	defer a.mu.Unlock()

	// Stop any existing proxy managers first
	a.stopAllProxyMgrs()

	cfg := config.Get()
	verbose := cfg.GetBool("verbose")
	discoveryUrl := cfg.GetString("discovery_url")

	// Check all proxies before starting — emit status events for UI
	proxies := cfg.GetStringSlice("proxies")
	var allStatuses []proxy.Status

	if len(proxies) > 0 {
		allStatuses = make([]proxy.Status, len(proxies))
		for i, p := range proxies {
			allStatuses[i] = proxy.Status{URL: p, Error: "checking"}
		}
		runtime.EventsEmit(a.ctx, "proxy:status", allStatuses)

		// Check in parallel — auto-detects protocol
		var wg sync.WaitGroup
		for i, p := range proxies {
			wg.Add(1)
			go func(idx int, proxyUrl string) {
				defer wg.Done()
				allStatuses[idx] = proxy.CheckHealth(proxyUrl)
				runtime.EventsEmit(a.ctx, "proxy:status", allStatuses)
			}(i, p)
		}
		wg.Wait()

		now := time.Now().Unix()
		for i, ps := range allStatuses {
			if ps.Alive {
				allStatuses[i].Since = now
			} else {
				log.Warn().Str("proxy", ps.URL).Str("error", ps.Error).Msg("Proxy dead, skipping")
			}
		}

		// Persist statuses for dashboard
		a.proxyStatusMu.Lock()
		a.proxyStatuses = allStatuses
		a.proxyStatusMu.Unlock()
		runtime.EventsEmit(a.ctx, "proxy:status", allStatuses)
	}

	// Always create a direct (no proxy) SDK instance
	a.proxyMgrsMu.Lock()
	a.proxyMgrs = nil
	a.directEntry = a.createDirectMgrEntry(verbose, discoveryUrl, partnerId)
	if a.directEntry == nil {
		a.proxyMgrsMu.Unlock()
		return fmt.Errorf("failed to create direct node instance")
	}

	// Create one SDK instance per alive proxy
	startedCount := 0
	for i, ps := range allStatuses {
		if !ps.Alive {
			continue
		}
		proxyURL := proxy.BuildProxyURL(ps.URL, ps.Protocol)
		entry := a.createProxyMgrEntry(i, proxyURL, verbose, discoveryUrl, partnerId)
		if entry != nil {
			a.proxyMgrs = append(a.proxyMgrs, entry)
			startedCount++
		} else {
			log.Warn().Str("proxy", ps.URL).Msg("Failed to create manager for proxy")
		}
	}
	a.proxyMgrsMu.Unlock()

	log.Info().Int("proxies_started", startedCount).Int("proxies_total", len(proxies)).Msg("Node instances started (+ direct)")

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
	return nil
}

// createProxyMgrEntry creates a new RelayManager for a single proxy, inits and starts it.
// proxyIdx is the index in proxyStatuses[] (-1 if no proxy).
func (a *App) createProxyMgrEntry(proxyIdx int, proxyURL string, verbose bool, discoveryUrl, partnerId string) *proxyMgrEntry {
	mgr := relay.NewRelayManager()
	entry := &proxyMgrEntry{mgr: mgr, proxyIdx: proxyIdx}

	mgr.OnLog = func(msg string) {
		a.addLog(msg)
		runtime.EventsEmit(a.ctx, "log:new", msg)
	}

	mgr.OnStatsUpdate = func(stats *relay.Stats) {
		entry.lastStats.Store(stats)
		// Update per-proxy bandwidth directly from this manager's stats
		if proxyIdx >= 0 {
			a.proxyStatusMu.Lock()
			if proxyIdx < len(a.proxyStatuses) {
				a.proxyStatuses[proxyIdx].BytesSent = stats.BytesSent
				a.proxyStatuses[proxyIdx].BytesRecv = stats.BytesRecv
			}
			statuses := make([]proxy.Status, len(a.proxyStatuses))
			copy(statuses, a.proxyStatuses)
			a.proxyStatusMu.Unlock()
			runtime.EventsEmit(a.ctx, "proxy:status", statuses)
		}
		// Emit aggregated stats for the dashboard
		a.emitAggregateStats()
	}

	mgr.OnStatusChange = func(connected bool) {
		// Emit aggregate connected status
		a.emitAggregateConnected()
	}

	if err := mgr.Init(verbose); err != nil {
		log.Warn().Err(err).Str("proxy", proxyURL).Msg("Failed to init node manager")
		return nil
	}

	if discoveryUrl != "" {
		if err := mgr.SetDiscoveryURL(discoveryUrl); err != nil {
			log.Warn().Err(err).Msg("Failed to set discovery URL")
		}
	}

	if proxyURL != "" {
		if err := mgr.AddProxy(proxyURL); err != nil {
			log.Warn().Err(err).Str("proxy", proxyURL).Msg("Failed to add proxy")
			mgr.Close()
			return nil
		}
	}

	if err := mgr.Start(partnerId); err != nil {
		log.Warn().Err(err).Str("proxy", proxyURL).Msg("Failed to start node manager")
		mgr.Close()
		return nil
	}

	return entry
}

// createDirectMgrEntry creates a direct (no proxy) RelayManager.
// Its stats are emitted via "direct:stats" so the UI can show a Direct row.
func (a *App) createDirectMgrEntry(verbose bool, discoveryUrl, partnerId string) *proxyMgrEntry {
	mgr := relay.NewRelayManager()
	entry := &proxyMgrEntry{mgr: mgr, proxyIdx: -1}

	mgr.OnLog = func(msg string) {
		a.addLog(msg)
		runtime.EventsEmit(a.ctx, "log:new", msg)
	}

	mgr.OnStatsUpdate = func(stats *relay.Stats) {
		entry.lastStats.Store(stats)
		runtime.EventsEmit(a.ctx, "direct:stats", stats)
		a.emitAggregateStats()
	}

	mgr.OnStatusChange = func(connected bool) {
		a.emitAggregateConnected()
	}

	if err := mgr.Init(verbose); err != nil {
		log.Warn().Err(err).Msg("Failed to init direct node manager")
		return nil
	}

	if discoveryUrl != "" {
		if err := mgr.SetDiscoveryURL(discoveryUrl); err != nil {
			log.Warn().Err(err).Msg("Failed to set discovery URL for direct manager")
		}
	}

	if err := mgr.Start(partnerId); err != nil {
		log.Warn().Err(err).Msg("Failed to start direct node manager")
		mgr.Close()
		return nil
	}

	return entry
}

func (a *App) StopRelay() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stopAllProxyMgrs()

	runtime.EventsEmit(a.ctx, "relay:stopped", true)
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
	}

	a.proxyMgrsMu.RLock()
	defer a.proxyMgrsMu.RUnlock()

	if a.directEntry == nil && len(a.proxyMgrs) == 0 {
		return resp, nil
	}

	var totalSent, totalRecv, maxUptime, totalStreams, reconnectCount int64
	var activeStreams, connectedNodes int32
	anyConnected := false

	// Include direct entry
	if a.directEntry != nil {
		status := a.directEntry.mgr.GetStatus()
		if status.Connected {
			anyConnected = true
		}
		if resp.DeviceId == "" && status.DeviceId != "" {
			resp.DeviceId = status.DeviceId
		}
		if resp.Version == "" && status.Version != "" {
			resp.Version = status.Version
		}
		if status.Stats != nil {
			totalSent += status.Stats.BytesSent
			totalRecv += status.Stats.BytesRecv
			maxUptime = status.Stats.Uptime
			totalStreams += status.Stats.TotalStreams
			activeStreams += status.Stats.ActiveStreams
			connectedNodes += status.Stats.ConnectedNodes
			reconnectCount += status.Stats.ReconnectCount
		}
	}

	// Include proxy entries
	for _, entry := range a.proxyMgrs {
		status := entry.mgr.GetStatus()
		if status.Connected {
			anyConnected = true
		}
		if resp.DeviceId == "" && status.DeviceId != "" {
			resp.DeviceId = status.DeviceId
		}
		if resp.Version == "" && status.Version != "" {
			resp.Version = status.Version
		}
		if status.Stats != nil {
			totalSent += status.Stats.BytesSent
			totalRecv += status.Stats.BytesRecv
			if status.Stats.Uptime > maxUptime {
				maxUptime = status.Stats.Uptime
			}
			totalStreams += status.Stats.TotalStreams
			activeStreams += status.Stats.ActiveStreams
			connectedNodes += status.Stats.ConnectedNodes
			reconnectCount += status.Stats.ReconnectCount
		}
	}

	resp.IsConnected = anyConnected
	resp.Stats = &relay.Stats{
		BytesSent:      totalSent,
		BytesRecv:      totalRecv,
		Uptime:         maxUptime,
		TotalStreams:   totalStreams,
		ActiveStreams:  activeStreams,
		ConnectedNodes: connectedNodes,
		ReconnectCount: reconnectCount,
		Timestamp:      time.Now().Unix(),
	}

	return resp, nil
}

func (a *App) IsRelayRunning() bool {
	return a.isRelayRunning()
}

func (a *App) isRelayRunning() bool {
	a.proxyMgrsMu.RLock()
	defer a.proxyMgrsMu.RUnlock()
	return a.directEntry != nil || len(a.proxyMgrs) > 0
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

	// Stop proxy managers and clear statuses (proxy indices shift, must rebuild)
	a.stopProxyMgrsOnly()
	a.proxyStatusMu.Lock()
	a.proxyStatuses = nil
	a.proxyStatusMu.Unlock()

	runtime.EventsEmit(a.ctx, "proxy:status", []proxy.Status{})
	runtime.EventsEmit(a.ctx, "proxies:updated", newProxies)
	return nil
}

func (a *App) RemoveAllProxies() error {
	cfg := config.Get()
	cfg.Set("proxies", []string{})
	if err := config.Save(); err != nil {
		return err
	}

	// Stop proxy managers and clear statuses
	a.stopProxyMgrsOnly()
	a.proxyStatusMu.Lock()
	a.proxyStatuses = nil
	a.proxyStatusMu.Unlock()

	runtime.EventsEmit(a.ctx, "proxy:status", []proxy.Status{})
	runtime.EventsEmit(a.ctx, "proxies:updated", []string{})
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
	// If relay not running, start it before hiding
	if !a.isRelayRunning() {
		cfg := config.Get()
		go func() {
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

// stopAllProxyMgrs stops and closes all per-proxy relay managers.
func (a *App) stopAllProxyMgrs() {
	a.proxyMgrsMu.Lock()
	defer a.proxyMgrsMu.Unlock()

	if a.directEntry != nil {
		_ = a.directEntry.mgr.Stop()
		a.directEntry.mgr.Close()
		a.directEntry = nil
	}

	for _, entry := range a.proxyMgrs {
		_ = entry.mgr.Stop()
		entry.mgr.Close()
	}
	a.proxyMgrs = nil
}

// stopProxyMgrsOnly stops proxy managers but keeps the direct entry running.
func (a *App) stopProxyMgrsOnly() {
	a.proxyMgrsMu.Lock()
	defer a.proxyMgrsMu.Unlock()

	for _, entry := range a.proxyMgrs {
		_ = entry.mgr.Stop()
		entry.mgr.Close()
	}
	a.proxyMgrs = nil
}

// emitAggregateStats sums stats from all managers (direct + proxies) and emits as stats:update.
func (a *App) emitAggregateStats() {
	a.proxyMgrsMu.RLock()
	defer a.proxyMgrsMu.RUnlock()

	var agg relay.Stats
	// Include direct entry
	if a.directEntry != nil {
		if ds := a.directEntry.lastStats.Load(); ds != nil {
			agg.BytesSent += ds.BytesSent
			agg.BytesRecv += ds.BytesRecv
			agg.Uptime = ds.Uptime
			agg.TotalStreams += ds.TotalStreams
			agg.ActiveStreams += ds.ActiveStreams
			agg.ConnectedNodes += ds.ConnectedNodes
			agg.ReconnectCount += ds.ReconnectCount
		}
	}
	// Include proxy entries
	for _, entry := range a.proxyMgrs {
		if ls := entry.lastStats.Load(); ls != nil {
			agg.BytesSent += ls.BytesSent
			agg.BytesRecv += ls.BytesRecv
			if ls.Uptime > agg.Uptime {
				agg.Uptime = ls.Uptime
			}
			agg.TotalStreams += ls.TotalStreams
			agg.ActiveStreams += ls.ActiveStreams
			agg.ConnectedNodes += ls.ConnectedNodes
			agg.ReconnectCount += ls.ReconnectCount
		}
	}
	agg.Timestamp = time.Now().Unix()
	runtime.EventsEmit(a.ctx, "stats:update", &agg)
}

// emitAggregateConnected checks if any manager (direct + proxies) is connected and emits status:change.
func (a *App) emitAggregateConnected() {
	a.proxyMgrsMu.RLock()
	defer a.proxyMgrsMu.RUnlock()

	anyConnected := false
	if a.directEntry != nil {
		status := a.directEntry.mgr.GetStatus()
		if status.Connected {
			anyConnected = true
		}
	}
	if !anyConnected {
		for _, entry := range a.proxyMgrs {
			status := entry.mgr.GetStatus()
			if status.Connected {
				anyConnected = true
				break
			}
		}
	}
	runtime.EventsEmit(a.ctx, "status:change", anyConnected)
}
