package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	Trusts    []TrustConfig  `toml:"trusts,omitempty"`
	Redirects []RedirectRule `toml:"redirects"`
	Headers   []HeaderRule   `toml:"headers"`
}

// TrustConfig declares an OIDC deploy trust in .sandbar/config.toml.
// Authoritative: `sandbar deploy` reconciles the server's trust list
// to match this block. Trusts present on the server but absent here
// are deleted — including the trust the current deploy used to
// authenticate, so don't remove a trust from config until the
// workflow using it stops running.
//
// Identity is the (Provider, Repository, RefFilter, Environment)
// tuple; matching is exact.
type TrustConfig struct {
	Provider    string `toml:"provider,omitempty"`    // default "github"
	Repository  string `toml:"repository"`            // e.g. "mataki-dev/mataki-web"
	RefFilter   string `toml:"ref_filter,omitempty"`  // default "*"
	Environment string `toml:"environment,omitempty"` // default "*"
}

// EffectiveProvider returns the trust's provider with the "github"
// default applied — matches how the server normalises empty values.
func (t *TrustConfig) EffectiveProvider() string {
	if t.Provider == "" {
		return "github"
	}
	return t.Provider
}

// EffectiveRefFilter returns the ref filter with the "*" default.
func (t *TrustConfig) EffectiveRefFilter() string {
	if t.RefFilter == "" {
		return "*"
	}
	return t.RefFilter
}

// EffectiveEnvironment returns the environment with the "*" default.
func (t *TrustConfig) EffectiveEnvironment() string {
	if t.Environment == "" {
		return "*"
	}
	return t.Environment
}

// Key returns the tuple identity for matching against the server's
// trust list.
func (t *TrustConfig) Key() TrustKey {
	return TrustKey{
		Provider:    t.EffectiveProvider(),
		Repository:  t.Repository,
		RefFilter:   t.EffectiveRefFilter(),
		Environment: t.EffectiveEnvironment(),
	}
}

// TrustKey is the (provider, repo, ref, env) tuple used to match
// config trust entries against server-side rows.
type TrustKey struct {
	Provider    string
	Repository  string
	RefFilter   string
	Environment string
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
	Auth      AuthConfig      `toml:"auth"`
	APIURL    string          `toml:"api_url,omitempty"`
	Microwave MicrowaveConfig `toml:"microwave,omitempty"`
}

type AuthConfig struct {
	Token string `toml:"token"`
}

type MicrowaveConfig struct {
	APIURL                  string `toml:"api_url,omitempty"`
	AuthURL                 string `toml:"auth_url,omitempty"`
	CLIExchangeID           string `toml:"cli_exchange_id,omitempty"`
	GitHubActionsExchangeID string `toml:"github_actions_exchange_id,omitempty"`
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

func ResolveMicrowaveAPIURL() string {
	if u := os.Getenv("SANDBAR_MICROWAVE_API_URL"); u != "" {
		return u
	}
	if cfg := LoadGlobal(); cfg.Microwave.APIURL != "" {
		return cfg.Microwave.APIURL
	}
	return "https://api.microwave.sh"
}

func ResolveMicrowaveAuthURL() string {
	if u := os.Getenv("SANDBAR_MICROWAVE_AUTH_URL"); u != "" {
		return u
	}
	if cfg := LoadGlobal(); cfg.Microwave.AuthURL != "" {
		return cfg.Microwave.AuthURL
	}
	return "https://auth.microwave.sh"
}

func ResolveCLIExchangeID() string {
	if id := os.Getenv("SANDBAR_MICROWAVE_CLI_EXCHANGE_ID"); id != "" {
		return id
	}
	return LoadGlobal().Microwave.CLIExchangeID
}

func ResolveGitHubActionsExchangeID() string {
	if id := os.Getenv("SANDBAR_MICROWAVE_GITHUB_ACTIONS_EXCHANGE_ID"); id != "" {
		return id
	}
	return LoadGlobal().Microwave.GitHubActionsExchangeID
}

// LoadProject reads .sandbar/config.toml from the given directory.
// Unknown keys are reported as errors (catches typos like `name` in a
// `[[domains]]` block, or `[[sites]]` instead of `[[domains]]`). Each
// declarative section is then validated for required fields so that
// a missing `hostname` / `repository` fails at load time rather than
// during the reconcile.
func LoadProject(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ".sandbar", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no .sandbar/config.toml found. Run `sandbar init` first")
	}
	var cfg ProjectConfig
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if err := validateUnknownKeys(md); err != nil {
		return nil, err
	}
	if err := validateProjectConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// validateUnknownKeys returns a single error describing every key
// in the file that didn't decode into ProjectConfig — typos and
// renamed-fields are the common cases. The [env] table is whitelisted
// because its shape is a free-form map[string]any and undecoded keys
// inside it are user-defined environment variables, not config typos.
func validateUnknownKeys(md toml.MetaData) error {
	var unknown []string
	for _, key := range md.Undecoded() {
		parts := []string(key)
		if len(parts) > 0 && parts[0] == "env" {
			// Free-form user env vars; not a config typo.
			continue
		}
		unknown = append(unknown, formatUnknownKey(parts))
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unknown key(s) in .sandbar/config.toml — check for typos or removed fields:\n  - %s",
		strings.Join(unknown, "\n  - "))
}

// knownFieldsBySection lists the legal field names for each section
// of .sandbar/config.toml. The empty-string key holds the top-level
// section names; subsections hold their leaf fields. Kept in sync
// with the structs above.
var knownFieldsBySection = map[string][]string{
	"":          {"site", "build", "deploy", "preview", "env", "domains", "trusts", "redirects", "headers"},
	"site":      {"slug", "name", "production_branch", "build_dir", "framework"},
	"build":     {"command"},
	"deploy":    {"auto_activate", "message_from_git"},
	"preview":   {"default_expiry"},
	"domains":   {"hostname", "redirect_to"},
	"trusts":    {"provider", "repository", "ref_filter", "environment"},
	"redirects": {"from", "to", "status", "force"},
	"headers":   {"for", "values"},
}

// formatUnknownKey renders a "did you mean" suggestion next to the
// offending key when there's a plausibly-close known field in the
// same section. Without a close match we fall back to listing every
// valid field so the user can spot the right one themselves.
func formatUnknownKey(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, ".")

	var section string
	var leaf string
	if len(parts) == 1 {
		// Top-level table like `[[sites]]`.
		section = ""
		leaf = parts[0]
	} else {
		// A leaf field inside a known section, e.g. `domains.name`.
		section = parts[0]
		leaf = parts[len(parts)-1]
	}

	candidates, ok := knownFieldsBySection[section]
	if !ok {
		return joined
	}

	if best := bestSuggestion(leaf, candidates); best != "" {
		return fmt.Sprintf("%s — did you mean `%s`?", joined, best)
	}

	// No close match: surface the whole legal set so the user can
	// pick. Sort for stable output.
	known := append([]string(nil), candidates...)
	sort.Strings(known)
	return fmt.Sprintf("%s (valid fields: %s)", joined, strings.Join(known, ", "))
}

// bestSuggestion picks the most likely intended field name. Substring
// containment beats edit distance — `name` → `hostname`, `repo` →
// `repository`, `ref` → `ref_filter` are all intuitive matches that
// pure Levenshtein would miss. Falls back to closest Levenshtein when
// no substring relationship exists, capped at distance 3 to avoid
// suggesting nonsense for genuinely unknown keys.
func bestSuggestion(unknown string, candidates []string) string {
	if unknown == "" || len(candidates) == 0 {
		return ""
	}
	lower := strings.ToLower(unknown)

	// First pass: substring containment, picking the shortest hit so
	// `name` prefers `hostname` over a hypothetical `hostname_alias`.
	var substrBest string
	for _, c := range candidates {
		cl := strings.ToLower(c)
		if cl == lower {
			continue
		}
		if strings.Contains(cl, lower) || strings.Contains(lower, cl) {
			if substrBest == "" || len(c) < len(substrBest) {
				substrBest = c
			}
		}
	}
	if substrBest != "" {
		return substrBest
	}

	// Second pass: closest Levenshtein within a small threshold.
	if best, dist := closestMatch(unknown, candidates); best != "" && dist <= 3 {
		return best
	}
	return ""
}

// closestMatch returns the candidate with the smallest Levenshtein
// distance from s, along with that distance. Empty s or empty
// candidates yields ("", -1).
func closestMatch(s string, candidates []string) (string, int) {
	if s == "" || len(candidates) == 0 {
		return "", -1
	}
	bestDist := -1
	best := ""
	for _, c := range candidates {
		d := levenshtein(s, c)
		if bestDist < 0 || d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best, bestDist
}

// levenshtein computes the edit distance between two short strings.
// Iterative two-row implementation; field names are tiny so the
// O(len(a)*len(b)) cost is fine.
func levenshtein(a, b string) int {
	la := len(a)
	lb := len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// validateProjectConfig surfaces required-field gaps so a deploy
// doesn't pass an empty hostname or repository to the reconcile.
// Returns a single error listing every problem found.
func validateProjectConfig(cfg *ProjectConfig) error {
	var problems []string

	if cfg.Site.Slug == "" && cfg.Site.Name == "" {
		problems = append(problems, "[site] must set `slug` (or legacy `name`)")
	}

	for i, d := range cfg.Domains {
		if strings.TrimSpace(d.Hostname) == "" {
			problems = append(problems, fmt.Sprintf("[[domains]] entry #%d is missing `hostname`", i+1))
		}
	}

	for i, t := range cfg.Trusts {
		if strings.TrimSpace(t.Repository) == "" {
			problems = append(problems, fmt.Sprintf("[[trusts]] entry #%d is missing `repository`", i+1))
		} else if !strings.Contains(t.Repository, "/") {
			problems = append(problems, fmt.Sprintf("[[trusts]] entry #%d: repository %q must be in `<owner>/<repo>` form", i+1, t.Repository))
		}
	}

	for i, r := range cfg.Redirects {
		switch {
		case strings.TrimSpace(r.From) == "":
			problems = append(problems, fmt.Sprintf("[[redirects]] entry #%d is missing `from`", i+1))
		case strings.TrimSpace(r.To) == "":
			problems = append(problems, fmt.Sprintf("[[redirects]] entry #%d is missing `to`", i+1))
		}
	}

	for i, h := range cfg.Headers {
		if strings.TrimSpace(h.For) == "" {
			problems = append(problems, fmt.Sprintf("[[headers]] entry #%d is missing `for`", i+1))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("invalid .sandbar/config.toml:\n  - %s", strings.Join(problems, "\n  - "))
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
