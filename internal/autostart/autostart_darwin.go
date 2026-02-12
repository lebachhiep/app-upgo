//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
)

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>io.upgo.node</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--silent</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>
`

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "io.upgo.node.plist")
}

func IsEnabled() (bool, error) {
	_, err := os.Stat(plistPath())
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func Enable() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	dir := filepath.Dir(plistPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	content := []byte(fmt.Sprintf(plistTemplate, exePath))
	return os.WriteFile(plistPath(), content, 0644)
}

func Disable() error {
	err := os.Remove(plistPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
