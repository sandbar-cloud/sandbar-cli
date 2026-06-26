package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrDeviceExpired means the device authorization lapsed before the operator
// approved it in the console. The caller should ask the user to run login again.
var ErrDeviceExpired = errors.New("device authorization expired before approval")

// ErrDeviceDenied means the operator declined the device authorization in the
// console.
var ErrDeviceDenied = errors.New("device authorization was denied")

// DeviceAuthResponse is Microwave's reply to a device-authorization request. The
// CLI shows the user_code + verification_uri to the operator and polls with the
// (secret) device_code. The verification_uri is the static Sandbar console page;
// it carries no code.
type DeviceAuthResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceTokenResponse is Microwave's reply to a device-token poll. Status is one
// of pending, approved, expired (or denied); Token is set only when approved.
type DeviceTokenResponse struct {
	Status    string `json:"status"`
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// StartDeviceAuth begins a Microwave device-authorization flow for the given
// Sandbar CLI trust exchange. requestURL is the absolute Microwave endpoint the
// CLI learned from Sandbar's /auth/config; the CLI holds no Sandbar credential
// here.
func StartDeviceAuth(c *http.Client, requestURL, exchangeID string) (*DeviceAuthResponse, error) {
	var out DeviceAuthResponse
	if err := postDeviceJSON(c, requestURL, map[string]string{"trust_exchange_id": exchangeID}, &out); err != nil {
		return nil, fmt.Errorf("start device authorization: %w", err)
	}
	return &out, nil
}

// PollDeviceToken polls Microwave's device-token endpoint once with the secret
// device_code. tokenURL is the absolute Microwave endpoint from /auth/config.
func PollDeviceToken(c *http.Client, tokenURL, deviceCode string) (*DeviceTokenResponse, error) {
	var out DeviceTokenResponse
	if err := postDeviceJSON(c, tokenURL, map[string]string{"device_code": deviceCode}, &out); err != nil {
		return nil, fmt.Errorf("poll device token: %w", err)
	}
	return &out, nil
}

// postDeviceJSON POSTs body as JSON to an absolute URL and decodes the JSON reply
// into result. Unlike Client.do it sends no Authorization header — the device
// endpoints are public and the CLI has no Sandbar credential yet.
func postDeviceJSON(c *http.Client, url string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach device endpoint at %s. Check your connection", url)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("device endpoint returned HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}
	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}
