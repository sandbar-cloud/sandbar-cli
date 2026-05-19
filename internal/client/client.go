package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sandbar-cloud/sandbar-cli/internal/config"
)

const (
	APIVersion     = "2026-04-14"
	defaultBaseURL = "https://api.sandbar.cloud"
)

// Client wraps all Sandbar API calls.
type Client struct {
	baseURL    string
	token      string
	version    string
	httpClient *http.Client
}

// New creates a Client with the given base URL, API key, and CLI version string.
func New(baseURL, token, version string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		version: version,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewFromEnv creates a Client. Base URL priority: explicit override > SANDBAR_API_URL env > global config > default.
func NewFromEnv(token, version string) *Client {
	return NewWithBaseURL("", token, version)
}

// NewWithBaseURL creates a Client with an optional base URL override.
// If override is empty, falls back to SANDBAR_API_URL env > global config > default.
func NewWithBaseURL(override, token, version string) *Client {
	baseURL := override
	if baseURL == "" {
		baseURL = config.ResolveAPIURL()
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return New(baseURL, token, version)
}

// do is the central HTTP helper: marshals body (if non-nil), sends the request,
// checks for API errors, and unmarshals the response into result (if non-nil).
func (c *Client) do(method, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "sandbar-cli/"+c.version)
	req.Header.Set("API-Version", APIVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach Sandbar API at %s. Check your connection", c.baseURL)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if jsonErr := json.Unmarshal(respBody, &apiErr); jsonErr == nil && (apiErr.Message != "" || apiErr.Detail != "") {
			apiErr.StatusCode = resp.StatusCode
			return &apiErr
		}
		return &APIError{Message: fmt.Sprintf("HTTP %d", resp.StatusCode), StatusCode: resp.StatusCode}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// doPublic is like do but skips the Authorization header. Used for public endpoints
// such as the device auth flow.
func (c *Client) doPublic(method, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "sandbar-cli/"+c.version)
	req.Header.Set("API-Version", APIVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach Sandbar API at %s. Check your connection", c.baseURL)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if jsonErr := json.Unmarshal(respBody, &apiErr); jsonErr == nil && (apiErr.Message != "" || apiErr.Detail != "") {
			apiErr.StatusCode = resp.StatusCode
			return &apiErr
		}
		return &APIError{Message: fmt.Sprintf("HTTP %d", resp.StatusCode), StatusCode: resp.StatusCode}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}

	return nil
}

// --- Auth ---

// RequestDeviceCode starts the device authorization flow.
func (c *Client) RequestDeviceCode() (*DeviceCodeResponse, error) {
	var result DeviceCodeResponse
	if err := c.doPublic("POST", "/auth/device", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PollDeviceToken polls for the device authorization token.
func (c *Client) PollDeviceToken(deviceCode string) (*DeviceTokenResponse, error) {
	var result DeviceTokenResponse
	if err := c.doPublic("POST", "/auth/device/token", map[string]string{"device_code": deviceCode}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// --- Sites ---

func (c *Client) CreateSite(req CreateSiteRequest) (*Site, error) {
	var site Site
	if err := c.do(http.MethodPost, "/sites", req, &site); err != nil {
		return nil, err
	}
	return &site, nil
}

func (c *Client) GetSite(slug string) (*Site, error) {
	var site Site
	if err := c.do(http.MethodGet, "/sites/"+slug, nil, &site); err != nil {
		return nil, err
	}
	return &site, nil
}

func (c *Client) SearchSites(query string) (*SearchResponse[Site], error) {
	body := map[string]string{}
	if query != "" {
		body["q"] = query
	}
	var resp SearchResponse[Site]
	if err := c.do(http.MethodPost, "/sites/search", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateSite(slug string, req UpdateSiteRequest) (*Site, error) {
	var site Site
	if err := c.do(http.MethodPatch, "/sites/"+slug, req, &site); err != nil {
		return nil, err
	}
	return &site, nil
}

func (c *Client) DeleteSite(slug string) error {
	return c.do(http.MethodDelete, "/sites/"+slug, nil, nil)
}

// --- Deploys ---

func (c *Client) CreateDeploy(slug string, req CreateDeployRequest) (*CreateDeployResponse, error) {
	var resp CreateDeployResponse
	if err := c.do(http.MethodPost, "/sites/"+slug+"/deploys", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) FinalizeDeploy(slug, deployID string) error {
	return c.do(http.MethodPost, "/sites/"+slug+"/deploys/"+deployID+"/finalize", nil, nil)
}

func (c *Client) ActivateDeploy(slug, deployID string) error {
	return c.do(http.MethodPost, "/sites/"+slug+"/deploys/"+deployID+"/activate", nil, nil)
}

func (c *Client) GetDeploy(slug, deployID string) (*Deploy, error) {
	var deploy Deploy
	if err := c.do(http.MethodGet, "/sites/"+slug+"/deploys/"+deployID, nil, &deploy); err != nil {
		return nil, err
	}
	return &deploy, nil
}

func (c *Client) SearchDeploys(slug string) (*SearchResponse[Deploy], error) {
	var resp SearchResponse[Deploy]
	if err := c.do(http.MethodPost, "/sites/"+slug+"/deploys/search", struct{}{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- Domains ---

func (c *Client) AddDomain(slug string, req AddDomainRequest) (*AddDomainResponse, error) {
	var resp AddDomainResponse
	if err := c.do(http.MethodPost, "/sites/"+slug+"/domains", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListDomains(slug string) (*ListResponse[Domain], error) {
	var resp ListResponse[Domain]
	if err := c.do(http.MethodGet, "/sites/"+slug+"/domains", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteDomain(slug, domainID string) error {
	return c.do(http.MethodDelete, "/sites/"+slug+"/domains/"+domainID, nil, nil)
}
