//go:build windows

package main

import "relay-app/internal/singleinstance"

func listenShowSignal(app *App) {
	singleinstance.ListenForShowSignal(func() {
		app.ShowWindow()
	})
}
