package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ProjectConfig represents .sandbar/config.toml
type ProjectConfig struct {
	Site      SiteConfig     `toml:"site"`
	Build     BuildConfig    `toml:"build"`
	Deploy    DeployConfig   `toml:"deploy"`
	Preview   PreviewConfig  `toml:"preview"`
	Env       map[string]any `toml:"env"`
	Domains   []DomainConfig `toml:"domains,omitempty"`
	Redirects []RedirectRule `toml:"redirects"`
	Headers   []HeaderRule   `toml:"headers"`
}

// DomainConfig declares a custom domain in .sandbar/config.toml.
// Authoritative: `sandbar deploy` reconciles the server state to match
// this list — domains present on the server but absent here are
// deleted. RedirectTo (optional) is the canonical hostname this
// hostname 301s to at the edge (the common case is www -> apex).
type DomainConfig struct {
	Hostname   string `toml:"hostname"`
	RedirectTo string `toml:"redirect_to,omitempty"`
}

type BuildConfig struct {
	// Command runs before deploy. Empty = skip the build step.
	Command string `toml:"command"`
}

// EnvFor returns the merged env vars for the given environment name:
// string-valued keys under [env] (defaults) plus string-valued keys
// under [env.<name>] (overrides). Pass "" for defaults only. Unknown
// names yield defaults — caller checks HasEnv before invoking if
// "unknown env" should be an error.
func (c *ProjectConfig) EnvFor(name string) map[string]string {
	out := map[string]string{}
	for k, v := range c.Env {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	if name == "" {
		return out
	}
	sub, ok := c.Env[name].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range sub {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// HasEnv reports whether a named [env.<name>] table exists.
func (c *ProjectConfig) HasEnv(name string) bool {
	if name == "" {
		return true
	}
	_, ok := c.Env[name].(map[string]any)
	return ok
}

type SiteConfig struct {
	// Slug is the URL-safe identity for the site (mataki-web in
	// `mataki-web.on.sandbar.cloud`). Immutable on the server.
	Slug string `toml:"slug,omitempty"`
	// Name is the display name shown in the dashboard. Optional.
	// When set, synced to the server on every `sandbar deploy`.
	Name string `toml:"name,omitempty"`
	// ProductionBranch is the git branch the server treats as
	// production. Branches matching this name deploy to the live URL;
	// others land on `<branch>--<slug>.on.sandbar.cloud` previews.
	// Defaults to "main" on the server when unset.
	ProductionBranch string `toml:"production_branch,omitempty"`
	BuildDir         string `toml:"build_dir,omitempty"`
	Framework        string `toml:"framework,omitempty"`
}

// EffectiveSlug returns the site identity, preferring the newer Slug
// field and falling back to Name for back-compat with configs written
// before the slug/name split (where Name held the slug). The fallback
// is gated on Slug being empty — a config with both set means the
// user has migrated and Name is the display name.
func (s *SiteConfig) EffectiveSlug() string {
	if s.Slug != "" {
		return s.Slug
	}
	return s.Name
}

// DisplayName returns the display name to sync to the server, or ""
// when the config is in legacy mode (Slug empty, Name holds the
// slug — nothing to sync there). New-shape configs that omit Name
// also return "" so deploy reconcile doesn't try to push an empty
// name and clobber whatever's on the server.
func (s *SiteConfig) DisplayName() string {
	if s.Slug == "" {
		// Legacy mode: Name is the slug, not a display name.
		return ""
	}
	return s.Name
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
