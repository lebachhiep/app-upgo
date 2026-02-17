package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"relay-app/internal/autostart"
	"relay-app/internal/config"
	"relay-app/internal/proxy"
	"relay-app/internal/relay"
	"relay-app/pkg/relayleaf"
)

var appVersion = "1.0.0"

func SetVersion(v string) {
	appVersion = v
}

func Execute() error {
	return NewRootCmd().Execute()
}

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "upgo-node",
		Short: "UPGO Node - P2P Network Client",
		Long:  "UPGO Node is a BNC network node for earning rewards by sharing bandwidth.",
	}

	rootCmd.AddCommand(
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newStatsCmd(),
		newConfigCmd(),
		newVersionCmd(),
		newDeviceIdCmd(),
		newProxyCmd(),
	)

	return rootCmd
}

func newStartCmd() *cobra.Command {
	var (
		partnerId    string
		daemon       bool
		proxyUrls    []string
		verbose      bool
		discoveryUrl string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the BNC node",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()

			if partnerId == "" {
				partnerId = cfg.GetString("partner_id")
			}
			if partnerId == "" {
				return fmt.Errorf("partner-id is required (use --partner-id or set in config)")
			}

			if verbose {
				cfg.Set("verbose", true)
			}

			isVerbose := cfg.GetBool("verbose")

			// Resolve discovery URL
			discUrl := discoveryUrl
			if discUrl == "" {
				discUrl = cfg.GetString("discovery_url")
			}

			// Collect all proxies (config + CLI flags)
			allProxies := append(cfg.GetStringSlice("proxies"), proxyUrls...)

			// ── Health-check proxies in parallel (like GUI) ──
			var allStatuses []proxy.Status
			if len(allProxies) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Checking proxies...")
				allStatuses = make([]proxy.Status, len(allProxies))
				var wg sync.WaitGroup
				for i, p := range allProxies {
					wg.Add(1)
					go func(idx int, proxyUrl string) {
						defer wg.Done()
						allStatuses[idx] = proxy.CheckHealth(proxyUrl)
					}(i, p)
				}
				wg.Wait()

				for _, ps := range allStatuses {
					status := "FAIL"
					if ps.Alive {
						status = "OK"
					}
					detail := ""
					if ps.Error != "" {
						detail = fmt.Sprintf(" (%s)", ps.Error)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s  proto=%s  latency=%dms%s\n",
						status, ps.URL, ps.Protocol, ps.Latency, detail)
				}
			}

			// ── Create SINGLE SDK client with all proxies ──
			mgr := relay.NewRelayManager()
			mgr.OnLog = func(msg string) {
				if isVerbose {
					fmt.Fprintln(cmd.OutOrStdout(), msg)
				}
			}
			mgr.OnStatusChange = func(connected bool) {
				ts := time.Now().Format("15:04:05")
				if connected {
					fmt.Fprintf(cmd.OutOrStdout(), "[%s] STATUS: CONNECTED\n", ts)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "[%s] STATUS: DISCONNECTED\n", ts)
				}
			}
			mgr.OnStatsUpdate = func(stats *relay.Stats) {
				ts := time.Now().Format("15:04:05")
				connStr := "NO"
				if stats.ConnectedNodes > 0 {
					connStr = "YES"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] up=%ds conn=%s nodes=%d streams=%d/%d sent=%d recv=%d reconn=%d exits=%d\n",
					ts, stats.Uptime, connStr, stats.ConnectedNodes, stats.ActiveStreams, stats.TotalStreams,
					stats.BytesSent, stats.BytesRecv, stats.ReconnectCount, countExitPoints(stats.ExitPointsJSON))
			}

			mgr.OnNeedRestart = func() {
				// Fallback if Restart() fails inside the manager
				ts := time.Now().Format("15:04:05")
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] WATCHDOG: Restart() failed, attempting full restart...\n", ts)
			}

			if err := mgr.Init(isVerbose); err != nil {
				return fmt.Errorf("failed to init node: %w", err)
			}

			if discUrl != "" {
				if err := mgr.SetDiscoveryURL(discUrl); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to set discovery URL: %v\n", err)
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
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to add proxy %s: %v\n", proxyURL, err)
				} else {
					addedCount++
					fmt.Fprintf(cmd.OutOrStdout(), "Added proxy: %s (%s)\n", ps.URL, ps.Protocol)
				}
			}

			if err := mgr.Start(partnerId); err != nil {
				mgr.Close()
				return fmt.Errorf("failed to start node: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\nNode started with partner ID: %s (direct + %d proxies, single client)\n", partnerId, addedCount)

			if daemon || !isTerminal() {
				fmt.Fprintln(cmd.OutOrStdout(), "Running in daemon mode...")
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			fmt.Fprintln(cmd.OutOrStdout(), "\nStopping node...")
			mgr.Close()
			return nil
		},
	}

	cmd.Flags().StringVar(&partnerId, "partner-id", "", "Partner ID for BNC connection")
	cmd.Flags().BoolVar(&daemon, "daemon", false, "Run in daemon mode")
	cmd.Flags().StringSliceVar(&proxyUrls, "proxy", nil, "Proxy URLs (can specify multiple)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Enable verbose logging")
	cmd.Flags().StringVar(&discoveryUrl, "discovery-url", "", "Discovery service URL")

	return cmd
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the BNC node",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "Stop command sent. (Use Ctrl+C in the running instance)")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	var showStats bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show node status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			partnerId := cfg.GetString("partner_id")

			fmt.Fprintln(cmd.OutOrStdout(), "UPGO Node Status")
			fmt.Fprintln(cmd.OutOrStdout(), "─────────────────")
			fmt.Fprintf(cmd.OutOrStdout(), "Partner ID:    %s\n", partnerId)
			fmt.Fprintf(cmd.OutOrStdout(), "Library:       %s\n", relayleaf.Version())
			fmt.Fprintf(cmd.OutOrStdout(), "Platform:      %s/%s\n", relay.GetPlatformInfo().OS, relay.GetPlatformInfo().Arch)

			if showStats {
				fmt.Fprintln(cmd.OutOrStdout(), "\nNote: Live stats available only when node is running via GUI or daemon mode.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&showStats, "stats", false, "Show detailed stats")
	return cmd
}

func newStatsCmd() *cobra.Command {
	var (
		watch    bool
		jsonOut  bool
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show node statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager := relay.NewRelayManager()
			cfg := config.Get()
			partnerId := cfg.GetString("partner_id")

			if partnerId == "" {
				partnerId = "test"
			}

			if err := manager.Init(cfg.GetBool("verbose")); err != nil {
				return err
			}

			if err := manager.Start(partnerId); err != nil {
				return err
			}

			defer manager.Close()

			if watch {
				sigCh := make(chan os.Signal, 1)
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()

				for {
					select {
					case <-sigCh:
						fmt.Fprintln(cmd.OutOrStdout())
						return nil
					case <-ticker.C:
						printStats(cmd, manager, jsonOut)
					}
				}
			}

			time.Sleep(1 * time.Second)
			printStats(cmd, manager, jsonOut)
			return nil
		},
	}

	cmd.Flags().BoolVar(&watch, "watch", false, "Watch stats in real-time")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output in JSON format")
	return cmd
}

func printStats(cmd *cobra.Command, manager *relay.RelayManager, jsonOut bool) {
	status := manager.GetStatus()
	if status.Stats == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "No stats available")
		return
	}

	if jsonOut {
		data, _ := json.MarshalIndent(status.Stats, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
	} else {
		s := status.Stats
		fmt.Fprintf(cmd.OutOrStdout(), "Bytes Sent:      %d\n", s.BytesSent)
		fmt.Fprintf(cmd.OutOrStdout(), "Bytes Received:  %d\n", s.BytesRecv)
		fmt.Fprintf(cmd.OutOrStdout(), "Connections:     %d\n", s.Connections)
		fmt.Fprintf(cmd.OutOrStdout(), "Active Streams:  %d\n", s.ActiveStreams)
		fmt.Fprintf(cmd.OutOrStdout(), "Total Streams:   %d\n", s.TotalStreams)
		fmt.Fprintf(cmd.OutOrStdout(), "Uptime:          %ds\n", s.Uptime)
		fmt.Fprintf(cmd.OutOrStdout(), "Connected:       %v\n", status.Connected)
	}
}

func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := config.NormalizeKey(args[0])
			value := args[1]

			cfg := config.Get()
			cfg.Set(key, value)
			if err := config.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			// Handle launch_on_startup: register/unregister system autostart (like GUI)
			if key == "launch_on_startup" {
				enabled := strings.EqualFold(value, "true") || value == "1"
				if enabled {
					if err := autostart.Enable(); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to enable autostart: %v\n", err)
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), "System autostart enabled")
					}
				} else {
					if err := autostart.Disable(); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to disable autostart: %v\n", err)
					} else {
						fmt.Fprintln(cmd.OutOrStdout(), "System autostart disabled")
					}
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Config set: %s = %s\n", key, value)
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show all config values",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			fmt.Fprintln(cmd.OutOrStdout(), "Configuration")
			fmt.Fprintln(cmd.OutOrStdout(), "─────────────")
			fmt.Fprintf(cmd.OutOrStdout(), "partner_id:    %s\n", cfg.GetString("partner_id"))
			fmt.Fprintf(cmd.OutOrStdout(), "discovery_url: %s\n", cfg.GetString("discovery_url"))
			fmt.Fprintf(cmd.OutOrStdout(), "proxies:       %s\n", strings.Join(cfg.GetStringSlice("proxies"), ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "verbose:            %v\n", cfg.GetBool("verbose"))
			fmt.Fprintf(cmd.OutOrStdout(), "auto_start:         %v\n", cfg.GetBool("auto_start"))
			fmt.Fprintf(cmd.OutOrStdout(), "launch_on_startup:  %v\n", cfg.GetBool("launch_on_startup"))
			fmt.Fprintf(cmd.OutOrStdout(), "log_level:          %s\n", cfg.GetString("log_level"))
			fmt.Fprintf(cmd.OutOrStdout(), "config_file:   %s\n", cfg.ConfigFileUsed())
			return nil
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			key := config.NormalizeKey(args[0])
			fmt.Fprintln(cmd.OutOrStdout(), cfg.GetString(key))
			return nil
		},
	}

	configCmd.AddCommand(setCmd, showCmd, getCmd)
	return configCmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform := relay.GetPlatformInfo()
			fmt.Fprintf(cmd.OutOrStdout(), "UPGO Node v%s\n", appVersion)
			fmt.Fprintf(cmd.OutOrStdout(), "Library:  %s\n", relayleaf.Version())
			fmt.Fprintf(cmd.OutOrStdout(), "Platform: %s/%s\n", platform.OS, platform.Arch)
			fmt.Fprintf(cmd.OutOrStdout(), "Lib file: %s\n", platform.LibraryName)
			return nil
		},
	}
}

func newDeviceIdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "device-id",
		Short: "Show device ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := relayleaf.NewClient(false)
			if err != nil {
				return err
			}
			defer client.Close()
			fmt.Fprintln(cmd.OutOrStdout(), client.GetDeviceID())
			return nil
		},
	}
}

func newProxyCmd() *cobra.Command {
	proxyCmd := &cobra.Command{
		Use:   "proxy",
		Short: "Manage proxy configuration",
	}

	addCmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Add a proxy (auto-detects protocol)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			normalized := proxy.NormalizeURL(args[0])
			cfg := config.Get()
			proxies := cfg.GetStringSlice("proxies")

			for _, p := range proxies {
				if p == normalized {
					return fmt.Errorf("proxy already exists: %s", normalized)
				}
			}

			// Auto-check health and detect protocol (like GUI)
			fmt.Fprintf(cmd.OutOrStdout(), "Checking %s ...\n", normalized)
			result := proxy.CheckHealth(normalized)

			if result.Alive {
				fmt.Fprintf(cmd.OutOrStdout(), "  Status:   OK\n")
				fmt.Fprintf(cmd.OutOrStdout(), "  Protocol: %s\n", result.Protocol)
				fmt.Fprintf(cmd.OutOrStdout(), "  Latency:  %dms\n", result.Latency)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "  Status:   FAIL (%s)\n", result.Error)
				fmt.Fprintln(cmd.OutOrStdout(), "  Warning:  proxy saved but may not work at runtime")
			}

			proxies = append(proxies, normalized)
			cfg.Set("proxies", proxies)
			if err := config.Save(); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Proxy added: %s\n", normalized)
			return nil
		},
	}

	var listCheck bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List proxies (use --check to test health)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			proxies := cfg.GetStringSlice("proxies")

			if len(proxies) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No proxies configured")
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Configured Proxies:")
			for i, p := range proxies {
				if listCheck {
					result := proxy.CheckHealth(p)
					status := "FAIL"
					if result.Alive {
						status = "OK"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s  [%s] proto=%s latency=%dms\n",
						i+1, p, status, result.Protocol, result.Latency)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "  %d. %s\n", i+1, p)
				}
			}
			return nil
		},
	}
	listCmd.Flags().BoolVar(&listCheck, "check", false, "Check health of each proxy")

	removeCmd := &cobra.Command{
		Use:   "remove <url>",
		Short: "Remove a proxy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Get()
			proxies := cfg.GetStringSlice("proxies")
			newProxies := make([]string, 0, len(proxies))
			found := false

			for _, p := range proxies {
				if p == args[0] {
					found = true
				} else {
					newProxies = append(newProxies, p)
				}
			}

			if !found {
				return fmt.Errorf("proxy not found: %s", args[0])
			}

			cfg.Set("proxies", newProxies)
			if err := config.Save(); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Proxy removed: %s\n", args[0])
			return nil
		},
	}

	checkCmd := &cobra.Command{
		Use:   "check [url]",
		Short: "Check proxy health (all configured, or specific URL)",
		RunE: func(cmd *cobra.Command, args []string) error {
			var targets []string
			if len(args) > 0 {
				targets = args
			} else {
				cfg := config.Get()
				targets = cfg.GetStringSlice("proxies")
			}

			if len(targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No proxies to check")
				return nil
			}

			for _, t := range targets {
				result := proxy.CheckHealth(t)
				status := "FAIL"
				if result.Alive {
					status = "OK"
				}
				detail := ""
				if result.Error != "" {
					detail = fmt.Sprintf(" (%s)", result.Error)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s  proto=%s  latency=%dms%s\n",
					status, result.URL, result.Protocol, result.Latency, detail)
			}
			return nil
		},
	}

	proxyCmd.AddCommand(addCmd, listCmd, removeCmd, checkCmd)
	return proxyCmd
}

func countExitPoints(exitPointsJSON string) int {
	if exitPointsJSON == "" {
		return 0
	}
	var arr []interface{}
	if err := json.Unmarshal([]byte(exitPointsJSON), &arr); err != nil {
		return 0
	}
	return len(arr)
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
