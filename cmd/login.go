package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/microwave-sh/microwave-go/auth"
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

// authConfig mirrors Sandbar's public /auth/config discovery document. The CLI
// reads it — holding no Sandbar credential — to learn how to authenticate
// without any locally-configured Microwave detail.
type authConfig struct {
	GitHubActions ghActionsAuthConfig `json:"github_actions"`
	CLI           cliAuthConfig       `json:"cli"`
}

// ghActionsAuthConfig is the CI redeem target: where + how to exchange a GitHub
// Actions OIDC token for a Sandbar CI session JWT.
type ghActionsAuthConfig struct {
	TokenEndpoint string `json:"token_endpoint"`
	Resource      string `json:"resource"`
	Audience      string `json:"audience"`
}

// cliAuthConfig is the operator login target: the Microwave authorization-server
// metadata document the shared SDK login core discovers, and the client id (the
// trust exchange) it authenticates against. The operator configures none of this.
type cliAuthConfig struct {
	AuthMetadataURL string `json:"auth_metadata_url"`
	ClientID        string `json:"client_id"`
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
	ac, err := fetchAuthConfig(httpClient)
	if err != nil {
		sp.Fail("Failed to fetch auth config")
		return err
	}
	ci := ac.GitHubActions
	if ci.TokenEndpoint == "" || ci.Resource == "" {
		sp.Fail("Auth config is incomplete")
		return fmt.Errorf("auth config is missing the github_actions redeem target")
	}

	sp.UpdateMsg("Requesting GitHub OIDC token...")
	oidcToken, err := requestGitHubOIDCToken(httpClient, requestURL, requestToken, ci.Audience)
	if err != nil {
		sp.Fail("Failed to request OIDC token")
		return err
	}

	sp.UpdateMsg("Exchanging for a Sandbar CI session...")
	redeemed, err := auth.RedeemTokenExchange(context.Background(), httpClient, ci.TokenEndpoint, ci.Resource, oidcToken)
	if err != nil {
		sp.Fail("Token exchange failed")
		// The SDK returns a typed *auth.OAuthError carrying the server's error +
		// error_description, so this surfaces e.g. "invalid_grant: policy denied:
		// assertion.repository did not match" rather than a bare "HTTP 400".
		return fmt.Errorf("exchange GitHub OIDC token for a Sandbar CI session: %w", err)
	}

	// Store the Microwave-issued Sandbar CI JWT. Sandbar never sees the raw OIDC.
	if err := config.WriteGlobalAuth(redeemed.AccessToken); err != nil {
		sp.Fail("Failed to save token")
		return err
	}

	sp.Stop("Authenticated via GitHub OIDC")
	return nil
}

// fetchAuthConfig reads Sandbar's public /auth/config discovery document. The
// CLI holds no Sandbar credential at this point.
func fetchAuthConfig(c *http.Client) (authConfig, error) {
	apiURL := config.ResolveAPIURL()
	if apiURL == "" {
		apiURL = "https://api.sandbar.cloud"
	}
	resp, err := c.Get(strings.TrimRight(apiURL, "/") + "/auth/config")
	if err != nil {
		return authConfig{}, fmt.Errorf("fetch auth config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return authConfig{}, fmt.Errorf("auth config unavailable: HTTP %d", resp.StatusCode)
	}
	var doc authConfig
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return authConfig{}, fmt.Errorf("decode auth config: %w", err)
	}
	return doc, nil
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
	httpClient := &http.Client{Timeout: 90 * time.Second}

	sp := output.NewSpinner("Discovering login config...")
	ac, err := fetchAuthConfig(httpClient)
	if err != nil {
		sp.Fail("Failed to fetch auth config")
		return err
	}
	cli := ac.CLI
	if cli.AuthMetadataURL == "" || cli.ClientID == "" {
		sp.Fail("Auth config is incomplete")
		return fmt.Errorf("auth config is missing the cli login target")
	}
	// Hand off to the shared Microwave SDK login core: it discovers the
	// authorization server, runs the loopback authorization-code + PKCE flow
	// (brokered to the tenant IdP), and falls back to the device grant when no
	// browser is available. It owns its own progress output, so stop our spinner
	// first to avoid two writers fighting the terminal.
	sp.Stop("Found login config")

	creds, err := auth.Login(context.Background(), auth.LoginConfig{
		MetadataURL: cli.AuthMetadataURL,
		ClientID:    cli.ClientID,
		Mode:        auth.LoginAuto,
		HTTPClient:  httpClient,
		Output:      os.Stderr,
	})
	if err != nil {
		return err
	}

	if err := config.WriteGlobalAuth(creds.AccessToken); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	fmt.Printf("\n  %s Logged in. Token saved to %s\n\n",
		output.Green.Render("✓"),
		output.Dim.Render(filepath.Join(config.GlobalConfigDir(), "config.toml")))
	return nil
}
