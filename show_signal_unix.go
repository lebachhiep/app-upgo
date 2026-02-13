//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func listenShowSignal(app *App) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		for range ch {
			runtime.WindowShow(app.ctx)
			runtime.WindowUnminimise(app.ctx)
		}
	}()
}
