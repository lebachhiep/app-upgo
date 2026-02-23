//go:build windows

package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func pidFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".relay-app", "upgo-node.pid")
}

func writePID() {
	_ = os.WriteFile(pidFilePath(), []byte(fmt.Sprintf("%d", os.Getpid())), 0600)
}

func removePID() {
	_ = os.Remove(pidFilePath())
}

func readPID() uint32 {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return uint32(pid)
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

	writePID()
	return &Lock{handle: syscall.Handle(handle)}, nil
}

func (l *Lock) Release() {
	removePID()
	if l.handle != 0 {
		closeHandle.Call(uintptr(l.handle))
		l.handle = 0
	}
}

func terminatePID(pid uint32) {
	handle, _, _ := openProcess.Call(processTerminate, 0, uintptr(pid))
	if handle != 0 {
		terminateProcess.Call(handle, 0)
		closeHandle.Call(handle)
	}
}

// KillExisting finds the running UPGO Node instance and terminates it
// so the new instance can take over. Works for both GUI (window) and CLI (PID file).
func KillExisting() {
	self := uint32(os.Getpid())

	// Try 1: find by window title (GUI mode)
	titlePtr, _ := syscall.UTF16PtrFromString("UPGO Node")
	hwnd, _, _ := findWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd != 0 {
		var pid uint32
		getWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != 0 && pid != self {
			terminatePID(pid)
			time.Sleep(500 * time.Millisecond)
			return
		}
	}

	// Try 2: find by PID file (CLI mode — no window)
	pid := readPID()
	if pid != 0 && pid != self {
		terminatePID(pid)
		removePID()
		time.Sleep(500 * time.Millisecond)
	}
}
