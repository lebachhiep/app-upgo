//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func listenShowSignal(app *App) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		for range ch {
			app.ShowWindow()
		}
	}()
}
