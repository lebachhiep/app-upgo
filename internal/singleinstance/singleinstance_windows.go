//go:build windows

package singleinstance

import (
	"syscall"
	"unsafe"
)

var (
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	createMutexW  = kernel32.NewProc("CreateMutexW")
	closeHandle   = kernel32.NewProc("CloseHandle")
)

const errorAlreadyExists = 183

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
