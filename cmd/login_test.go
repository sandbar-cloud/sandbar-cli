package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/config"
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

func TestFetchAuthConfigParsesBlocks(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		_, _ = w.Write([]byte(`{
			"github_actions": {
				"token_endpoint": "https://auth.microwave.sh/token",
				"resource": "https://auth.microwave.sh/trust-federations/tf_ci",
				"audience": "https://api.sandbar.cloud"
			},
			"device": {
				"request_url": "https://api.microwave.sh/auth/device",
				"token_url": "https://api.microwave.sh/auth/device/token",
				"exchange_id": "tex_dev"
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
	if ac.Device.RequestURL != "https://api.microwave.sh/auth/device" {
		t.Errorf("device request_url = %q", ac.Device.RequestURL)
	}
	if ac.Device.TokenURL != "https://api.microwave.sh/auth/device/token" {
		t.Errorf("device token_url = %q", ac.Device.TokenURL)
	}
	if ac.Device.ExchangeID != "tex_dev" {
		t.Errorf("device exchange_id = %q", ac.Device.ExchangeID)
	}
}

// TestLoginDeviceFlow drives the full interactive device-approval login against
// mock Sandbar (/auth/config) + Microwave (device request + token) servers and
// asserts the wire contract:
//   - GETs /auth/config off the Sandbar API base
//   - POSTs {trust_exchange_id} to the device request_url
//   - shows the user_code + static verification_uri, never the device_code
//   - polls the device token_url with the device_code
//   - stores the approved token
func TestLoginDeviceFlow(t *testing.T) {
	const (
		userCode   = "WXYZ-1234"
		deviceCode = "secret-device-code-do-not-show"
		verifyURI  = "https://app.sandbar.cloud/device"
		exchangeID = "tex_dev"
		wantToken  = "minted-session-jwt"
	)

	var (
		sawConfigPath   string
		sawExchangeID   string
		sawDeviceCode   string
		tokenPollCount  int
		sawAuthHeaderOK = true
	)

	// Microwave device endpoints.
	mwMux := http.NewServeMux()
	mwMux.HandleFunc("/auth/device", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("device request method = %q", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			sawAuthHeaderOK = false
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			TrustExchangeID string `json:"trust_exchange_id"`
		}
		_ = json.Unmarshal(body, &req)
		sawExchangeID = req.TrustExchangeID
		_, _ = w.Write([]byte(`{
			"device_code": "` + deviceCode + `",
			"user_code": "` + userCode + `",
			"verification_uri": "` + verifyURI + `",
			"expires_in": 300,
			"interval": 1
		}`))
	})
	mwMux.HandleFunc("/auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("device token method = %q", r.Method)
		}
		// RFC 8628 §3.4 device-grant poll: form-encoded grant_type + device_code.
		_ = r.ParseForm()
		sawDeviceCode = r.PostForm.Get("device_code")
		tokenPollCount++
		// First poll pending, second approved — proves the loop polls.
		if tokenPollCount < 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": "authorization_pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token": "` + wantToken + `", "token_type": "Bearer"}`))
	})
	mw := httptest.NewServer(mwMux)
	defer mw.Close()

	// Sandbar /auth/config advertising the mock Microwave device endpoints.
	sandbar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawConfigPath = r.URL.Path
		_, _ = w.Write([]byte(`{
			"github_actions": {"token_endpoint": "x", "resource": "y", "audience": "z"},
			"device": {
				"request_url": "` + mw.URL + `/auth/device",
				"token_url": "` + mw.URL + `/auth/device/token",
				"exchange_id": "` + exchangeID + `"
			}
		}`))
	}))
	defer sandbar.Close()

	t.Setenv("SANDBAR_API_URL", sandbar.URL)

	// Redirect the credential write into a test sandbox (GlobalConfigDir reads
	// HOME). Clear SANDBAR_TOKEN so ResolveToken reads the file we wrote.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SANDBAR_TOKEN", "")
	t.Setenv("PATH", "") // openBrowser's exec fails harmlessly; it's best-effort.

	// Capture stderr to assert what the operator sees.
	origStderr := os.Stderr
	rPipe, wPipe, _ := os.Pipe()
	os.Stderr = wPipe
	outCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(rPipe)
		outCh <- string(b)
	}()

	runErr := (&LoginCmd{}).loginDevice(&Globals{})

	_ = wPipe.Close()
	os.Stderr = origStderr
	printed := <-outCh

	if runErr != nil {
		t.Fatalf("loginDevice: %v\nstderr:\n%s", runErr, printed)
	}

	if sawConfigPath != "/auth/config" {
		t.Errorf("config path = %q, want /auth/config", sawConfigPath)
	}
	if sawExchangeID != exchangeID {
		t.Errorf("posted trust_exchange_id = %q, want %q", sawExchangeID, exchangeID)
	}
	if sawDeviceCode != deviceCode {
		t.Errorf("polled device_code = %q, want %q", sawDeviceCode, deviceCode)
	}
	if !sawAuthHeaderOK {
		t.Error("device request sent an Authorization header; the CLI holds no credential yet")
	}
	if tokenPollCount < 2 {
		t.Errorf("token poll count = %d, want >= 2 (pending then approved)", tokenPollCount)
	}
	if !strings.Contains(printed, userCode) {
		t.Errorf("stderr did not show the user_code %q:\n%s", userCode, printed)
	}
	if !strings.Contains(printed, verifyURI) {
		t.Errorf("stderr did not show the verification_uri %q:\n%s", verifyURI, printed)
	}
	if strings.Contains(printed, deviceCode) {
		t.Errorf("stderr leaked the secret device_code:\n%s", printed)
	}

	// The approved token must be persisted to the global config.
	got, err := config.ResolveToken(config.GlobalConfigDir())
	if err != nil {
		t.Fatalf("ResolveToken: %v", err)
	}
	if got != wantToken {
		t.Errorf("stored token = %q, want %q", got, wantToken)
	}
}
