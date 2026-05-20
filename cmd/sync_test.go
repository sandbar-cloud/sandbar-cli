package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
)

// TestSyncCmd_RunsAllReconcilersWithoutDeploy is the load-bearing
// guarantee: `sandbar sync` must hit every reconcile endpoint that a
// production `sandbar deploy` does, but it must NOT touch /deploys,
// /uploads, or /activate. Otherwise we've reinvented deploy.
func TestSyncCmd_RunsAllReconcilersWithoutDeploy(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		// reconcileSite: GET then PATCH on drift.
		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                "site_abc",
				"slug":              "site_abc",
				"name":              "old-name",
				"production_branch": "main",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/sites/site_abc":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "site_abc"})

		// reconcileDomains.
		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc/domains":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc/domains":
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"domain": map[string]any{
					"id":       "dom_new",
					"hostname": body["hostname"],
				},
				"dns_records": []any{},
			})

		// reconcileTrusts.
		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc/trusts":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case r.Method == http.MethodPost && r.URL.Path == "/sites/site_abc/trusts":
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "trust_new",
				"provider":    body["provider"],
				"repository":  body["repository"],
				"ref_filter":  body["ref_filter"],
				"environment": body["environment"],
			})

		// reconcilePreviewExpiry.
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/sites/site_abc/preview"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "site_abc"})

		default:
			t.Errorf("unexpected %s %s — sync should not touch this endpoint", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok", "test")

	cfg := &config.ProjectConfig{
		Site: config.SiteConfig{
			Name:             "new-name",
			ProductionBranch: "main",
		},
		Domains: []config.DomainConfig{
			{Hostname: "app.example.com"},
		},
		Trusts: []config.TrustConfig{
			{Repository: "owner/repo", RefFilter: "refs/heads/main*"},
		},
		Preview: config.PreviewConfig{DefaultExpiry: "7d"},
	}

	if err := (&SyncCmd{}).RunWith(c, "site_abc", cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Fail loudly if anything that smells like a deploy fires.
	for _, call := range calls {
		if strings.Contains(call, "/deploys") || strings.Contains(call, "/uploads") || strings.Contains(call, "/activate") {
			t.Errorf("sync touched deploy-path endpoint %q — should be reconcile-only", call)
		}
	}

	// Each reconciler should have hit its list endpoint, at minimum.
	for _, want := range []string{
		"GET /sites/site_abc",
		"GET /sites/site_abc/domains",
		"GET /sites/site_abc/trusts",
	} {
		if !containsCall(calls, want) {
			t.Errorf("expected call %q; got %v", want, calls)
		}
	}
}

// TestSyncCmd_PrintsProgress is the regression guard for the
// "sync silently exits" bug: every section must announce itself so
// users see what ran even when there are no diffs. We capture
// stdout, run a no-op sync, and assert the headers + skip notes +
// final tick are present.
func TestSyncCmd_PrintsProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                "site_abc",
				"slug":              "site_abc",
				"name":              "site_abc",
				"production_branch": "main",
			})
		}
	}))
	defer srv.Close()

	out := captureStdoutStderr(t, func() {
		c := client.New(srv.URL, "tok", "test")
		cfg := &config.ProjectConfig{
			Site: config.SiteConfig{Name: "site_abc", ProductionBranch: "main"},
		}
		if err := (&SyncCmd{}).RunWith(c, "site_abc", cfg); err != nil {
			t.Fatalf("sync: %v", err)
		}
	})

	for _, want := range []string{
		"Syncing",
		"site_abc",
		"site metadata",
		"domains",
		"OIDC trusts",
		"preview expiry",
		"no config",
		"Done",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected sync output to contain %q; got:\n%s", want, out)
		}
	}
}

func captureStdoutStderr(t *testing.T, fn func()) string {
	t.Helper()
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	doneOut := make(chan string, 1)
	doneErr := make(chan string, 1)
	go func() { b, _ := io.ReadAll(rOut); doneOut <- string(b) }()
	go func() { b, _ := io.ReadAll(rErr); doneErr <- string(b) }()

	fn()
	_ = wOut.Close()
	_ = wErr.Close()
	return <-doneOut + <-doneErr
}

// TestSyncCmd_NilSectionsAreSkipped guards against the helpers being
// called on nil slices — reconcileDomains/Trusts on a nil slice would
// list-then-delete every server entry, which is exactly the opposite
// of declarative-but-partial config.
func TestSyncCmd_NilSectionsAreSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sites/site_abc":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":                "site_abc",
				"slug":              "site_abc",
				"name":              "site_abc",
				"production_branch": "main",
			})
		default:
			t.Errorf("unexpected %s %s — nil cfg sections should be no-ops", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok", "test")
	cfg := &config.ProjectConfig{
		Site: config.SiteConfig{Name: "site_abc", ProductionBranch: "main"},
		// Domains, Trusts, Preview intentionally zero/nil.
	}
	if err := (&SyncCmd{}).RunWith(c, "site_abc", cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
}
