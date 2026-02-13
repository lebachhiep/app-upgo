//go:build windows

package main

func listenShowSignal(app *App) {
	// Windows: no SIGUSR1 support, single-instance handled via mutex only
}
