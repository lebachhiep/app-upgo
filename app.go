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

type App struct {
	ctx           context.Context
	version       string
	manager       *relay.RelayManager // control manager (EnsureLibrary only, never Started)
	relayMgr      *relay.RelayManager // single SDK client with all proxies
	relayMu       sync.RWMutex
	relayStarting bool                        // true while StartRelay is in progress
	lastStats     atomic.Pointer[relay.Stats] // latest stats from single client
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
	a.stopRelay()
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

	// Create SINGLE SDK client with all proxies
	mgr := relay.NewRelayManager()
	mgr.OnLog = func(msg string) {
		a.addLog(msg)
		runtime.EventsEmit(a.ctx, "log:new", msg)
	}
	mgr.OnStatsUpdate = func(stats *relay.Stats) {
		a.lastStats.Store(stats)
		runtime.EventsEmit(a.ctx, "stats:update", stats)
	}
	mgr.OnStatusChange = func(connected bool) {
		runtime.EventsEmit(a.ctx, "status:change", connected)
	}
	mgr.OnNeedRestart = func() {
		// Fallback: Restart() inside the manager failed, do a full StartRelay
		cfg := config.Get()
		pid := cfg.GetString("partner_id")
		if pid != "" {
			log.Info().Msg("Watchdog fallback: full relay restart")
			if err := a.StartRelay(pid); err != nil {
				log.Error().Err(err).Msg("Watchdog fallback: relay restart failed")
			}
		}
	}

	if err := mgr.Init(verbose); err != nil {
		return fmt.Errorf("failed to init node: %w", err)
	}

	if discoveryUrl != "" {
		if err := mgr.SetDiscoveryURL(discoveryUrl); err != nil {
			log.Warn().Err(err).Msg("Failed to set discovery URL")
		}
	}

	// Add all alive proxies to the single client
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

	if err := mgr.Start(partnerId); err != nil {
		mgr.Close()
		return fmt.Errorf("failed to start node: %w", err)
	}

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

	log.Info().Int("proxies_added", addedCount).Int("proxies_total", len(proxies)).Msg("Single SDK client started with all proxies")

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

func (a *App) StopRelay() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.stopRelay()

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

	// Clear proxy statuses
	a.proxyStatusMu.Lock()
	a.proxyStatuses = nil
	a.proxyStatusMu.Unlock()

	runtime.EventsEmit(a.ctx, "proxy:status", []proxy.Status{})
	runtime.EventsEmit(a.ctx, "proxies:updated", newProxies)

	// Restart relay with updated proxy list (single client must be recreated)
	partnerId := cfg.GetString("partner_id")
	if partnerId != "" && a.isRelayRunning() {
		go func() {
			if err := a.StartRelay(partnerId); err != nil {
				log.Error().Err(err).Msg("Failed to restart relay after proxy removal")
			}
		}()
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

// stopRelay stops and closes the single relay manager.
func (a *App) stopRelay() {
	a.relayMu.Lock()
	defer a.relayMu.Unlock()

	if a.relayMgr != nil {
		_ = a.relayMgr.Stop()
		a.relayMgr.Close()
		a.relayMgr = nil
	}
}
