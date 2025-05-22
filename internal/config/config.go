// Package config handles srv configuration and paths.
package config

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config holds the srv configuration.
type Config struct {
	Root        string // Base directory for all srv state
	TraefikDir  string // Traefik configuration directory
	SitesDir    string // Sites symlinks directory
	NetworkName string // Docker network name
}

var (
	configOnce   sync.Once
	cachedConfig *Config
	configErr    error
)

// Load returns the srv configuration, creating directories as needed.
// The result is cached after the first call.
func Load() (*Config, error) {
	configOnce.Do(func() {
		cachedConfig, configErr = load()
	})
	return cachedConfig, configErr
}

func load() (*Config, error) {
	root, err := getSrvRoot()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Root:        root,
		TraefikDir:  filepath.Join(root, "traefik"),
		SitesDir:    filepath.Join(root, "sites"),
		NetworkName: generateNetworkName(),
	}

	return cfg, nil
}

// getSrvRoot returns the srv configuration directory.
// Priority: SRV_ROOT env var > XDG_CONFIG_HOME/srv > ~/.config/srv
func getSrvRoot() (string, error) {
	// Check for environment variable override
	if envRoot := os.Getenv("SRV_ROOT"); envRoot != "" {
		if !filepath.IsAbs(envRoot) {
			return "", fmt.Errorf("SRV_ROOT must be an absolute path: %s", envRoot)
		}
		if err := os.MkdirAll(envRoot, 0755); err != nil {
			return "", fmt.Errorf("failed to create SRV_ROOT directory: %w", err)
		}
		return envRoot, nil
	}

	// Use XDG_CONFIG_HOME if set, otherwise ~/.config
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	srvRoot := filepath.Join(configDir, "srv")
	if err := os.MkdirAll(srvRoot, 0755); err != nil {
		return "", fmt.Errorf("failed to create srv directory: %w", err)
	}

	return srvRoot, nil
}

// generateNetworkName creates a unique network name based on hostname.
// Format: {md5(hostname)[:12]}_traefik
func generateNetworkName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "default"
	}
	hash := md5.Sum([]byte(hostname))
	return hex.EncodeToString(hash[:])[:12] + "_traefik"
}

// EnvTraefikPath returns the path to the traefik environment file.
func (c *Config) EnvTraefikPath() string {
	return filepath.Join(c.Root, "env.traefik")
}

// TraefikComposePath returns the path to traefik's docker-compose.yml.
func (c *Config) TraefikComposePath() string {
	return filepath.Join(c.TraefikDir, "docker-compose.yml")
}

// TraefikConfDir returns the path to traefik's conf directory.
func (c *Config) TraefikConfDir() string {
	return filepath.Join(c.TraefikDir, "conf")
}

// LocalCertsDir returns the path to local SSL certificates.
func (c *Config) LocalCertsDir() string {
	return filepath.Join(c.TraefikDir, "certs", "local")
}

// ResetCache clears the cached configuration, forcing a reload on next Load() call.
func ResetCache() {
	configOnce = sync.Once{}
	cachedConfig = nil
	configErr = nil
}
