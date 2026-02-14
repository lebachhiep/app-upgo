//go:build windows

package singleinstance

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	createMutexW     = kernel32.NewProc("CreateMutexW")
	closeHandle      = kernel32.NewProc("CloseHandle")
	openProcess      = kernel32.NewProc("OpenProcess")
	terminateProcess = kernel32.NewProc("TerminateProcess")

	user32                   = syscall.NewLazyDLL("user32.dll")
	findWindowW              = user32.NewProc("FindWindowW")
	getWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

const (
	errorAlreadyExists = 183
	processTerminate   = 0x0001
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

// KillExisting finds the running UPGO Node window, gets its process ID,
// and terminates that process so the new instance can take over.
func KillExisting() {
	titlePtr, _ := syscall.UTF16PtrFromString("UPGO Node")
	hwnd, _, _ := findWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	var pid uint32
	getWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == uint32(os.Getpid()) {
		return
	}
	handle, _, _ := openProcess.Call(processTerminate, 0, uintptr(pid))
	if handle != 0 {
		terminateProcess.Call(handle, 0)
		closeHandle.Call(handle)
	}
	time.Sleep(500 * time.Millisecond)
}
