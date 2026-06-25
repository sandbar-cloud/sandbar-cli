package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedeemTokenExchange(t *testing.T) {
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

	resource := server.URL + "/trust-federations/tf_ci"
	out, err := RedeemTokenExchange(server.URL+"/token", resource, "github-oidc-jwt")
	if err != nil {
		t.Fatalf("RedeemTokenExchange: %v", err)
	}

	if sawPath != "/token" {
		t.Fatalf("path = %q", sawPath)
	}
	if sawForm["subject_token"] != "github-oidc-jwt" {
		t.Fatalf("subject_token = %q", sawForm["subject_token"])
	}
	if sawForm["resource"] != resource {
		t.Fatalf("resource = %q", sawForm["resource"])
	}
	if sawForm["grant_type"] != "urn:ietf:params:oauth:grant-type:token-exchange" {
		t.Fatalf("grant_type = %q", sawForm["grant_type"])
	}
	if out.Token != "sandbar-ci-jwt" {
		t.Fatalf("token = %q", out.Token)
	}
}

func TestMicrowaveDeviceFlow(t *testing.T) {
	var requested bool
	var polled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/device":
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if body["trust_exchange_id"] != "tex_cli" {
				t.Fatalf("trust_exchange_id = %q", body["trust_exchange_id"])
			}
			requested = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":   "device_123",
				"user_code":     "ABCD-EFGH",
				"authorize_url": serverURL(r) + "/authorize/device_123?trust_exchange_id=tex_cli",
				"expires_in":    900,
				"interval":      5,
			})
		case "/auth/device/token":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode poll: %v", err)
			}
			if body["device_code"] != "device_123" {
				t.Fatalf("device_code = %q", body["device_code"])
			}
			polled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "approved",
				"token":  "sandbar-cli-jwt",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewMicrowaveClient(server.URL)
	code, err := client.RequestDeviceCode("tex_cli")
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if code.DeviceCode != "device_123" {
		t.Fatalf("device code = %q", code.DeviceCode)
	}
	token, err := client.PollDeviceToken(code.DeviceCode)
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if token.Token != "sandbar-cli-jwt" || token.Status != "approved" {
		t.Fatalf("token response = %#v", token)
	}
	if !requested || !polled {
		t.Fatalf("requested=%v polled=%v", requested, polled)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
