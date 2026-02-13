//go:build !windows

package singleinstance

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

	// Write PID so second instance can signal us
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

// SignalExisting sends SIGUSR1 to the running instance to show its window.
func SignalExisting() error {
	data, err := os.ReadFile(lockPath())
	if err != nil {
		return fmt.Errorf("cannot read lock file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID in lock file: %w", err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGUSR1)
}
