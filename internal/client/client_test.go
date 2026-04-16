package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_CreateSite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/sites" {
			t.Errorf("expected /sites, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("User-Agent") != "sandbar-cli/test" {
			t.Errorf("expected sandbar-cli/test User-Agent, got %s", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("API-Version") != APIVersion {
			t.Errorf("expected API-Version %s, got %s", APIVersion, r.Header.Get("API-Version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json Content-Type, got %s", r.Header.Get("Content-Type"))
		}

		var req CreateSiteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if req.Name != "my-site" {
			t.Errorf("expected name my-site, got %s", req.Name)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Site{
			Slug:      "my-site",
			Name:      "my-site",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", "test")
	site, err := c.CreateSite(CreateSiteRequest{Name: "my-site"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if site.Slug != "my-site" {
		t.Errorf("expected site slug my-site, got %s", site.Slug)
	}
}

func TestClient_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(APIError{
			Message: "invalid API key",
			Code:    "unauthorized",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "bad-key", "test")
	_, err := c.GetSite("site-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Message != "invalid API key" {
		t.Errorf("expected 'invalid API key', got %s", apiErr.Message)
	}
	if apiErr.Code != "unauthorized" {
		t.Errorf("expected code 'unauthorized', got %s", apiErr.Code)
	}
}

func TestClient_GetSite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sites/site-456" {
			t.Errorf("expected /sites/site-456, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Site{
			Slug: "test-site",
			Name: "Test Site",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", "test")
	site, err := c.GetSite("site-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if site.Slug != "test-site" {
		t.Errorf("expected site slug test-site, got %s", site.Slug)
	}
	if site.Name != "Test Site" {
		t.Errorf("expected name 'Test Site', got %s", site.Name)
	}
}

func TestClient_CreateDeploy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/sites/site-789/deploys" {
			t.Errorf("expected /sites/site-789/deploys, got %s", r.URL.Path)
		}

		var req CreateDeployRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if len(req.FileManifest) != 2 {
			t.Errorf("expected 2 files in manifest, got %d", len(req.FileManifest))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(CreateDeployResponse{
			DeployID: "deploy-abc",
			Uploads: []UploadInfo{
				{Path: "index.html", UploadURL: "https://storage.example.com/upload/1"},
			},
			Skipped:      []string{"style.css"},
			SkippedCount: 1,
			UploadCount:  1,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", "test")
	resp, err := c.CreateDeploy("site-789", CreateDeployRequest{
		Message: "initial deploy",
		FileManifest: []FileEntry{
			{Path: "index.html", Hash: "abc123", Size: 1024},
			{Path: "style.css", Hash: "def456", Size: 512},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DeployID != "deploy-abc" {
		t.Errorf("expected deploy ID deploy-abc, got %s", resp.DeployID)
	}
	if resp.UploadCount != 1 {
		t.Errorf("expected upload count 1, got %d", resp.UploadCount)
	}
	if resp.SkippedCount != 1 {
		t.Errorf("expected skipped count 1, got %d", resp.SkippedCount)
	}
}
