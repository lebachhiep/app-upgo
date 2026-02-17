package selfinstall

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EnsureInstalled checks if the app is running from the proper install
// location. If not, copies itself there and relaunches with the same
// arguments. Returns true if the caller should exit.
// IMPORTANT: app must NEVER run from a non-installed location (e.g. temp).
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

	// Already running from proper install location
	if isSamePath(currentExe, targetExe) {
		return false
	}

	// Not in proper location — try to copy/update, then ALWAYS exit
	copySelf(currentExe, targetExe) // ignore error: target may be locked by running instance

	// Try to launch from install location (if file exists there)
	// If another instance is already running, the new process will
	// hit single-instance check → signal existing → exit on its own.
	if _, err := os.Stat(targetExe); err == nil {
		relaunch(targetExe, args)
	}

	return true // NEVER continue running from wrong location
}

// isSamePath compares two paths in a platform-appropriate way.
// Case-insensitive on Windows/macOS, case-sensitive on Linux.
func isSamePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)

	equal := func(x, y string) bool {
		if runtime.GOOS == "linux" {
			return x == y
		}
		return strings.EqualFold(x, y)
	}

	if equal(a, b) {
		return true
	}
	// Check by resolving symlinks on both sides
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		return equal(filepath.Clean(ra), filepath.Clean(rb))
	}
	return false
}

// copyCompanionLibs copies relay leaf native libraries from the source exe
// directory to the target exe directory. This ensures the DLL/so/dylib is
// available next to the installed exe.
func copyCompanionLibs(srcDir, dstDir string) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "relay_leaf") || strings.HasPrefix(name, "librelay_leaf") {
			_ = copyFile(filepath.Join(srcDir, name), filepath.Join(dstDir, name))
		}
	}
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

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	// Check Close error to detect flush/write failures
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}
