//go:build !windows

package singleinstance

import (
	"os"
	"path/filepath"
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
