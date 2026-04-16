package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mataki-dev/sandbar-cli/internal/client"
	"github.com/mataki-dev/sandbar-cli/internal/config"
	"github.com/mataki-dev/sandbar-cli/internal/output"
)

type LoginCmd struct{}

func (cmd *LoginCmd) Run(globals *Globals) error {
	// Detect GitHub Actions environment
	if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" {
		return cmd.loginGitHubOIDC(globals)
	}

	return cmd.loginDevice(globals)
}

func (cmd *LoginCmd) loginGitHubOIDC(globals *Globals) error {
	// Request OIDC token from GitHub Actions runtime
	requestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := os.Getenv("ACTIONS_RUNTIME_TOKEN")

	if requestURL == "" || requestToken == "" {
		return fmt.Errorf("GitHub Actions OIDC not available. Ensure `permissions: id-token: write` is set in your workflow")
	}

	sp := output.NewSpinner("Requesting GitHub OIDC token...")

	// Fetch OIDC token from GitHub's token endpoint
	httpClient := &http.Client{Timeout: 10 * time.Second}
	audience := config.ResolveAPIURL()
	if audience == "" {
		audience = "https://api.sandbar.cloud"
	}
	req, err := http.NewRequest("GET", requestURL+"&audience="+audience, nil)
	if err != nil {
		sp.Fail("Failed to create request")
		return err
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		sp.Fail("Failed to request OIDC token")
		return fmt.Errorf("failed to request OIDC token: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		sp.Fail("Failed to decode OIDC token")
		return fmt.Errorf("failed to decode OIDC token response: %w", err)
	}

	if tokenResp.Value == "" {
		sp.Fail("Empty OIDC token")
		return fmt.Errorf("received empty OIDC token. Check workflow permissions")
	}

	// Store as the session token — the API will validate it as a GitHub OIDC JWT
	if err := config.WriteGlobalAuth(tokenResp.Value); err != nil {
		sp.Fail("Failed to save token")
		return err
	}

	sp.Stop("Authenticated via GitHub OIDC")
	return nil
}

func (cmd *LoginCmd) loginDevice(globals *Globals) error {
	// Create an unauthenticated client for the device flow
	c := client.NewFromEnv("", globals.Version)

	sp := output.NewSpinner("Starting login...")
	code, err := c.RequestDeviceCode()
	if err != nil {
		sp.Fail("Failed to start login")
		return err
	}
	sp.Stop("Opening browser...")

	fmt.Printf("\n  If the browser didn't open, visit:\n  %s\n\n", output.Bold.Render(code.AuthorizeURL))

	// Try to open browser (ignore error — user can visit URL manually)
	openBrowser(code.AuthorizeURL) //nolint:errcheck

	// Poll for approval with interrupt support
	sp = output.NewSpinner("Waiting for authorization...")
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	interval := time.Duration(code.Interval) * time.Second
	if interval < 2*time.Second {
		interval = 5 * time.Second
	}

	for time.Now().Before(deadline) {
		select {
		case <-sp.CancelledC:
			return fmt.Errorf("login cancelled")
		case <-time.After(interval):
		}

		tokenResp, err := c.PollDeviceToken(code.DeviceCode)
		if err != nil {
			// 404 = device code deleted = user denied
			sp.Fail("Login denied")
			return fmt.Errorf("authorization denied or expired")
		}

		switch tokenResp.Status {
		case "approved":
			// Save token to global config
			if err := config.WriteGlobalAuth(tokenResp.Token); err != nil {
				sp.Fail("Failed to save credentials")
				return err
			}
			sp.Stop("Logged in")
			fmt.Printf("\n  Token saved to %s\n\n", output.Dim.Render(filepath.Join(config.GlobalConfigDir(), "config.toml")))
			return nil

		case "expired":
			sp.Fail("Login expired")
			return fmt.Errorf("authorization expired. Run `sandbar login` again")

		case "pending":
			continue

		default:
			sp.Fail("Login failed")
			return fmt.Errorf("unexpected status: %s", tokenResp.Status)
		}
	}

	sp.Fail("Login timed out")
	return fmt.Errorf("authorization timed out after %d seconds", code.ExpiresIn)
}

