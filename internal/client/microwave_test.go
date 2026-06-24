package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRedeemMicrowaveFederation(t *testing.T) {
	var sawPath string
	var sawBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&sawBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "sandbar-ci-jwt",
			"expires_at": time.Now().Add(15 * time.Minute).Format(time.RFC3339),
			"scopes":     []string{"deploys:write"},
		})
	}))
	defer server.Close()

	client := NewMicrowaveClient(server.URL)
	out, err := client.RedeemTrustFederation("tf_github", "github-oidc-jwt")
	if err != nil {
		t.Fatalf("RedeemTrustFederation: %v", err)
	}

	if sawPath != "/api/trust-federations/tf_github/redeem" {
		t.Fatalf("path = %q", sawPath)
	}
	if sawBody["token"] != "github-oidc-jwt" {
		t.Fatalf("token body = %q", sawBody["token"])
	}
	if out.Token != "sandbar-ci-jwt" {
		t.Fatalf("token = %q", out.Token)
	}
}

func TestRedeemMicrowaveTrustExchange(t *testing.T) {
	var sawPath string
	var sawForm map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		sawForm = map[string]string{}
		for k := range r.PostForm {
			sawForm[k] = r.PostForm.Get(k)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "sandbar-ci-jwt",
			"issued_token_type": "urn:ietf:params:oauth:token-type:jwt",
			"token_type":        "Bearer",
			"expires_in":        900,
		})
	}))
	defer server.Close()

	client := NewMicrowaveClient(server.URL)
	out, err := client.RedeemTrustExchange("tex_github", "github-oidc-jwt")
	if err != nil {
		t.Fatalf("RedeemTrustExchange: %v", err)
	}

	if sawPath != "/token" {
		t.Fatalf("path = %q", sawPath)
	}
	if sawForm["subject_token"] != "github-oidc-jwt" {
		t.Fatalf("subject_token = %q", sawForm["subject_token"])
	}
	if sawForm["resource"] != server.URL+"/trust-exchanges/tex_github" {
		t.Fatalf("resource = %q", sawForm["resource"])
	}
	if out.Token != "sandbar-ci-jwt" {
		t.Fatalf("token = %q", out.Token)
	}
}
