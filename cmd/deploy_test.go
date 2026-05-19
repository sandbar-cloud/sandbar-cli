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
