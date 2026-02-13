package selfinstall

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// EnsureInstalled checks if the app is running from the proper install
// location. If not, copies itself there and relaunches with the same
// arguments. Returns true if the caller should exit (relaunch happened).
func EnsureInstalled(args []string) bool {
	currentExe, err := os.Executable()
	if err != nil {
		return false
	}
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return false
	}

	targetExe := installedExePath()
	if targetExe == "" {
		return false
	}

	if isSamePath(currentExe, targetExe) {
		return false
	}

	if err := copySelf(currentExe, targetExe); err != nil {
		return false
	}

	relaunch(targetExe, args)
	return true
}

// isSamePath compares two paths in a platform-appropriate way.
func isSamePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if strings.EqualFold(a, b) {
		return true
	}
	// Check by resolving symlinks on both sides
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		return strings.EqualFold(filepath.Clean(ra), filepath.Clean(rb))
	}
	return false
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}
