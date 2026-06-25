package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultMicrowaveAPIURL = "https://api.microwave.sh"

// microwaveOAuthError turns a non-2xx device-endpoint response into an error that
// surfaces the server's RFC 6749 error + error_description (e.g. "expired_token:
// device code expired") instead of a bare HTTP status. Falls back to the status
// and trimmed body when the envelope can't be parsed.
func microwaveOAuthError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var oe struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &oe) == nil && oe.Error != "" {
		if oe.Description != "" {
			return fmt.Errorf("%s: %s: %s", op, oe.Error, oe.Description)
		}
		return fmt.Errorf("%s: %s", op, oe.Error)
	}
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		return fmt.Errorf("%s: HTTP %d: %s", op, resp.StatusCode, trimmed)
	}
	return fmt.Errorf("%s: HTTP %d", op, resp.StatusCode)
}

type MicrowaveClient struct {
	baseURL    string
	httpClient *http.Client
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
		return nil, microwaveOAuthError("request microwave device code", resp)
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
		return nil, microwaveOAuthError("poll microwave device token", resp)
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

// The RFC 8693 CI token exchange now lives in the Microwave SDK
// (auth.RedeemTokenExchange), which returns a typed *auth.OAuthError carrying the
// server's error + error_description instead of a bare HTTP status. See
// cmd/login.go.
