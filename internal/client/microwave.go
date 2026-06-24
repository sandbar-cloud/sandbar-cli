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

type MicrowaveRedeemResult struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Scopes    []string  `json:"scopes"`
}

type MicrowaveTokenExchangeResult struct {
	Token     string
	ExpiresIn int64
	Scope     string
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

func (c *MicrowaveClient) RedeemTrustFederation(federationID, token string) (*MicrowaveRedeemResult, error) {
	if strings.TrimSpace(federationID) == "" {
		return nil, fmt.Errorf("microwave federation id is required")
	}
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return nil, fmt.Errorf("marshal microwave redeem request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/trust-federations/"+federationID+"/redeem", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create microwave redeem request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("redeem microwave federation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("redeem microwave federation: HTTP %d", resp.StatusCode)
	}

	var out MicrowaveRedeemResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode microwave redeem response: %w", err)
	}
	if out.Token == "" {
		return nil, fmt.Errorf("microwave redeem response did not include token")
	}
	return &out, nil
}

func (c *MicrowaveClient) RedeemTrustExchange(exchangeID, token string) (*MicrowaveTokenExchangeResult, error) {
	if strings.TrimSpace(exchangeID) == "" {
		return nil, fmt.Errorf("microwave trust exchange id is required")
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:jwt")
	form.Set("requested_token_type", "urn:ietf:params:oauth:token-type:jwt")
	form.Set("subject_token", token)
	form.Set("resource", c.baseURL+"/trust-exchanges/"+exchangeID)

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create microwave token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("redeem microwave trust exchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("redeem microwave trust exchange: HTTP %d", resp.StatusCode)
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
