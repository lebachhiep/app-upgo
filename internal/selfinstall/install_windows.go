//go:build windows

package selfinstall

import (
	"os"
	"os/exec"
	"path/filepath"
)

func installedExePath() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return ""
	}
	return filepath.Join(localAppData, "UPGONode", "upgo-node.exe")
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
