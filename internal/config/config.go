package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ProjectConfig represents .sandbar/config.toml
type ProjectConfig struct {
	Site      SiteConfig                   `toml:"site"`
	Deploy    DeployConfig                 `toml:"deploy"`
	Preview   PreviewConfig                `toml:"preview"`
	Redirects []RedirectRule `toml:"redirects"`
	Headers   []HeaderRule   `toml:"headers"`
}

type SiteConfig struct {
	Name      string `toml:"name"`
	BuildDir  string `toml:"build_dir"`
	Framework string `toml:"framework"`
}

type DeployConfig struct {
	AutoActivate   bool `toml:"auto_activate"`
	MessageFromGit bool `toml:"message_from_git"`
}

type PreviewConfig struct {
	DefaultExpiry string `toml:"default_expiry"`
}

type RedirectRule struct {
	From   string `toml:"from"`
	To     string `toml:"to"`
	Status int    `toml:"status"`
	Force  bool   `toml:"force,omitempty"`
}

type HeaderRule struct {
	For    string            `toml:"for"`
	Values map[string]string `toml:"values"`
}

// GlobalConfig represents ~/.config/sandbar/config.toml
type GlobalConfig struct {
	Auth   AuthConfig `toml:"auth"`
	APIURL string     `toml:"api_url,omitempty"`
}

type AuthConfig struct {
	Token string `toml:"token"`
}

// LoadGlobal reads ~/.config/sandbar/config.toml.
func LoadGlobal() *GlobalConfig {
	path := filepath.Join(GlobalConfigDir(), "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return &GlobalConfig{}
	}
	var cfg GlobalConfig
	toml.Unmarshal(data, &cfg) //nolint:errcheck
	return &cfg
}

// ResolveAPIURL returns API base URL. Priority: SANDBAR_API_URL env > global config > default.
func ResolveAPIURL() string {
	if u := os.Getenv("SANDBAR_API_URL"); u != "" {
		return u
	}
	if cfg := LoadGlobal(); cfg.APIURL != "" {
		return cfg.APIURL
	}
	return ""
}

// LoadProject reads .sandbar/config.toml from the given directory.
func LoadProject(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ".sandbar", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no .sandbar/config.toml found. Run `sandbar init` first")
	}
	var cfg ProjectConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

// GlobalConfigDir returns the default global config directory.
func GlobalConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sandbar")
}

// ResolveToken returns the auth token from env or global config.
// Priority: SANDBAR_TOKEN env > ~/.config/sandbar/config.toml
func ResolveToken(globalDir string) (string, error) {
	if key := os.Getenv("SANDBAR_TOKEN"); key != "" {
		return key, nil
	}

	path := filepath.Join(globalDir, "config.toml")
	data, err := os.ReadFile(path)
	if err == nil {
		var cfg GlobalConfig
		if err := toml.Unmarshal(data, &cfg); err == nil && cfg.Auth.Token != "" {
			return cfg.Auth.Token, nil
		}
	}

	return "", fmt.Errorf("not logged in. Run `sandbar login` to authenticate")
}

// WriteGlobalAuth saves an auth token to the global config file.
func WriteGlobalAuth(token string) error {
	dir := GlobalConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(dir, "config.toml")

	// Load existing config or start fresh
	var cfg GlobalConfig
	if data, err := os.ReadFile(path); err == nil {
		toml.Unmarshal(data, &cfg) //nolint:errcheck
	}

	cfg.Auth.Token = token

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(cfg)
}

// WriteProject writes a .sandbar/config.toml file.
func WriteProject(dir string, cfg *ProjectConfig) error {
	sandbarDir := filepath.Join(dir, ".sandbar")
	if err := os.MkdirAll(sandbarDir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(sandbarDir, "config.toml"))
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
