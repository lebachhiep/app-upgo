package main

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"relay-app/frontend"
	"relay-app/internal/cli"
	"relay-app/internal/config"
	"relay-app/internal/singleinstance"
)

var version = "1.0.0"

func main() {
	// Extract --silent flag before routing to CLI or GUI
	silent := false
	isBindings := false
	filteredArgs := []string{os.Args[0]}
	for _, arg := range os.Args[1:] {
		if arg == "--silent" {
			silent = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
		if arg == "--bindings" || arg == "-bindings" {
			isBindings = true
		}
	}
	os.Args = filteredArgs

	// Skip single-instance check during Wails binding generation
	if !isBindings {
		lock, err := singleinstance.Acquire()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer lock.Release()
	}

	if len(os.Args) > 1 {
		runCLI()
	} else {
		runGUI(silent)
	}
}

func runCLI() {
	cfg := config.Get()
	logLevel := cfg.GetString("log_level")
	if logLevel == "" {
		logLevel = "info"
	}

	cli.SetVersion(version)
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runGUI(silent bool) {
	app := NewApp()
	app.version = version
	app.silentMode = silent

	// Always start Normal — silent mode hides via WindowHide in startup()
	err := wails.Run(&options.App{
		Title:     "UPGO Node",
		Width:     1280,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: frontend.Assets,
		},
		BackgroundColour: &options.RGBA{R: 20, G: 35, B: 52, A: 1},
		WindowStartState: options.Normal,
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind: []interface{}{
			app,
		},
		Frameless: true,
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableFramelessWindowDecorations: false,
			Theme:                             windows.Dark,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "UPGO Node",
				Message: "BNC Network Node",
			},
		},
		Linux: &linux.Options{
			WindowIsTranslucent: false,
			ProgramName:         "UPGO Node",
		},
	})

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err.Error())
		os.Exit(1)
	}
}
