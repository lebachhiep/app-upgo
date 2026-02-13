//go:build windows

package singleinstance

import (
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	createMutexW    = kernel32.NewProc("CreateMutexW")
	createEventW    = kernel32.NewProc("CreateEventW")
	openEventW      = kernel32.NewProc("OpenEventW")
	setEvent        = kernel32.NewProc("SetEvent")
	waitForSingleOb = kernel32.NewProc("WaitForSingleObject")
	closeHandle     = kernel32.NewProc("CloseHandle")
)

const (
	errorAlreadyExists = 183
	eventModifyState   = 0x0002
	infinite           = 0xFFFFFFFF
	waitObject0        = 0
)

type Lock struct {
	handle syscall.Handle
}

func Acquire() (*Lock, error) {
	name, _ := syscall.UTF16PtrFromString("Global\\UPGONode_SingleInstance")
	handle, _, err := createMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, err
	}

	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		closeHandle.Call(handle)
		return nil, ErrAlreadyRunning
	}

	return &Lock{handle: syscall.Handle(handle)}, nil
}

func (l *Lock) Release() {
	if l.handle != 0 {
		closeHandle.Call(uintptr(l.handle))
		l.handle = 0
	}
}

// ListenForShowSignal creates a named event and calls callback whenever
// a second instance signals it. This runs in a background goroutine.
func ListenForShowSignal(callback func()) {
	evName, _ := syscall.UTF16PtrFromString("Local\\UPGONode_ShowWindow")
	h, _, _ := createEventW.Call(0, 0, 0, uintptr(unsafe.Pointer(evName))) // auto-reset event
	if h == 0 {
		return
	}
	go func() {
		for {
			ret, _, _ := waitForSingleOb.Call(h, infinite)
			if ret != waitObject0 {
				return // event handle closed or error
			}
			callback()
		}
	}()
}

// SignalExisting sets the named event so the running instance shows its window.
func SignalExisting() error {
	evName, _ := syscall.UTF16PtrFromString("Local\\UPGONode_ShowWindow")
	h, _, _ := openEventW.Call(eventModifyState, 0, uintptr(unsafe.Pointer(evName)))
	if h == 0 {
		return nil // event not found (app may not have created it yet)
	}
	setEvent.Call(h)
	closeHandle.Call(h)
	return nil
}
