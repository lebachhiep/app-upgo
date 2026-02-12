//go:build linux

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

const desktopEntry = `[Desktop Entry]
Type=Application
Name=UPGO Node
Exec="%s" --silent
Hidden=false
NoDisplay=false
X-GNOME-Autostart-enabled=true
Comment=UPGO Node - BNC Network Node
`

func autostartDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "autostart")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "autostart")
}

func desktopFile() string {
	return filepath.Join(autostartDir(), "upgo-node.desktop")
}

func IsEnabled() (bool, error) {
	_, err := os.Stat(desktopFile())
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func Enable() error {
	dir := autostartDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	content := []byte(fmt.Sprintf(desktopEntry, exePath))
	return os.WriteFile(desktopFile(), content, 0644)
}

func Disable() error {
	err := os.Remove(desktopFile())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
