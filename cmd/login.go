package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/client"
	"github.com/sandbar-cloud/sandbar-cli/internal/config"
	"github.com/sandbar-cloud/sandbar-cli/internal/output"
)

type LoginCmd struct{}

func (cmd *LoginCmd) Run(globals *Globals) error {
	// Detect GitHub Actions environment
	if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" {
		return cmd.loginGitHubOIDC(globals)
	}

	return cmd.loginDevice(globals)
}

// ciAuthConfig is the github_actions block of Sandbar's public /auth/config
// discovery document: where + how to redeem a GitHub Actions OIDC token.
type ciAuthConfig struct {
	TokenEndpoint string `json:"token_endpoint"`
	Resource      string `json:"resource"`
	Audience      string `json:"audience"`
}

// loginGitHubOIDC redeems a GitHub Actions OIDC token for a Sandbar CI session
// JWT: discover the redeem target from Sandbar, mint a GitHub OIDC token for that
// audience, then exchange it via Microwave's standard RFC 8693 token endpoint.
// The minted session JWT — not the raw OIDC token — is what Sandbar verifies.
func (cmd *LoginCmd) loginGitHubOIDC(globals *Globals) error {
	requestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if requestURL == "" || requestToken == "" {
		return fmt.Errorf("GitHub Actions OIDC not available. Ensure `permissions: id-token: write` is set in your workflow")
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}

	sp := output.NewSpinner("Discovering CI auth config...")
	ci, err := fetchCIAuthConfig(httpClient)
	if err != nil {
		sp.Fail("Failed to fetch auth config")
		return err
	}

	sp = output.NewSpinner("Requesting GitHub OIDC token...")
	oidcToken, err := requestGitHubOIDCToken(httpClient, requestURL, requestToken, ci.Audience)
	if err != nil {
		sp.Fail("Failed to request OIDC token")
		return err
	}

	sp = output.NewSpinner("Exchanging for a Sandbar CI session...")
	redeemed, err := client.RedeemTokenExchange(ci.TokenEndpoint, ci.Resource, oidcToken)
	if err != nil {
		sp.Fail("Token exchange failed")
		return err
	}

	// Store the Microwave-issued Sandbar CI JWT. Sandbar never sees the raw OIDC.
	if err := config.WriteGlobalAuth(redeemed.Token); err != nil {
		sp.Fail("Failed to save token")
		return err
	}

	sp.Stop("Authenticated via GitHub OIDC")
	return nil
}

// fetchCIAuthConfig reads Sandbar's public /auth/config for the github_actions
// redeem target. The CLI holds no Sandbar credential at this point.
func fetchCIAuthConfig(c *http.Client) (ciAuthConfig, error) {
	apiURL := config.ResolveAPIURL()
	if apiURL == "" {
		apiURL = "https://api.sandbar.cloud"
	}
	resp, err := c.Get(strings.TrimRight(apiURL, "/") + "/auth/config")
	if err != nil {
		return ciAuthConfig{}, fmt.Errorf("fetch auth config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ciAuthConfig{}, fmt.Errorf("auth config unavailable: HTTP %d", resp.StatusCode)
	}
	var doc struct {
		GitHubActions ciAuthConfig `json:"github_actions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return ciAuthConfig{}, fmt.Errorf("decode auth config: %w", err)
	}
	if doc.GitHubActions.TokenEndpoint == "" || doc.GitHubActions.Resource == "" {
		return ciAuthConfig{}, fmt.Errorf("auth config is missing the github_actions redeem target")
	}
	return doc.GitHubActions, nil
}

// requestGitHubOIDCToken mints a GitHub Actions OIDC token for the given audience.
func requestGitHubOIDCToken(c *http.Client, requestURL, requestToken, audience string) (string, error) {
	tokenURL, err := githubOIDCTokenURL(requestURL, audience)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("request OIDC token: %w", err)
	}
	defer resp.Body.Close()
	var tokenResp struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode OIDC token: %w", err)
	}
	if tokenResp.Value == "" {
		return "", fmt.Errorf("received empty OIDC token; check workflow permissions")
	}
	return tokenResp.Value, nil
}

func githubOIDCTokenURL(requestURL, audience string) (string, error) {
	u, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("parse GitHub OIDC request URL: %w", err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (cmd *LoginCmd) loginDevice(globals *Globals) error {
	exchangeID := config.ResolveCLIExchangeID()
	if exchangeID == "" {
		return fmt.Errorf("SANDBAR_MICROWAVE_CLI_EXCHANGE_ID is required for CLI login")
	}
	microwaveClient := client.NewMicrowaveClient(config.ResolveMicrowaveAPIURL())

	sp := output.NewSpinner("Starting login...")
	code, err := microwaveClient.RequestDeviceCode(exchangeID)
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

		tokenResp, err := microwaveClient.PollDeviceToken(code.DeviceCode)
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
