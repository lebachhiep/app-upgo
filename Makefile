APP_NAME    := upgo-node
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "1.0.0")
LDFLAGS     := -s -w -X main.version=$(VERSION)

.PHONY: dev build build-windows-x64 build-windows-x86 build-linux-amd64 build-linux-arm64 build-darwin clean frontend-install frontend-build test

# ─── Development ──────────────────────────────────────────
dev:
	wails dev

# ─── Builds (GUI + CLI in one binary, must run on target OS)
build:
	wails build -ldflags "$(LDFLAGS)"

# ── Windows ───────────────────────────────────────────────
build-windows-x64:
	wails build -platform windows/amd64 -ldflags "$(LDFLAGS)"

build-windows-x86:
	wails build -platform windows/386 -ldflags "$(LDFLAGS)"

# ── macOS (universal = Intel + Apple Silicon) ─────────────
build-darwin:
	wails build -platform darwin/universal -ldflags "$(LDFLAGS)"

# ── Linux ─────────────────────────────────────────────────
build-linux-amd64:
	wails build -platform linux/amd64 -ldflags "$(LDFLAGS)"

build-linux-arm64:
	CC=aarch64-linux-gnu-gcc CXX=aarch64-linux-gnu-g++ CGO_ENABLED=1 \
	PKG_CONFIG_PATH=/usr/lib/aarch64-linux-gnu/pkgconfig \
	wails build -platform linux/arm64 -ldflags "$(LDFLAGS)"

# ─── Frontend ─────────────────────────────────────────────
frontend-install:
	cd frontend && npm install

frontend-build:
	cd frontend && npm run build

# ─── Utilities ────────────────────────────────────────────
clean:
	rm -rf build/bin/*
	cd frontend && rm -rf dist

test:
	go run . version
	go run . config show
