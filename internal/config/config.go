// Package config handles loading and managing calvault configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config represents the calvault configuration.
type Config struct {
	OAuth OAuthConfig `toml:"oauth"`
	Sync  SyncConfig  `toml:"sync"`

	// Computed paths (not from config file)
	HomeDir string `toml:"-"`
}

// OAuthConfig holds OAuth configuration.
type OAuthConfig struct {
	ClientSecrets string `toml:"client_secrets"`
}

// SyncConfig holds sync-related configuration.
type SyncConfig struct {
	RateLimitQPS int `toml:"rate_limit_qps"`
}

// DefaultHome returns the default calvault home directory.
// Respects CALVAULT_HOME environment variable.
func DefaultHome() string {
	if h := os.Getenv("CALVAULT_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".calvault"
	}
	return filepath.Join(home, ".calvault")
}

// Load reads the configuration from the specified file.
// If path is empty, uses the default location (~/.calvault/config.toml).
func Load(path string) (*Config, error) {
	homeDir := DefaultHome()

	if path == "" {
		path = filepath.Join(homeDir, "config.toml")
	}

	cfg := &Config{
		HomeDir: homeDir,
		Sync: SyncConfig{
			RateLimitQPS: 10,
		},
	}

	// Config file is optional - use defaults if not present
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	// Expand ~ in paths
	cfg.OAuth.ClientSecrets = expandPath(cfg.OAuth.ClientSecrets)

	return cfg, nil
}

// DatabasePath returns the path to the SQLite database.
func (c *Config) DatabasePath() string {
	return filepath.Join(c.HomeDir, "calvault.db")
}

// TokensDir returns the path to the OAuth tokens directory.
func (c *Config) TokensDir() string {
	return filepath.Join(c.HomeDir, "tokens")
}

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
