//go:build cgo || windows

package tray

import (
	"os"
	"runtime"
	"time"

	"github.com/energye/systray"
)

type TrayCallbacks struct {
	OnShowWindow   func()
	OnStartRelay   func()
	OnStopRelay    func()
	OnQuit         func()
	IsRelayRunning func() bool
}

type TrayController struct {
	callbacks    TrayCallbacks
	mStartStop   *systray.MenuItem
	relayRunning bool
}

func NewTrayController(cb TrayCallbacks) *TrayController {
	return &TrayController{callbacks: cb}
}

func (tc *TrayController) Start() {
	// Must run in a goroutine locked to one OS thread so that the
	// hidden tray window and its message loop share the same thread.
	// RunWithExternalLoop splits them across threads, breaking message dispatch.
	go func() {
		runtime.LockOSThread()
		systray.Run(tc.onReady, nil)
	}()
}

func (tc *TrayController) onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("UPGO Node")
	systray.SetTooltip("UPGO Node - BNC Network")

	// Left-click / double-click → show window
	systray.SetOnClick(func(menu systray.IMenu) {
		if tc.callbacks.OnShowWindow != nil {
			tc.callbacks.OnShowWindow()
		}
	})
	systray.SetOnDClick(func(menu systray.IMenu) {
		if tc.callbacks.OnShowWindow != nil {
			tc.callbacks.OnShowWindow()
		}
	})

	// Right-click → explicitly show the context menu
	systray.SetOnRClick(func(menu systray.IMenu) {
		menu.ShowMenu()
	})

	// Menu items
	mShow := systray.AddMenuItem("Show Window", "Show the application window")
	systray.AddSeparator()
	tc.mStartStop = systray.AddMenuItem("Start Node", "Start or stop the BNC node")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Exit", "Quit the application")

	if tc.callbacks.IsRelayRunning != nil && tc.callbacks.IsRelayRunning() {
		tc.mStartStop.SetTitle("Stop Node")
		tc.relayRunning = true
	}

	// Handle menu clicks via Click() callbacks
	mShow.Click(func() {
		if tc.callbacks.OnShowWindow != nil {
			tc.callbacks.OnShowWindow()
		}
	})

	tc.mStartStop.Click(func() {
		if tc.relayRunning {
			if tc.callbacks.OnStopRelay != nil {
				tc.callbacks.OnStopRelay()
			}
		} else {
			if tc.callbacks.OnStartRelay != nil {
				tc.callbacks.OnStartRelay()
			}
		}
	})

	mQuit.Click(func() {
		if tc.callbacks.OnQuit != nil {
			tc.callbacks.OnQuit()
		}
		// Fallback: force exit after 3 seconds if Quit() didn't work
		time.AfterFunc(3*time.Second, func() {
			os.Exit(0)
		})
	})
}

func (tc *TrayController) SetRelayRunning(running bool) {
	tc.relayRunning = running
	if tc.mStartStop == nil {
		return
	}
	if running {
		tc.mStartStop.SetTitle("Stop Node")
	} else {
		tc.mStartStop.SetTitle("Start Node")
	}
}

func (tc *TrayController) Stop() {
	systray.Quit()
}
