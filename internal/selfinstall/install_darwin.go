//go:build darwin

package selfinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func installedExePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	currentExe, err := os.Executable()
	if err != nil {
		return ""
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	// If running inside a .app bundle, install entire .app to ~/Applications/
	if idx := strings.LastIndex(currentExe, ".app/Contents/MacOS/"); idx >= 0 {
		appName := filepath.Base(currentExe[:idx+4]) // "upgo-node.app"
		binaryName := filepath.Base(currentExe)
		return filepath.Join(home, "Applications", appName, "Contents", "MacOS", binaryName)
	}

	// Standalone binary
	return filepath.Join(home, ".local", "share", "UPGONode", "upgo-node")
}

func copySelf(currentExe, targetExe string) error {
	// Check if inside .app bundle — copy entire bundle
	if idx := strings.LastIndex(currentExe, ".app/Contents/MacOS/"); idx >= 0 {
		srcApp := currentExe[:idx+4] // source .app directory
		dstApp := targetExe
		if idx2 := strings.LastIndex(dstApp, ".app/Contents/MacOS/"); idx2 >= 0 {
			dstApp = dstApp[:idx2+4] // target .app directory
		}

		os.MkdirAll(filepath.Dir(dstApp), 0755)
		os.RemoveAll(dstApp)

		// Use cp -a to preserve the bundle structure
		cmd := exec.Command("cp", "-a", srcApp, dstApp)
		return cmd.Run()
	}

	// Standalone binary
	return copyFile(currentExe, targetExe)
}

func relaunch(targetExe string, args []string) {
	// If inside .app bundle, use 'open' command
	if idx := strings.LastIndex(targetExe, ".app/Contents/MacOS/"); idx >= 0 {
		appPath := targetExe[:idx+4]
		openArgs := []string{appPath}
		if len(args) > 0 {
			openArgs = append(openArgs, "--args")
			openArgs = append(openArgs, args...)
		}
		cmd := exec.Command("open", openArgs...)
		cmd.Start()
		return
	}

	// Standalone binary
	cmd := exec.Command(targetExe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Start()
}
