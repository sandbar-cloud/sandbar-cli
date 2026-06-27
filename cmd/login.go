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
	"sync/atomic"
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
	Device        deviceAuthConfig    `json:"device"`
}

// ghActionsAuthConfig is the CI redeem target: where + how to exchange a GitHub
// Actions OIDC token for a Sandbar CI session JWT.
type ghActionsAuthConfig struct {
	TokenEndpoint string `json:"token_endpoint"`
	Resource      string `json:"resource"`
	Audience      string `json:"audience"`
}

// deviceAuthConfig is the interactive login target: Microwave's device-flow
// request + token endpoints and the Sandbar CLI trust exchange Microwave mints
// the session through. The operator configures none of this.
type deviceAuthConfig struct {
	RequestURL string `json:"request_url"`
	TokenURL   string `json:"token_url"`
	ExchangeID string `json:"exchange_id"`
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

// loginDevice runs Microwave's device-approval flow for interactive login. The
// CLI learns Microwave's device endpoints + the Sandbar CLI trust exchange from
// Sandbar's /auth/config, starts a device authorization on Microwave, shows the
// operator a short code to enter in the Sandbar console, then polls Microwave
// until the operator approves it there and Microwave mints a session token.
func (cmd *LoginCmd) loginDevice(globals *Globals) error {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	sp := output.NewSpinner("Discovering login config...")
	ac, err := fetchAuthConfig(httpClient)
	if err != nil {
		sp.Fail("Failed to fetch auth config")
		return err
	}
	dev := ac.Device
	if dev.RequestURL == "" || dev.TokenURL == "" || dev.ExchangeID == "" {
		sp.Fail("Auth config is incomplete")
		return fmt.Errorf("auth config is missing the device login target")
	}
	sp.Stop("Login config ready")

	// Bridge Ctrl+C to the login: while a spinner holds the terminal in raw mode
	// the interrupt never reaches a signal handler, so cancel this context off
	// the spinner instead.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr := &loginProgress{cancel: cancel}

	// Drive Microwave's device-approval flow through the shared SDK, targeting
	// Sandbar's CLI trust exchange. The SDK owns the request → show code → poll
	// loop (the device_code never leaves it; only the static verification URL +
	// short user code reach the operator) and emits the phase events the spinner
	// renders. DeviceApprovalURL is the Microwave API base the auth-config's
	// request_url is rooted at.
	creds, err := auth.Login(ctx, auth.LoginConfig{
		Mode:              auth.LoginDeviceApproval,
		DeviceApprovalURL: strings.TrimSuffix(dev.RequestURL, "/auth/device"),
		TrustExchangeID:   dev.ExchangeID,
		HTTPClient:        httpClient,
		Output:            os.Stderr,
		Progress:          pr,
	})
	if err != nil {
		if pr.Cancelled() {
			return fmt.Errorf("login cancelled")
		}
		return err
	}

	if err := config.WriteGlobalAuth(creds.AccessToken); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\n  %s Logged in. Token saved to %s\n\n",
		output.Green.Render("✓"),
		output.Dim.Render(filepath.Join(config.GlobalConfigDir(), "config.toml")))
	return nil
}

// loginProgress renders auth.ProgressReporter phase events with the CLI spinner,
// so the SDK-driven device-approval flow shows the same animated steps the rest
// of `sandbar login` does. The SDK pairs each Begin with exactly one Succeed or
// Fail, so at most one spinner is live at a time. A Ctrl+C on the active spinner
// cancels the login context (Bubbletea swallows the interrupt in raw mode).
type loginProgress struct {
	cancel    context.CancelFunc
	sp        *output.Spinner
	watchDone chan struct{}
	cancelled atomic.Bool
}

func (p *loginProgress) Begin(message string) {
	p.sp = output.NewSpinner(message)
	p.watchDone = make(chan struct{})
	go func(sp *output.Spinner, done chan struct{}) {
		select {
		case <-sp.CancelledC:
			p.cancelled.Store(true)
			if p.cancel != nil {
				p.cancel()
			}
		case <-done:
		}
	}(p.sp, p.watchDone)
}

func (p *loginProgress) Succeed(message string) { p.finish(false, message) }
func (p *loginProgress) Fail(message string)    { p.finish(true, message) }

func (p *loginProgress) finish(failed bool, message string) {
	if p.sp == nil {
		return
	}
	sp := p.sp
	p.sp = nil
	close(p.watchDone)
	// On Ctrl+C the spinner already rendered its own "Cancelled" line and exited;
	// don't send to a finished program.
	if p.cancelled.Load() {
		return
	}
	if failed {
		sp.Fail(message)
	} else {
		sp.Stop(message)
	}
}

// Cancelled reports whether the operator interrupted the login with Ctrl+C.
func (p *loginProgress) Cancelled() bool { return p.cancelled.Load() }
