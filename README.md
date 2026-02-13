<p align="center">
  <img src="build/appicon.png" width="128" height="128" alt="UPGO Node">
</p>

<h1 align="center">UPGO Node</h1>

<p align="center">
  <strong>Earn rewards by sharing your idle bandwidth through the P2P relay network.</strong>
</p>

<p align="center">
  <a href="#-quick-start">Quick Start</a>&nbsp;&nbsp;|&nbsp;&nbsp;
  <a href="#-cli-reference">CLI Reference</a>&nbsp;&nbsp;|&nbsp;&nbsp;
  <a href="#-proxy-management">Proxy</a>&nbsp;&nbsp;|&nbsp;&nbsp;
  <a href="#%EF%B8%8F-build">Build</a>&nbsp;&nbsp;|&nbsp;&nbsp;
  <a href="#-architecture">Architecture</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Wails-v2.11-5C2D91?logo=webassembly&logoColor=white" alt="Wails">
  <img src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=white" alt="React">
  <img src="https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white" alt="TypeScript">
  <img src="https://img.shields.io/badge/Ant_Design-6-0170FE?logo=antdesign&logoColor=white" alt="Ant Design">
  <img src="https://img.shields.io/badge/License-MIT-green" alt="License">
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Windows-x64_%7C_x86-0078D6?logo=windows&logoColor=white" alt="Windows">
  <img src="https://img.shields.io/badge/macOS-Intel_%7C_Apple_Silicon-000000?logo=apple&logoColor=white" alt="macOS">
  <img src="https://img.shields.io/badge/Linux-amd64_%7C_arm64-FCC624?logo=linux&logoColor=black" alt="Linux">
</p>

---

## Features

| | Feature | Description |
|---|---------|-------------|
| :desktop_computer: | **GUI + CLI** | Full graphical dashboard or headless command-line operation |
| :chart_with_upwards_trend: | **Real-time Stats** | Live bandwidth, connections, streams, uptime with Recharts |
| :globe_with_meridians: | **Proxy Support** | SOCKS5, HTTP, HTTPS with auto-detection and health checking |
| :arrows_counterclockwise: | **Direct + Proxy** | Always maintains a direct connection alongside proxy connections |
| :rocket: | **Auto-start** | Launch on boot (LaunchAgent / Registry / XDG) |
| :ghost: | **Silent Mode** | Background operation with `--silent`, show GUI on re-launch |
| :lock: | **Single Instance** | Mutex lock; second launch shows existing window |
| :package: | **Embedded Library** | Native relay library embedded at build time, auto-updates via SHA256 |
| :file_folder: | **Self-install** | Auto-installs to proper OS location when run from ZIP/temp |
| :keyboard: | **Built-in Terminal** | xterm.js terminal emulator in the GUI |

---

## Quick Start

### Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| **Go** | 1.24+ | [go.dev/dl](https://go.dev/dl/) |
| **Node.js** | 20+ | [nodejs.org](https://nodejs.org/) |
| **Wails CLI** | 2.11.0 | `go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0` |

<details>
<summary><strong>macOS</strong> — Xcode Command Line Tools</summary>

```bash
xcode-select --install
```
</details>

<details>
<summary><strong>Linux (Debian/Ubuntu)</strong> — GTK3 + WebKit2GTK</summary>

```bash
sudo apt-get install -y build-essential libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
```
</details>

<details>
<summary><strong>Windows</strong> — WebView2 + GCC</summary>

- [WebView2 Runtime](https://developer.microsoft.com/en-us/microsoft-edge/webview2/) (pre-installed on Windows 10/11)
- GCC via [MSYS2](https://www.msys2.org/) or [TDM-GCC](https://jmeubank.github.io/tdm-gcc/)
</details>

### Install & Run

```bash
git clone https://github.com/lebachhiep/app-upgo.git
cd app-upgo

# Development (hot-reload)
wails dev

# Production build
wails build -ldflags "-s -w -X main.version=1.0.0"
```

| OS | Output |
|----|--------|
| macOS | `build/bin/upgo-node.app` |
| Windows | `build/bin/upgo-node.exe` |
| Linux | `build/bin/upgo-node` |

---

## CLI Reference

Run with a subcommand for CLI mode, or without arguments to launch GUI.

### Node Control

```bash
upgo-node start --partner-id YOUR_ID                        # Start the node
upgo-node start --partner-id YOUR_ID --daemon               # Daemon mode
upgo-node start --partner-id YOUR_ID --verbose              # Verbose logging
upgo-node start --partner-id YOUR_ID --proxy socks5://x:y   # With extra proxy
upgo-node start --discovery-url https://custom.url          # Custom discovery
upgo-node stop                                               # Stop the node
upgo-node status                                             # Show status
upgo-node status --stats                                     # Status with live stats
upgo-node stats --watch                                      # Live stats
upgo-node stats --json                                       # JSON output
upgo-node version                                            # Version info
upgo-node device-id                                          # Show device ID
```

### Configuration

```bash
upgo-node config show                                   # Show all config
upgo-node config set partner_id YOUR_PARTNER_ID         # Set partner ID
upgo-node config set auto_start true                    # Auto-start relay on app open
upgo-node config set launch_on_startup true             # Launch on system boot
upgo-node config set log_level debug                    # Set log level
upgo-node config get partner_id                         # Get a value
```

**Config keys:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `partner_id` | string | `""` | Partner ID for BNC connection |
| `discovery_url` | string | `""` | Custom discovery server URL |
| `proxies` | string[] | `[]` | List of proxy URLs |
| `verbose` | bool | `false` | Verbose logging |
| `auto_start` | bool | `true` | Auto-start relay when app opens |
| `launch_on_startup` | bool | `true` | Launch app on system boot |
| `log_level` | string | `"info"` | Log level: debug / info / warn / error |

Config file: `~/.relay-app/config.yaml`

---

## Proxy Management

UPGO Node supports **SOCKS5**, **HTTP**, and **HTTPS** proxies. Protocol is auto-detected when adding.

### Add a proxy (auto-checks health & protocol)

```bash
upgo-node proxy add 10.0.0.1:1080                           # Auto-detect protocol
upgo-node proxy add socks5://10.0.0.1:1080                   # SOCKS5 explicit
upgo-node proxy add http://proxy.com:8080                    # HTTP
upgo-node proxy add https://proxy.com:443                    # HTTPS
upgo-node proxy add socks5://user:pass@10.0.0.1:1080         # SOCKS5 with auth
upgo-node proxy add http://user:pass@proxy.com:8080          # HTTP with auth
upgo-node proxy add https://user:pass@secure.proxy.com:443   # HTTPS with auth
upgo-node proxy add user:pass@10.0.0.1:1080                  # Auth without scheme (auto-detect)
upgo-node proxy add 10.0.0.1:1080:myuser:mypass              # Legacy 4-part format
```

Output:
```
Checking 10.0.0.1:1080 ...
  Status:   OK
  Protocol: socks5
  Latency:  45ms
Proxy added: 10.0.0.1:1080
```

### Other commands

```bash
upgo-node proxy list                  # List all proxies
upgo-node proxy list --check          # List with health check
upgo-node proxy check                 # Check all configured proxies
upgo-node proxy check 10.0.0.1:1080   # Check specific proxy
upgo-node proxy remove 10.0.0.1:1080  # Remove a proxy
```

### Supported protocols

| Protocol | Example | Default Port |
|----------|---------|:------------:|
| **SOCKS5** | `socks5://host:1080` | 1080 |
| **HTTP** | `http://host:8080` | 8080 |
| **HTTPS** | `https://host:443` | 443 |
| **Auto** | `host:port` | tries SOCKS5 -> HTTP -> HTTPS |

### Authentication

All proxy protocols support `username:password` authentication. Credentials are embedded in the proxy URL:

```
scheme://username:password@host:port
```

**Supported auth formats:**

| Format | Example | Description |
|--------|---------|-------------|
| **Standard URL** | `socks5://user:pass@host:1080` | Recommended format |
| **HTTP auth** | `http://user:pass@host:8080` | HTTP proxy with auth |
| **HTTPS auth** | `https://user:pass@host:443` | HTTPS proxy with auth |
| **No scheme** | `user:pass@host:1080` | Auto-detect protocol |
| **Legacy 4-part** | `host:port:user:pass` | Converted to `scheme://user:pass@host:port` |

**Notes:**
- Credentials with special characters should be URL-encoded (e.g., `p%40ss` for `p@ss`)
- Auth is passed via SOCKS5 handshake (for SOCKS5) or `Proxy-Authorization` header (for HTTP/HTTPS)
- Proxies without auth work as well — simply omit the `user:pass@` part

### How proxy works at runtime

When the node starts (GUI or CLI), it:

1. Creates a **direct** (no-proxy) SDK instance — always running
2. Health-checks each configured proxy in parallel
3. Creates one SDK instance per **alive** proxy
4. Aggregates stats from all instances (direct + proxies)
5. Dead proxies are skipped with a warning, not fatal

---

## Build

### Makefile targets

```bash
make dev                # Development with hot-reload
make build              # Build for current platform
make build-darwin       # macOS universal (Intel + Apple Silicon)
make build-windows-x64  # Windows 64-bit
make build-windows-x86  # Windows 32-bit
make build-linux-amd64  # Linux x86_64
make build-linux-arm64  # Linux ARM64
make test               # Run basic tests
make clean              # Remove build artifacts
```

### Docker (Linux)

```bash
# Build
docker build -f Dockerfile.linux -t upgo-linux .

# Test CLI
docker run --rm upgo-linux upgo-node version
docker run --rm upgo-linux upgo-node help

# Test GUI (headless)
docker run --rm upgo-linux bash -c \
  "Xvfb :99 -screen 0 1024x768x24 &>/dev/null & \
   export DISPLAY=:99; dbus-launch upgo-node"
```

### CI/CD

Push a version tag to trigger automated builds for all 6 targets:

```bash
git tag v1.0.0 && git push origin v1.0.0
```

---

## Architecture

```
upgo-node/
|-- main.go                       # Entry: CLI vs GUI routing, single-instance lock
|-- app.go                        # Wails lifecycle, relay orchestration
|-- show_signal_unix.go           # SIGUSR1 handler (macOS/Linux)
|-- show_signal_windows.go        # Signal stub (Windows)
|
|-- internal/
|   |-- cli/commands.go           # Cobra CLI commands
|   |-- relay/
|   |   |-- client.go             # RelayManager: init, start, stop, poll stats
|   |   |-- platform.go           # Platform detection (OS, arch, library name)
|   |   +-- helpers.go            # Library version helper
|   |-- config/config.go          # Viper config (YAML ~/.relay-app/)
|   |-- proxy/check.go            # Proxy health check (SOCKS5/HTTP/HTTPS)
|   |-- autostart/
|   |   |-- autostart_darwin.go   # macOS LaunchAgent plist
|   |   |-- autostart_linux.go    # Linux XDG .desktop
|   |   +-- autostart_windows.go  # Windows Registry
|   |-- singleinstance/
|   |   |-- errors.go                 # ErrAlreadyRunning error
|   |   |-- singleinstance_unix.go    # flock + PID + SIGUSR1
|   |   +-- singleinstance_windows.go # Windows Mutex
|   |-- selfinstall/
|   |   |-- selfinstall.go        # Self-install logic (copy & relaunch)
|   |   |-- install_windows.go    # Windows: %LOCALAPPDATA%\UPGONode\
|   |   |-- install_darwin.go     # macOS: ~/Applications/ (.app) or ~/.local/share/
|   |   +-- install_linux.go      # Linux: ~/.local/share/UPGONode/
|   +-- window/
|       |-- constrain_windows.go  # Win32 window subclassing
|       +-- constrain_other.go    # No-op stub (macOS/Linux)
|
|-- pkg/relayleaf/
|   |-- embed.go                  # Embed native libs (go:embed all:libs)
|   |-- lib_downloader.go         # Auto-download library + SHA256 verify
|   |-- lib_embedded.go           # Extract embedded library to disk
|   |-- relay_leaf_stub.go        # Stub client for dev/testing
|   +-- libs/                     # Native libraries (downloaded before build)
|
|-- frontend/                     # React + TypeScript + Vite + Ant Design
|   |-- embed.go                  # Embed compiled assets into binary
|   +-- src/
|       |-- App.tsx               # Root component
|       |-- components/
|       |   |-- Dashboard.tsx     # Stats, charts, controls
|       |   |-- Settings.tsx      # Configuration UI
|       |   |-- Sidebar.tsx       # Navigation
|       |   |-- Terminal.tsx      # xterm terminal
|       |   +-- TitleBar.tsx      # Custom title bar
|       |-- services/wails.ts     # Wails API bindings
|       +-- theme.ts              # Dark theme
|
|-- scripts/download-libs.sh      # Download native libs for embedding
|-- Makefile                      # Build automation
|-- Dockerfile.linux              # Multi-stage Docker build
+-- .github/workflows/build.yml  # CI/CD pipeline
```

### Flow

```
[System Boot] --silent--> [App hidden] --user opens app--> [SIGUSR1] --> [Show window]
                              |
                              v
                    [Auto-start relay]
                    [Direct + Proxy SDK instances]
                    [Poll stats every 2s]
                              |
                              v
                    [Events --> React frontend]
                    [Dashboard / Settings / Terminal]
```

---

## Platform Details

| | Platform | GUI Engine | Autostart | Single Instance |
|---|----------|-----------|-----------|-----------------|
| :apple: | macOS Intel | WKWebView | LaunchAgent plist | flock + SIGUSR1 |
| :apple: | macOS Apple Silicon | WKWebView | LaunchAgent plist | flock + SIGUSR1 |
| :window: | Windows x64 | WebView2 | Registry (HKCU) | CreateMutexW |
| :window: | Windows x86 | WebView2 | Registry (HKCU) | CreateMutexW |
| :penguin: | Linux amd64 | WebKit2GTK | XDG .desktop | flock + SIGUSR1 |
| :penguin: | Linux arm64 | WebKit2GTK | XDG .desktop | flock + SIGUSR1 |

---

## Native Library

The relay native library is **embedded into the binary at build time** using `go:embed`. On first launch, it is extracted to disk next to the executable. The app also checks for updates via SHA256 verification against remote servers.

| Platform | Library |
|----------|---------|
| Windows x64 | `relay_leaf-windows-x64.dll` |
| Windows x86 | `relay_leaf-windows-x86.dll` |
| macOS Intel | `librelay_leaf-darwin-amd64.dylib` |
| macOS Apple Silicon | `librelay_leaf-darwin-arm64.dylib` |
| Linux x64 | `librelay_leaf-linux-x64.so` |
| Linux arm64 | `librelay_leaf-linux-arm64.so` |

**How it works:**

1. CI/CD runs `scripts/download-libs.sh <platform>` before building to place libraries in `pkg/relayleaf/libs/`
2. `go:embed all:libs` embeds them into the binary
3. On first launch, the embedded library is extracted to disk
4. Remote checksum is fetched to verify the library is up to date
5. If a newer version exists on the server, it is downloaded and replaces the local copy

**Dev builds** (`wails dev`) work without pre-downloading — the app falls back to runtime download. If download also fails, the app runs in **stub mode** with simulated data.

---

## Self-Install

When the app detects it is **not** running from its designated install location, it automatically copies itself there and relaunches. This ensures the app works correctly even when run directly from a ZIP archive (e.g., Windows Explorer opens EXE from ZIP in a temp directory).

| Platform | Install Location |
|----------|-----------------|
| **Windows** | `%LOCALAPPDATA%\UPGONode\upgo-node.exe` |
| **macOS** (`.app` bundle) | `~/Applications/upgo-node.app` |
| **macOS** (standalone) | `~/.local/share/UPGONode/upgo-node` |
| **Linux** | `~/.local/share/UPGONode/upgo-node` |

**How it works:**

1. On startup, the app checks if `os.Executable()` matches the install path
2. If not, copies itself (or the entire `.app` bundle on macOS) to the install location
3. Relaunches from the install location with the same arguments
4. The original process exits

This also ensures that **autostart paths** (Registry / LaunchAgent / XDG) always point to a stable, persistent location.

---

## Tech Stack

| Layer | Technology |
|-------|------------|
| **Backend** | Go 1.24 |
| **Desktop** | [Wails v2](https://wails.io/) |
| **Frontend** | React 18 + TypeScript 5 |
| **Build** | Vite 5 |
| **UI** | Ant Design 6 |
| **Charts** | Recharts |
| **Terminal** | xterm.js |
| **CLI** | Cobra + Viper |
| **Logging** | Zerolog |

---

## Configuration Example

`~/.relay-app/config.yaml`

```yaml
partner_id: "YOUR_PARTNER_ID"
proxies:
  - socks5://10.0.0.1:1080
  - socks5://user:pass@10.0.0.2:1080
  - http://proxy.example.com:8080
  - http://user:pass@proxy.example.com:8080
  - https://secure-proxy.example.com:443
verbose: false
auto_start: true
launch_on_startup: true
log_level: info
```

---

## Contributing

1. **Fork** the repository
2. **Create** a feature branch: `git checkout -b feature/my-feature`
3. **Verify** cross-platform builds:
   ```bash
   GOOS=darwin  GOARCH=amd64 go vet ./...
   GOOS=darwin  GOARCH=arm64 go vet ./...
   GOOS=linux   GOARCH=amd64 go vet ./...
   GOOS=linux   GOARCH=arm64 go vet ./...
   GOOS=windows GOARCH=amd64 go vet ./...
   GOOS=windows GOARCH=386   go vet ./...
   ```
4. **Commit** and push
5. **Open** a Pull Request

---

## License

MIT License - Copyright (c) 2025 UpGo. See [LICENSE](LICENSE) for details.
