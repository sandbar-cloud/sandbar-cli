package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandbar-cloud/sandbar-cli/internal/config"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadProjectConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "my-site"
build_dir = "dist"
framework = "vite"

[deploy]
auto_activate = true
message_from_git = true

[preview]
default_expiry = "7d"
`)

	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Site.Name != "my-site" {
		t.Errorf("Site.Name = %q, want %q", cfg.Site.Name, "my-site")
	}
	if cfg.Site.BuildDir != "dist" {
		t.Errorf("Site.BuildDir = %q, want %q", cfg.Site.BuildDir, "dist")
	}
	if cfg.Site.Framework != "vite" {
		t.Errorf("Site.Framework = %q, want %q", cfg.Site.Framework, "vite")
	}
	if !cfg.Deploy.AutoActivate {
		t.Error("Deploy.AutoActivate = false, want true")
	}
	if !cfg.Deploy.MessageFromGit {
		t.Error("Deploy.MessageFromGit = false, want true")
	}
	if cfg.Preview.DefaultExpiry != "7d" {
		t.Errorf("Preview.DefaultExpiry = %q, want %q", cfg.Preview.DefaultExpiry, "7d")
	}
}

func TestLoadProjectConfig_NotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := config.LoadProject(dir)
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

func TestResolveToken_EnvOverridesFile(t *testing.T) {
	globalDir := t.TempDir()
	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[auth]
token = "file-key-xyz"
`)

	t.Setenv("SANDBAR_TOKEN", "env-key-abc")

	key, err := config.ResolveToken(globalDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "env-key-abc" {
		t.Errorf("key = %q, want %q", key, "env-key-abc")
	}
}

func TestResolveToken_FromGlobalConfig(t *testing.T) {
	globalDir := t.TempDir()
	writeFile(t, filepath.Join(globalDir, "config.toml"), `
[auth]
token = "file-key-xyz"
`)

	t.Setenv("SANDBAR_TOKEN", "")

	key, err := config.ResolveToken(globalDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "file-key-xyz" {
		t.Errorf("key = %q, want %q", key, "file-key-xyz")
	}
}

func TestResolveToken_NoneFound(t *testing.T) {
	globalDir := t.TempDir()

	t.Setenv("SANDBAR_TOKEN", "")

	_, err := config.ResolveToken(globalDir)
	if err == nil {
		t.Fatal("expected error when no token found, got nil")
	}
}

func TestEnvFor_DefaultsOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "x"

[env]
PUBLIC_APP_URL = "https://app.sandbar.cloud"
API_KEY = "default"
`)
	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := cfg.EnvFor("")
	if got["PUBLIC_APP_URL"] != "https://app.sandbar.cloud" {
		t.Errorf("PUBLIC_APP_URL = %q", got["PUBLIC_APP_URL"])
	}
	if got["API_KEY"] != "default" {
		t.Errorf("API_KEY = %q", got["API_KEY"])
	}
}

func TestEnvFor_NamedOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "x"

[env]
PUBLIC_APP_URL = "https://app.sandbar.cloud"
API_KEY = "default"

[env.staging]
PUBLIC_APP_URL = "https://app.staging.sandbar.cloud"
`)
	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := cfg.EnvFor("staging")
	if got["PUBLIC_APP_URL"] != "https://app.staging.sandbar.cloud" {
		t.Errorf("PUBLIC_APP_URL (staging) = %q", got["PUBLIC_APP_URL"])
	}
	// Default key not overridden in [env.staging] is still present.
	if got["API_KEY"] != "default" {
		t.Errorf("API_KEY (staging) = %q, want carried over", got["API_KEY"])
	}
}

func TestEnvFor_UnknownNameFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "x"

[env]
PUBLIC_APP_URL = "default"
`)
	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := cfg.EnvFor("nonexistent")
	if got["PUBLIC_APP_URL"] != "default" {
		t.Errorf("PUBLIC_APP_URL = %q", got["PUBLIC_APP_URL"])
	}
}

func TestHasEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "x"

[env]
A = "1"

[env.staging]
A = "2"
`)
	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !cfg.HasEnv("") {
		t.Error(`HasEnv("") = false, want true`)
	}
	if !cfg.HasEnv("staging") {
		t.Error(`HasEnv("staging") = false, want true`)
	}
	if cfg.HasEnv("prod") {
		t.Error(`HasEnv("prod") = true, want false`)
	}
}

func TestLoadProjectConfig_WithRedirectsAndHeaders(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "site-redirect-test"
build_dir = "public"

[[redirects]]
from = "/old-path"
to = "/new-path"
status = 301

[[redirects]]
from = "/gone"
to = "/replacement"
status = 302

[[headers]]
for = "/assets/*"
[headers.values]
Cache-Control = "public, max-age=31536000"
X-Content-Type-Options = "nosniff"

[[headers]]
for = "/*"
[headers.values]
X-Frame-Options = "DENY"
`)

	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Redirects) != 2 {
		t.Fatalf("len(Redirects) = %d, want 2", len(cfg.Redirects))
	}

	r := cfg.Redirects[0]
	if r.From != "/old-path" {
		t.Errorf("redirect[0].From = %q, want %q", r.From, "/old-path")
	}
	if r.To != "/new-path" {
		t.Errorf("redirect[0].To = %q, want %q", r.To, "/new-path")
	}
	if r.Status != 301 {
		t.Errorf("redirect[0].Status = %d, want 301", r.Status)
	}

	r2 := cfg.Redirects[1]
	if r2.From != "/gone" {
		t.Errorf("redirect[1].From = %q, want %q", r2.From, "/gone")
	}
	if r2.Status != 302 {
		t.Errorf("redirect[1].Status = %d, want 302", r2.Status)
	}

	if len(cfg.Headers) != 2 {
		t.Fatalf("len(Headers) = %d, want 2", len(cfg.Headers))
	}

	h := cfg.Headers[0]
	if h.For != "/assets/*" {
		t.Errorf("headers[0].For = %q, want %q", h.For, "/assets/*")
	}
	if h.Values["Cache-Control"] != "public, max-age=31536000" {
		t.Errorf("Cache-Control = %q, want %q", h.Values["Cache-Control"], "public, max-age=31536000")
	}
	if h.Values["X-Content-Type-Options"] != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", h.Values["X-Content-Type-Options"], "nosniff")
	}

	h2 := cfg.Headers[1]
	if h2.For != "/*" {
		t.Errorf("headers[1].For = %q, want %q", h2.For, "/*")
	}
	if h2.Values["X-Frame-Options"] != "DENY" {
		t.Errorf("X-Frame-Options = %q, want %q", h2.Values["X-Frame-Options"], "DENY")
	}
}

func TestLoadProjectConfig_Domains(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "my-site"

[[domains]]
hostname = "example.com"

[[domains]]
hostname = "www.example.com"
redirect_to = "example.com"
`)

	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if len(cfg.Domains) != 2 {
		t.Fatalf("Domains len = %d, want 2", len(cfg.Domains))
	}
	if cfg.Domains[0].Hostname != "example.com" || cfg.Domains[0].RedirectTo != "" {
		t.Errorf("apex = %+v, want hostname=example.com redirect_to=''", cfg.Domains[0])
	}
	if cfg.Domains[1].Hostname != "www.example.com" || cfg.Domains[1].RedirectTo != "example.com" {
		t.Errorf("www = %+v, want hostname=www.example.com redirect_to=example.com", cfg.Domains[1])
	}
}

func TestWriteProject_DomainsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.ProjectConfig{
		Site: config.SiteConfig{Slug: "my-site"},
		Domains: []config.DomainConfig{
			{Hostname: "example.com"},
			{Hostname: "www.example.com", RedirectTo: "example.com"},
		},
	}
	if err := config.WriteProject(dir, cfg); err != nil {
		t.Fatalf("WriteProject: %v", err)
	}

	loaded, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if len(loaded.Domains) != 2 {
		t.Fatalf("Domains len after round-trip = %d, want 2", len(loaded.Domains))
	}
	if loaded.Domains[1].RedirectTo != "example.com" {
		t.Errorf("redirect_to lost in round-trip: %q", loaded.Domains[1].RedirectTo)
	}
}

func TestSiteConfig_EffectiveSlug_NewShape(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
slug              = "mataki-web"
name              = "Mataki Web"
production_branch = "trunk"
`)
	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if got := cfg.Site.EffectiveSlug(); got != "mataki-web" {
		t.Errorf("EffectiveSlug = %q, want %q", got, "mataki-web")
	}
	if got := cfg.Site.DisplayName(); got != "Mataki Web" {
		t.Errorf("DisplayName = %q, want %q", got, "Mataki Web")
	}
	if cfg.Site.ProductionBranch != "trunk" {
		t.Errorf("ProductionBranch = %q, want %q", cfg.Site.ProductionBranch, "trunk")
	}
}

func TestSiteConfig_EffectiveSlug_LegacyShape(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
name = "mataki-web"
`)
	cfg, err := config.LoadProject(dir)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if got := cfg.Site.EffectiveSlug(); got != "mataki-web" {
		t.Errorf("legacy fallback: EffectiveSlug = %q, want %q", got, "mataki-web")
	}
	// In legacy mode, Name holds the slug — not a display name to sync.
	if got := cfg.Site.DisplayName(); got != "" {
		t.Errorf("legacy fallback: DisplayName = %q, want empty (would clobber server name)", got)
	}
}

func TestLoadProject_RejectsUnknownKeys(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want string // substring expected in the error
	}{
		{
			name: "domains entry with name instead of hostname",
			toml: `
[site]
slug = "s"

[[domains]]
name = "example.com"
`,
			want: "domains",
		},
		{
			name: "[[sites]] instead of [[domains]]",
			toml: `
[site]
slug = "s"

[[sites]]
hostname = "example.com"
`,
			want: "sites",
		},
		{
			name: "unknown field on [site]",
			toml: `
[site]
slug                = "s"
build_dir_unknown   = "dist"
`,
			want: "build_dir_unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), c.toml)
			_, err := config.LoadProject(dir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q should mention %q", err, c.want)
			}
		})
	}
}

func TestLoadProject_AllowsEnvFreeForm(t *testing.T) {
	// The [env] table is free-form (map[string]any) so any key inside
	// it should NOT trigger the unknown-key error.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), `
[site]
slug = "s"

[env]
PUBLIC_API_URL = "https://example.com"

[env.production]
NODE_ENV = "production"
`)
	if _, err := config.LoadProject(dir); err != nil {
		t.Errorf("env keys should be allowed: %v", err)
	}
}

func TestLoadProject_RequiredFields(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want string
	}{
		{
			name: "no slug or name",
			toml: `
[deploy]
auto_activate = true
`,
			want: "[site] must set",
		},
		{
			name: "domain missing hostname",
			toml: `
[site]
slug = "s"

[[domains]]
redirect_to = "example.com"
`,
			want: "[[domains]] entry #1 is missing `hostname`",
		},
		{
			name: "trust missing repository",
			toml: `
[site]
slug = "s"

[[trusts]]
ref_filter = "*"
`,
			want: "[[trusts]] entry #1 is missing `repository`",
		},
		{
			name: "trust repository without owner",
			toml: `
[site]
slug = "s"

[[trusts]]
repository = "just-a-repo"
`,
			want: "must be in `<owner>/<repo>` form",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, ".sandbar", "config.toml"), c.toml)
			_, err := config.LoadProject(dir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q should mention %q", err, c.want)
			}
		})
	}
}
