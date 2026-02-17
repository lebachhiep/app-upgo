//go:build linux

package selfinstall

import (
	"fmt"
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
	if err := copyFile(currentExe, targetExe); err != nil {
		return err
	}
	copyCompanionLibs(filepath.Dir(currentExe), filepath.Dir(targetExe))
	return nil
}

func relaunch(targetExe string, args []string) {
	cmd := exec.Command(targetExe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Start()
}

// CreateDesktopShortcut creates a .desktop file on the user's Desktop.
func CreateDesktopShortcut() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine exe path: %w", err)
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Save embedded icon next to the binary
	iconPath := filepath.Join(filepath.Dir(exePath), "icon.png")
	if _, err := os.Stat(iconPath); os.IsNotExist(err) {
		os.WriteFile(iconPath, appIcon, 0644)
	}

	desktop := filepath.Join(home, "Desktop")
	desktopFile := filepath.Join(desktop, "upgo-node.desktop")

	// Skip if shortcut already exists
	if _, err := os.Stat(desktopFile); err == nil {
		return nil
	}

	// Ensure Desktop directory exists
	os.MkdirAll(desktop, 0755)

	content := fmt.Sprintf(`[Desktop Entry]
Name=UPGO Node
Comment=BNC Network Node
Exec="%s"
Icon=%s
Type=Application
Terminal=false
Categories=Network;Utility;
`, exePath, iconPath)

	if err := os.WriteFile(desktopFile, []byte(content), 0755); err != nil {
		return err
	}

	// Also install to applications menu
	appsDir := filepath.Join(home, ".local", "share", "applications")
	os.MkdirAll(appsDir, 0755)
	os.WriteFile(filepath.Join(appsDir, "upgo-node.desktop"), []byte(content), 0755)

	return nil
}
