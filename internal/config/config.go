package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

var (
	instance *viper.Viper
	once     sync.Once
	configMu sync.RWMutex
)

func Get() *viper.Viper {
	once.Do(func() {
		instance = viper.New()
		instance.SetConfigName("config")
		instance.SetConfigType("yaml")

		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "."
		}

		configDir := filepath.Join(homeDir, ".relay-app")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			configDir = "."
		}

		instance.AddConfigPath(configDir)

		instance.SetDefault("partner_id", "")
		instance.SetDefault("discovery_url", "")
		instance.SetDefault("proxies", []string{})
		instance.SetDefault("verbose", false)
		instance.SetDefault("auto_start", true)
		instance.SetDefault("launch_on_startup", true)
		instance.SetDefault("log_level", "info")

		configFile := filepath.Join(configDir, "config.yaml")
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			if err := instance.SafeWriteConfigAs(configFile); err != nil {
				// Ignore write errors on first run
			}
		}

		if err := instance.ReadInConfig(); err != nil {
			// Use defaults if config file can't be read
		}
	})

	return instance
}

func Save() error {
	configMu.Lock()
	defer configMu.Unlock()

	if instance == nil {
		return nil
	}
	return instance.WriteConfig()
}

func NormalizeKey(key string) string {
	return strings.ReplaceAll(key, "-", "_")
}

func GetConfigDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(homeDir, ".relay-app")
}
