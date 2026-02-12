//go:build windows

package window

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	procFindWindowW        = user32.NewProc("FindWindowW")
	procSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW    = user32.NewProc("CallWindowProcW")
	procMonitorFromWindow  = user32.NewProc("MonitorFromWindow")
	procMonitorFromRect    = user32.NewProc("MonitorFromRect")
	procGetMonitorInfoW    = user32.NewProc("GetMonitorInfoW")
)

const (
	gwlpWndProc             = ^uintptr(3) // -4 as uintptr
	wmMoving                = 0x0216
	wmGetMinMaxInfo         = 0x0024
	monitorDefaultToNearest = 0x00000002
)

type winPOINT struct {
	X, Y int32
}

type winRECT struct {
	Left, Top, Right, Bottom int32
}

type winMINMAXINFO struct {
	Reserved     winPOINT
	MaxSize      winPOINT
	MaxPosition  winPOINT
	MinTrackSize winPOINT
	MaxTrackSize winPOINT
}

type winMONITORINFO struct {
	Size    uint32
	Monitor winRECT
	Work    winRECT
	Flags   uint32
}

var (
	origWndProc uintptr
	cbPtr       uintptr // prevent GC of callback
)

func constrainProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmGetMinMaxInfo:
		// Let original proc handle first
		ret, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wParam, lParam)

		// Override maximize position/size to fit work area exactly
		// This prevents frameless window from extending beyond the screen
		hMon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
		if hMon != 0 {
			var mi winMONITORINFO
			mi.Size = uint32(unsafe.Sizeof(mi))
			ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi)))
			if ok != 0 {
				mmi := (*winMINMAXINFO)(unsafe.Pointer(lParam))
				mmi.MaxPosition.X = mi.Work.Left - mi.Monitor.Left
				mmi.MaxPosition.Y = mi.Work.Top - mi.Monitor.Top
				mmi.MaxSize.X = mi.Work.Right - mi.Work.Left
				mmi.MaxSize.Y = mi.Work.Bottom - mi.Work.Top
			}
		}
		return ret

	case wmMoving:
		if lParam != 0 {
			r := (*winRECT)(unsafe.Pointer(lParam))

			hMon, _, _ := procMonitorFromRect.Call(lParam, monitorDefaultToNearest)
			if hMon != 0 {
				var mi winMONITORINFO
				mi.Size = uint32(unsafe.Sizeof(mi))
				ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi)))
				if ok != 0 {
					w := r.Right - r.Left
					h := r.Bottom - r.Top
					work := mi.Work

					// Keep titlebar on screen: never above work area
					if r.Top < work.Top {
						r.Top = work.Top
						r.Bottom = r.Top + h
					}
					// Keep at least 40px visible at bottom
					if r.Top > work.Bottom-40 {
						r.Top = work.Bottom - 40
						r.Bottom = r.Top + h
					}
					// Keep at least 150px visible horizontally
					if r.Right < work.Left+150 {
						r.Left = work.Left + 150 - w
						r.Right = r.Left + w
					}
					if r.Left > work.Right-150 {
						r.Left = work.Right - 150
						r.Right = r.Left + w
					}
				}
			}
			return 1
		}
	}

	ret, _, _ := procCallWindowProcW.Call(origWndProc, hwnd, msg, wParam, lParam)
	return ret
}

var (
	procMoveWindow            = user32.NewProc("MoveWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
)

const swHide = 0

// HideWindow hides the window using Win32 ShowWindow(SW_HIDE) directly.
// This is more reliable than Wails runtime.WindowHide() during early startup.
func HideWindow(windowTitle string) error {
	titlePtr, err := syscall.UTF16PtrFromString(windowTitle)
	if err != nil {
		return err
	}

	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return fmt.Errorf("window not found: %s", windowTitle)
	}

	procShowWindow.Call(hwnd, swHide)
	return nil
}

const spiGetWorkArea = 0x0030

// CenterAndResize positions the window at the center of the work area,
// sized to ~80% of the available space (clamped between min and max bounds).
func CenterAndResize(windowTitle string) error {
	titlePtr, err := syscall.UTF16PtrFromString(windowTitle)
	if err != nil {
		return err
	}

	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return fmt.Errorf("window not found: %s", windowTitle)
	}

	// Get monitor work area for the window's current monitor
	hMon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if hMon == 0 {
		return fmt.Errorf("failed to get monitor")
	}

	var mi winMONITORINFO
	mi.Size = uint32(unsafe.Sizeof(mi))
	ok, _, _ := procGetMonitorInfoW.Call(hMon, uintptr(unsafe.Pointer(&mi)))
	if ok == 0 {
		return fmt.Errorf("failed to get monitor info")
	}

	workW := int(mi.Work.Right - mi.Work.Left)
	workH := int(mi.Work.Bottom - mi.Work.Top)

	// 50% of work area, clamped to reasonable bounds
	w := workW * 50 / 100
	h := workH * 50 / 100
	if w < 900 {
		w = 900
	}
	if h < 600 {
		h = 600
	}
	if w > workW {
		w = workW
	}
	if h > workH {
		h = workH
	}

	// Center in work area
	x := int(mi.Work.Left) + (workW-w)/2
	y := int(mi.Work.Top) + (workH-h)/2

	procMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
	return nil
}

// ConstrainToScreen subclasses the window to prevent it from being dragged
// off-screen and to fix maximize extending beyond the screen.
func ConstrainToScreen(windowTitle string) error {
	titlePtr, err := syscall.UTF16PtrFromString(windowTitle)
	if err != nil {
		return err
	}

	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return fmt.Errorf("window not found: %s", windowTitle)
	}

	cbPtr = syscall.NewCallback(constrainProc)
	origWndProc, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, cbPtr)
	if origWndProc == 0 {
		return fmt.Errorf("failed to subclass window")
	}

	return nil
}
