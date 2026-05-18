//go:build integration

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/cmd"
	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
)

func TestInitDeployActivateFlow(t *testing.T) {
	const (
		siteID   = "site-abc123"
		deployID = "deploy-xyz789"
		siteSlug = "my-test-site"
	)

	// --- Mock upload server ---
	var uploadCalled int32
	uploadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("upload server: expected PUT, got %s", r.Method)
		}
		atomic.AddInt32(&uploadCalled, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer uploadServer.Close()

	// --- Mock API server ---
	var activateCalled int32

	mux := http.NewServeMux()

	// POST /sites → create site
	mux.HandleFunc("POST /sites", func(w http.ResponseWriter, r *http.Request) {
		resp := client.Site{
			ID:        siteID,
			Slug:      siteSlug,
			Name:      "my-test-site",
			BuildDir:  "dist",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /sites/{id}/deploys → create deploy
	mux.HandleFunc("POST /sites/{siteID}/deploys", func(w http.ResponseWriter, r *http.Request) {
		resp := client.CreateDeployResponse{
			DeployID: deployID,
			Uploads: []client.UploadInfo{
				{
					Path:      "index.html",
					UploadURL: uploadServer.URL + "/upload/index.html",
				},
			},
			SkippedCount: 0,
			UploadCount:  1,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /sites/{id}/deploys/{id}/finalize
	mux.HandleFunc("POST /sites/{siteID}/deploys/{deployID}/finalize", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// GET /sites/{id}/deploys/{id} → status "ready"
	mux.HandleFunc("GET /sites/{siteID}/deploys/{deployID}", func(w http.ResponseWriter, r *http.Request) {
		resp := client.Deploy{
			ID:        deployID,
			SiteID:    siteID,
			Status:    "ready",
			FileCount: 1,
			CreatedAt: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /sites/{id}/deploys/{id}/activate
	mux.HandleFunc("POST /sites/{siteID}/deploys/{deployID}/activate", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&activateCalled, 1)
		w.WriteHeader(http.StatusOK)
	})

	// GET /sites/{id} → site details
	mux.HandleFunc("GET /sites/{siteID}", func(w http.ResponseWriter, r *http.Request) {
		resp := client.Site{
			ID:             siteID,
			Slug:           siteSlug,
			Name:           "my-test-site",
			BuildDir:       "dist",
			ActiveDeployID: deployID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	apiServer := httptest.NewServer(mux)
	defer apiServer.Close()

	// --- Set up temp directory with dist/index.html ---
	tmpDir := t.TempDir()
	distDir := filepath.Join(tmpDir, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		t.Fatalf("failed to create dist dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html><body>hello</body></html>"), 0o644); err != nil {
		t.Fatalf("failed to write index.html: %v", err)
	}

	// --- Create client pointing at mock API server ---
	c := client.New(apiServer.URL, "test-api-key", "0.0.0-test")

	// --- Step 1: Run InitCmd ---
	initCmd := &cmd.InitCmd{
		Name: "my-test-site",
		Dir:  "dist",
	}
	if err := initCmd.RunWith(c, tmpDir); err != nil {
		t.Fatalf("InitCmd.RunWith failed: %v", err)
	}

	// Verify config was written with correct site ID
	cfg, err := config.LoadProject(tmpDir)
	if err != nil {
		t.Fatalf("failed to load project config: %v", err)
	}
	if cfg.Site.ID != siteID {
		t.Errorf("config site ID = %q, want %q", cfg.Site.ID, siteID)
	}
	if cfg.Site.BuildDir != "dist" {
		t.Errorf("config build dir = %q, want %q", cfg.Site.BuildDir, "dist")
	}

	// --- Step 2: Run DeployCmd ---
	deployCmd := &cmd.DeployCmd{}
	if err := deployCmd.RunWith(c, tmpDir, "dist", nil); err != nil {
		t.Fatalf("DeployCmd.RunWith failed: %v", err)
	}

	// Verify activate endpoint was called
	if n := atomic.LoadInt32(&activateCalled); n != 1 {
		t.Errorf("activate called %d times, want 1", n)
	}

	// Verify upload was called
	if n := atomic.LoadInt32(&uploadCalled); n < 1 {
		t.Errorf("upload called %d times, want >= 1", n)
	}
}
