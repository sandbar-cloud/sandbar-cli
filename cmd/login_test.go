package cmd

import (
	"net/url"
	"testing"
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
