package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartDeviceAuth_PostsExchangeAndParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		// The CLI holds no Sandbar credential here — no Authorization header.
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header: %q", r.Header.Get("Authorization"))
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["trust_exchange_id"] != "tex_dev" {
			t.Errorf("trust_exchange_id = %q, want tex_dev", req["trust_exchange_id"])
		}
		_, _ = w.Write([]byte(`{
			"device_code": "dev-secret",
			"user_code": "WXYZ-1234",
			"verification_uri": "https://app.sandbar.cloud/device",
			"expires_in": 300,
			"interval": 5
		}`))
	}))
	defer srv.Close()

	c := &http.Client{Timeout: 5 * time.Second}
	da, err := StartDeviceAuth(c, srv.URL, "tex_dev")
	if err != nil {
		t.Fatalf("StartDeviceAuth: %v", err)
	}
	if da.DeviceCode != "dev-secret" {
		t.Errorf("device_code = %q", da.DeviceCode)
	}
	if da.UserCode != "WXYZ-1234" {
		t.Errorf("user_code = %q", da.UserCode)
	}
	if da.VerificationURI != "https://app.sandbar.cloud/device" {
		t.Errorf("verification_uri = %q", da.VerificationURI)
	}
	if da.ExpiresIn != 300 || da.Interval != 5 {
		t.Errorf("expires_in/interval = %d/%d", da.ExpiresIn, da.Interval)
	}
}

func TestPollDeviceToken_PostsDeviceCodeAndParsesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req["device_code"] != "dev-secret" {
			t.Errorf("device_code = %q, want dev-secret", req["device_code"])
		}
		_, _ = w.Write([]byte(`{"status": "approved", "token": "session-jwt"}`))
	}))
	defer srv.Close()

	c := &http.Client{Timeout: 5 * time.Second}
	res, err := PollDeviceToken(c, srv.URL, "dev-secret")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if res.Status != "approved" {
		t.Errorf("status = %q", res.Status)
	}
	if res.Token != "session-jwt" {
		t.Errorf("token = %q", res.Token)
	}
}

func TestPollDeviceToken_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer srv.Close()

	c := &http.Client{Timeout: 5 * time.Second}
	if _, err := PollDeviceToken(c, srv.URL, "dev-secret"); err == nil {
		t.Fatal("expected an error for a non-2xx device-token response")
	}
}
