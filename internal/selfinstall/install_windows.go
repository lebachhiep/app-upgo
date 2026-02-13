//go:build windows

package selfinstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// CreateDesktopShortcut creates a .lnk shortcut on the user's Desktop.
func CreateDesktopShortcut() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine exe path: %w", err)
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	shortcutPath := filepath.Join(desktop, "UPGO Node.lnk")

	// Skip if shortcut already exists
	if _, err := os.Stat(shortcutPath); err == nil {
		return nil
	}

	// Use PowerShell WScript.Shell COM to create .lnk
	// Escape single quotes in paths to prevent PowerShell injection
	escPath := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
	ps := fmt.Sprintf(
		`$ws = New-Object -ComObject WScript.Shell; `+
			`$s = $ws.CreateShortcut('%s'); `+
			`$s.TargetPath = '%s'; `+
			`$s.WorkingDirectory = '%s'; `+
			`$s.IconLocation = '%s,0'; `+
			`$s.Description = 'UPGO Node - BNC Network'; `+
			`$s.Save()`,
		escPath(shortcutPath), escPath(exePath), escPath(filepath.Dir(exePath)), escPath(exePath),
	)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	return cmd.Run()
}
