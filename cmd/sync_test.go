package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
