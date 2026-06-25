package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultMicrowaveAPIURL = "https://api.microwave.sh"

type MicrowaveClient struct {
	baseURL    string
	httpClient *http.Client
}

type MicrowaveTokenExchangeResult struct {
	Token     string
	ExpiresIn int64
	Scope     string
}

type MicrowaveDeviceCodeResponse struct {
	DeviceCode   string `json:"device_code"`
	UserCode     string `json:"user_code"`
	AuthorizeURL string `json:"authorize_url"`
	ExpiresIn    int    `json:"expires_in"`
	Interval     int    `json:"interval"`
}

type MicrowaveDeviceTokenResponse struct {
	Token     string `json:"token,omitempty"`
	Status    string `json:"status"`
	ExpiresIn int    `json:"expires_in,omitempty"`
}

func NewMicrowaveClient(baseURL string) *MicrowaveClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultMicrowaveAPIURL
	}
	return &MicrowaveClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *MicrowaveClient) RequestDeviceCode(trustExchangeID string) (*MicrowaveDeviceCodeResponse, error) {
	if strings.TrimSpace(trustExchangeID) == "" {
		return nil, fmt.Errorf("microwave trust exchange id is required")
	}
	body, err := json.Marshal(map[string]string{"trust_exchange_id": trustExchangeID})
	if err != nil {
		return nil, fmt.Errorf("marshal microwave device request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/auth/device", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create microwave device request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request microwave device code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("request microwave device code: HTTP %d", resp.StatusCode)
	}

	var out MicrowaveDeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode microwave device response: %w", err)
	}
	if out.DeviceCode == "" || out.AuthorizeURL == "" {
		return nil, fmt.Errorf("microwave device response did not include device_code and authorize_url")
	}
	return &out, nil
}

func (c *MicrowaveClient) PollDeviceToken(deviceCode string) (*MicrowaveDeviceTokenResponse, error) {
	if strings.TrimSpace(deviceCode) == "" {
		return nil, fmt.Errorf("microwave device code is required")
	}
	body, err := json.Marshal(map[string]string{"device_code": deviceCode})
	if err != nil {
		return nil, fmt.Errorf("marshal microwave device poll request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/auth/device/token", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create microwave device poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll microwave device token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("poll microwave device token: HTTP %d", resp.StatusCode)
	}

	var out MicrowaveDeviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode microwave device poll response: %w", err)
	}
	if out.Status == "" {
		return nil, fmt.Errorf("microwave device poll response did not include status")
	}
	return &out, nil
}

// RedeemTokenExchange runs the RFC 8693 token-exchange at an absolute Microwave
// token endpoint, swapping an OIDC subject token for a Microwave-issued JWT. The
// tokenEndpoint and resource indicator come from Sandbar's /auth/config discovery
// (resource selects the CI trust federation), so the CLI is not pinned to a
// specific federation — recreating it is a server-config change, not a release.
func RedeemTokenExchange(tokenEndpoint, resource, subjectToken string) (*MicrowaveTokenExchangeResult, error) {
	if strings.TrimSpace(tokenEndpoint) == "" || strings.TrimSpace(resource) == "" {
		return nil, fmt.Errorf("microwave token endpoint and resource are required")
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:jwt")
	form.Set("requested_token_type", "urn:ietf:params:oauth:token-type:jwt")
	form.Set("subject_token", subjectToken)
	form.Set("resource", resource)

	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create microwave token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("redeem microwave token exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("redeem microwave token exchange: HTTP %d", resp.StatusCode)
	}

	var wire struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in,omitempty"`
		Scope       string `json:"scope,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode microwave token exchange response: %w", err)
	}
	if wire.AccessToken == "" {
		return nil, fmt.Errorf("microwave token exchange response did not include access_token")
	}
	return &MicrowaveTokenExchangeResult{Token: wire.AccessToken, ExpiresIn: wire.ExpiresIn, Scope: wire.Scope}, nil
}
