//go:build !windows

package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Lock struct {
	file *os.File
}

func lockPath() string {
	dir := os.TempDir()
	return filepath.Join(dir, "upgo-node.lock")
}

func Acquire() (*Lock, error) {
	f, err := os.OpenFile(lockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, ErrAlreadyRunning
	}

	// Write PID so second instance can kill us
	f.Truncate(0)
	f.Seek(0, 0)
	fmt.Fprintf(f, "%d", os.Getpid())
	f.Sync()

	return &Lock{file: f}, nil
}

func (l *Lock) Release() {
	if l.file != nil {
		syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
		l.file.Close()
		os.Remove(lockPath())
		l.file = nil
	}
}

// KillExisting reads PID from lock file and kills the running instance.
func KillExisting() {
	data, err := os.ReadFile(lockPath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid == os.Getpid() {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	proc.Kill()
	time.Sleep(500 * time.Millisecond)
}
