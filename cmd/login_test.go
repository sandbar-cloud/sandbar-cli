package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestGitHubOIDCTokenURLEncodesAudience(t *testing.T) {
	got, err := githubOIDCTokenURL("https://actions.example/id-token?request=abc", "https://api.sandbar.cloud")
	if err != nil {
		t.Fatalf("githubOIDCTokenURL: %v", err)
	}

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if u.Query().Get("request") != "abc" {
		t.Fatalf("request query = %q", u.Query().Get("request"))
	}
	if u.Query().Get("audience") != "https://api.sandbar.cloud" {
		t.Fatalf("audience query = %q", u.Query().Get("audience"))
	}
	if u.RawQuery == "request=abc&audience=https://api.sandbar.cloud" {
		t.Fatalf("audience was not URL-encoded: %q", u.RawQuery)
	}
}

func TestFetchAuthConfigParsesBothBlocks(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		_, _ = w.Write([]byte(`{
			"github_actions": {
				"token_endpoint": "https://auth.microwave.sh/token",
				"resource": "https://auth.microwave.sh/trust-federations/tf_ci",
				"audience": "https://api.sandbar.cloud"
			},
			"cli": {
				"auth_metadata_url": "https://auth.microwave.sh/.well-known/oauth-authorization-server",
				"client_id": "tex_cli"
			}
		}`))
	}))
	defer srv.Close()

	// fetchAuthConfig reads the Sandbar API base from SANDBAR_API_URL.
	t.Setenv("SANDBAR_API_URL", srv.URL)

	ac, err := fetchAuthConfig(&http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("fetchAuthConfig: %v", err)
	}
	if sawPath != "/auth/config" {
		t.Fatalf("path = %q", sawPath)
	}
	if ac.GitHubActions.TokenEndpoint != "https://auth.microwave.sh/token" {
		t.Errorf("ci token_endpoint = %q", ac.GitHubActions.TokenEndpoint)
	}
	if ac.CLI.AuthMetadataURL != "https://auth.microwave.sh/.well-known/oauth-authorization-server" {
		t.Errorf("cli auth_metadata_url = %q", ac.CLI.AuthMetadataURL)
	}
	if ac.CLI.ClientID != "tex_cli" {
		t.Errorf("cli client_id = %q", ac.CLI.ClientID)
	}
}
