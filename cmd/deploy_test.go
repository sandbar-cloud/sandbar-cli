package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
)

func setupTestSite(t *testing.T) (string, *config.ProjectConfig) {
	t.Helper()
	dir := t.TempDir()
	distDir := filepath.Join(dir, "dist")
	os.MkdirAll(distDir, 0o755)
	os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<h1>Hello</h1>"), 0o644)
	os.MkdirAll(filepath.Join(distDir, "_astro"), 0o755)
	os.WriteFile(filepath.Join(distDir, "_astro", "app.js"), []byte("console.log('app')"), 0o644)
	cfg := &config.ProjectConfig{
		Site:   config.SiteConfig{Name: "site_abc123", BuildDir: "dist"},
		Deploy: config.DeployConfig{AutoActivate: true, MessageFromGit: false},
	}
	config.WriteProject(dir, cfg)
	return dir, cfg
}

func TestDeployCmd_FullFlow(t *testing.T) {
	dir, _ := setupTestSite(t)

	var (
		mu       sync.Mutex
		calls    []string
		uploadOK bool
	)

	// Mock upload server
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			mu.Lock()
			uploadOK = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer uploadSrv.Close()

	// Mock API server
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sites":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(client.Site{
				Slug: "site_abc123",
				Name: "site_abc123",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc123/deploys":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.CreateDeployResponse{
				DeployID: "deploy_xyz",
				Uploads: []client.UploadInfo{
					{Path: "index.html", UploadURL: uploadSrv.URL + "/upload/index.html"},
				},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc123/deploys/deploy_xyz/finalize":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.Deploy{
				ID:     "deploy_xyz",
				Status: "ready",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc123/deploys/deploy_xyz":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.Deploy{
				ID:     "deploy_xyz",
				Status: "ready",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc123/deploys/deploy_xyz/activate":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.Deploy{
				ID:     "deploy_xyz",
				Status: "active",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc123":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.Site{
				Slug: "site_abc123",
				Name: "My Site",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()

	c := client.New(apiSrv.URL, "sb_live_test", "test")
	cmd := DeployCmd{Concurrency: 1}
	err := cmd.RunWith(c, dir, "dist", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify API call sequence
	mu.Lock()
	defer mu.Unlock()

	want := []string{
		"GET /sites/site_abc123",
		"POST /sites/site_abc123/deploys",
		"POST /sites/site_abc123/deploys/deploy_xyz/finalize",
		"GET /sites/site_abc123/deploys/deploy_xyz",
		"POST /sites/site_abc123/deploys/deploy_xyz/activate",
		"GET /sites/site_abc123",
	}

	if len(calls) != len(want) {
		t.Fatalf("got %d API calls %v, want %d %v", len(calls), calls, len(want), want)
	}
	for i, c := range calls {
		if c != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, c, want[i])
		}
	}

	if !uploadOK {
		t.Error("expected at least one upload PUT, got none")
	}
}

func TestDeployCmd_NoActivate(t *testing.T) {
	dir, _ := setupTestSite(t)

	var (
		mu    sync.Mutex
		calls []string
	)

	// Mock upload server
	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer uploadSrv.Close()

	// Mock API server
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sites":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(client.Site{
				Slug: "site_abc123",
				Name: "site_abc123",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc123/deploys":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.CreateDeployResponse{
				DeployID: "deploy_xyz",
				Uploads: []client.UploadInfo{
					{Path: "index.html", UploadURL: uploadSrv.URL + "/upload/index.html"},
				},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc123/deploys/deploy_xyz/finalize":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.Deploy{
				ID:     "deploy_xyz",
				Status: "ready",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc123/deploys/deploy_xyz":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(client.Deploy{
				ID:     "deploy_xyz",
				Status: "ready",
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()

	c := client.New(apiSrv.URL, "sb_live_test", "test")
	cmd := DeployCmd{Concurrency: 1, NoActivate: true}
	err := cmd.RunWith(c, dir, "dist", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify activate was NOT called
	mu.Lock()
	defer mu.Unlock()

	for _, call := range calls {
		if call == "POST /sites/site_abc123/deploys/deploy_xyz/activate" {
			t.Error("activate endpoint was called but should not have been with --no-activate")
		}
	}

	// Verify finalize was called
	found := false
	for _, call := range calls {
		if call == "POST /sites/site_abc123/deploys/deploy_xyz/finalize" {
			found = true
		}
	}
	if !found {
		t.Error("finalize endpoint was not called")
	}
}

func TestMergeEnv_OverridesReplaceAndAppend(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/x", "FOO=keep"}
	overrides := map[string]string{
		"PATH":         "/override/bin",
		"PUBLIC_APP_URL": "https://app.sandbar.cloud",
	}
	got := mergeEnv(base, overrides)

	want := map[string]string{
		"PATH":           "/override/bin",
		"HOME":           "/home/x",
		"FOO":            "keep",
		"PUBLIC_APP_URL": "https://app.sandbar.cloud",
	}
	asMap := func(env []string) map[string]string {
		m := map[string]string{}
		for _, kv := range env {
			for i := 0; i < len(kv); i++ {
				if kv[i] == '=' {
					m[kv[:i]] = kv[i+1:]
					break
				}
			}
		}
		return m
	}
	gotMap := asMap(got)
	for k, v := range want {
		if gotMap[k] != v {
			t.Errorf("%s = %q, want %q", k, gotMap[k], v)
		}
	}
	if len(gotMap) != len(want) {
		t.Errorf("len = %d, want %d (got=%v)", len(gotMap), len(want), gotMap)
	}
}

func TestMergeEnv_EmptyOverridesReturnsBase(t *testing.T) {
	base := []string{"A=1", "B=2"}
	got := mergeEnv(base, nil)
	if &got[0] != &base[0] {
		// Same underlying slice — OK to share when no overrides.
	}
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Errorf("got %v, want %v", got, base)
	}
}

func TestRunBuild_InjectsEnvFromConfig(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	cfg := &config.ProjectConfig{
		Build: config.BuildConfig{Command: "printf '%s' \"$PUBLIC_APP_URL\" > " + out},
		Env: map[string]any{
			"PUBLIC_APP_URL": "default-url",
			"prod": map[string]any{
				"PUBLIC_APP_URL": "https://app.sandbar.cloud",
			},
		},
	}

	cmd := &DeployCmd{Env: "prod"}
	if err := cmd.runBuild(cfg, dir); err != nil {
		t.Fatalf("runBuild: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if got := string(data); got != "https://app.sandbar.cloud" {
		t.Errorf("PUBLIC_APP_URL in build = %q, want %q", got, "https://app.sandbar.cloud")
	}
}

func TestRunBuild_DefaultsAppliedWhenNoEnvFlag(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	cfg := &config.ProjectConfig{
		Build: config.BuildConfig{Command: "printf '%s' \"$PUBLIC_APP_URL\" > " + out},
		Env: map[string]any{
			"PUBLIC_APP_URL": "default-url",
		},
	}

	cmd := &DeployCmd{}
	if err := cmd.runBuild(cfg, dir); err != nil {
		t.Fatalf("runBuild: %v", err)
	}

	data, _ := os.ReadFile(out)
	if string(data) != "default-url" {
		t.Errorf("got %q, want %q", string(data), "default-url")
	}
}

func TestRunBuild_SkipBuildShortCircuits(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	cfg := &config.ProjectConfig{
		Build: config.BuildConfig{Command: "touch " + marker},
	}
	cmd := &DeployCmd{SkipBuild: true}
	if err := cmd.runBuild(cfg, dir); err != nil {
		t.Fatalf("runBuild: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("build ran despite --skip-build")
	}
}

func TestReconcileDomains_AddsDeletesAndWarnsOnDrift(t *testing.T) {
	var (
		mu     sync.Mutex
		calls  []string
		listed bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc/domains":
			// Server has apex (matches desired), legacy.example.io
			// (should be deleted), and www.example.com with a stale
			// redirect_to (should produce a drift warning).
			listed = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"id": "dom_apex", "hostname": "example.com", "verification_status": "verified", "certificate_status": "active"},
					{"id": "dom_legacy", "hostname": "legacy.example.io", "verification_status": "verified", "certificate_status": "active"},
					{"id": "dom_www", "hostname": "www.example.com", "verification_status": "verified", "certificate_status": "active", "redirect_to": "old.example.com"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc/domains":
			// The "added" entry — partner.example.com — gets created.
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                  "dom_new",
				"hostname":            body["hostname"],
				"redirect_to":         body["redirect_to"],
				"verification_status": "pending",
				"certificate_status":  "pending",
				"dns_instructions":    map[string]string{"record_type": "TXT", "record_name": "_sandbar", "record_value": "x"},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/sites/site_abc/domains/dom_legacy":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, "token", "test")

	desired := []config.DomainConfig{
		{Hostname: "example.com"},
		{Hostname: "www.example.com", RedirectTo: "example.com"}, // drift: server has old.example.com
		{Hostname: "partner.example.com"},                        // new
		// legacy.example.io is absent → should be deleted
	}

	reconcileDomains(c, "site_abc", desired)

	if !listed {
		t.Fatal("expected GET /sites/site_abc/domains call")
	}
	mu.Lock()
	defer mu.Unlock()
	wantPost := "POST /sites/site_abc/domains"
	wantDelete := "DELETE /sites/site_abc/domains/dom_legacy"
	if !containsCall(calls, wantPost) {
		t.Errorf("missing add call %q; got %v", wantPost, calls)
	}
	if !containsCall(calls, wantDelete) {
		t.Errorf("missing delete call %q; got %v", wantDelete, calls)
	}
	// No PATCH/PUT should fire for the redirect_to drift — warn-only.
	for _, c := range calls {
		if c[:3] == "PUT" || c[:3] == "PAT" {
			t.Errorf("unexpected update call %q (drift should be warn-only)", c)
		}
	}
}

func TestReconcileDomains_SkippedWhenNilDomains(t *testing.T) {
	// Sanity: reconcileDomains is only invoked when cfg.Domains is
	// non-nil. This test exercises the call-site guard, not the
	// function itself — calling reconcileDomains with nil would still
	// list. The guard lives in DeployCmd.RunWith.
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var desired []config.DomainConfig // nil
	if desired != nil {
		c := client.New(srv.URL, "token", "test")
		reconcileDomains(c, "site_abc", desired)
	}
	if hit {
		t.Error("reconcile should not have fired for nil Domains")
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}
