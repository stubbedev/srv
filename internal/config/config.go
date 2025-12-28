// Package config handles srv configuration and paths.
package config

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/stubbedev/srv/internal/constants"
	"gopkg.in/yaml.v3"
)

// Config holds the srv configuration.
type Config struct {
	Root        string // Base directory for all srv state
	TraefikDir  string // Traefik configuration directory
	SitesDir    string // Sites configuration directory
	NetworkName string // Docker network name
}

// UserConfig holds user-configurable settings stored in config.yml.
type UserConfig struct {
	ParkedPaths []string `yaml:"parked_paths,omitempty"`
}

var (
	configMu     sync.Mutex
	configOnce   sync.Once
	cachedConfig *Config
	configErr    error
)

// Load returns the srv configuration, creating directories as needed.
// The result is cached after the first call. This is thread-safe.
func Load() (*Config, error) {
	configMu.Lock()
	defer configMu.Unlock()
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
		TraefikDir:  filepath.Join(root, constants.TraefikSubdir),
		SitesDir:    filepath.Join(root, constants.SitesSubdir),
		NetworkName: generateNetworkName(),
	}

	return cfg, nil
}

// getSrvRoot returns the srv configuration directory.
// Priority: SRV_ROOT env var > XDG_CONFIG_HOME/srv > ~/.config/srv
func getSrvRoot() (string, error) {
	// Check for environment variable override
	if envRoot := os.Getenv(constants.EnvSrvRoot); envRoot != "" {
		if !filepath.IsAbs(envRoot) {
			return "", fmt.Errorf("%s must be an absolute path: %s", constants.EnvSrvRoot, envRoot)
		}
		if err := os.MkdirAll(envRoot, constants.DirPermDefault); err != nil {
			return "", fmt.Errorf("failed to create %s directory: %w", constants.EnvSrvRoot, err)
		}
		return envRoot, nil
	}

	// Use XDG_CONFIG_HOME if set, otherwise ~/.config
	configDir := os.Getenv(constants.EnvXDGConfigHome)
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir = filepath.Join(homeDir, constants.DefaultConfigDir)
	}

	srvRoot := filepath.Join(configDir, constants.AppName)
	if err := os.MkdirAll(srvRoot, constants.DirPermDefault); err != nil {
		return "", fmt.Errorf("failed to create srv directory: %w", err)
	}

	return srvRoot, nil
}

// generateNetworkName creates a unique network name based on hostname.
// Format: {md5(hostname)[:12]}_traefik
func generateNetworkName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = constants.DefaultHostname
	}
	hash := md5.Sum([]byte(hostname))
	return hex.EncodeToString(hash[:])[:constants.NetworkHashLength] + constants.NetworkSuffix
}

// EnvTraefikPath returns the path to the traefik environment file.
func (c *Config) EnvTraefikPath() string {
	return filepath.Join(c.Root, constants.EnvTraefikFile)
}

// TraefikComposePath returns the path to traefik's docker-compose.yml.
func (c *Config) TraefikComposePath() string {
	return filepath.Join(c.TraefikDir, constants.DockerComposeFile)
}

// TraefikConfDir returns the path to traefik's conf directory.
func (c *Config) TraefikConfDir() string {
	return filepath.Join(c.TraefikDir, constants.ConfSubdir)
}

// SiteCertsDir returns the path to a site's SSL certificates directory.
func (c *Config) SiteCertsDir(siteName string) string {
	return filepath.Join(c.SitesDir, siteName, constants.CertsSubdir)
}

// ResetCache clears the cached configuration, forcing a reload on next Load() call.
// This is thread-safe.
func ResetCache() {
	configMu.Lock()
	defer configMu.Unlock()
	configOnce = sync.Once{}
	cachedConfig = nil
	configErr = nil
}

// ConfigPath returns the path to the config.yml file.
func (c *Config) ConfigPath() string {
	return filepath.Join(c.Root, constants.UserConfigFile)
}

// LoadUserConfig loads the user configuration from config.yml.
func (c *Config) LoadUserConfig() (*UserConfig, error) {
	configPath := c.ConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var userCfg UserConfig
	if err := yaml.Unmarshal(data, &userCfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &userCfg, nil
}

// SaveUserConfig saves the user configuration to config.yml.
func (c *Config) SaveUserConfig(userCfg *UserConfig) error {
	configPath := c.ConfigPath()
	data, err := yaml.Marshal(userCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return atomicWriteFile(configPath, data, constants.FilePermDefault)
}

// atomicWriteFile writes data to a file atomically by writing to a temp file first.
// If the rename fails, the temp file is cleaned up.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + constants.ExtTmp
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup of temp file
		os.Remove(tmp)
		return err
	}
	return nil
}

// GetParkedPaths returns the list of parked directories from config.yml.
func (c *Config) GetParkedPaths() ([]string, error) {
	userCfg, err := c.LoadUserConfig()
	if err != nil {
		return nil, err
	}
	if userCfg.ParkedPaths == nil {
		return []string{}, nil
	}
	return userCfg.ParkedPaths, nil
}

// SetParkedPaths saves the list of parked directories to config.yml.
func (c *Config) SetParkedPaths(paths []string) error {
	userCfg, err := c.LoadUserConfig()
	if err != nil {
		// If we can't load, start with empty config
		userCfg = &UserConfig{}
	}
	userCfg.ParkedPaths = paths
	return c.SaveUserConfig(userCfg)
}
