//go:build linux

package selfinstall

import (
	"os"
	"os/exec"
	"path/filepath"
)

func installedExePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "UPGONode", "upgo-node")
}

func copySelf(currentExe, targetExe string) error {
	return copyFile(currentExe, targetExe)
}

func relaunch(targetExe string, args []string) {
	cmd := exec.Command(targetExe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Start()
}
