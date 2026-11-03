package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AccountClient calls the account service's internal endpoint to sync a user's
// kycLevel after approval. It authenticates with the shared internal token and is
// never reachable through the gateway.
type AccountClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAccountClient builds an AccountClient (nil-safe: a zero baseURL disables calls).
func NewAccountClient(baseURL, token string) *AccountClient {
	return &AccountClient{baseURL: strings.TrimRight(baseURL, "/"), token: token,
		http: &http.Client{Timeout: 5 * time.Second}}
}

// SetKYCLevel asks the account service to raise the user's kycLevel to at least
// level (account never downgrades). A zero baseURL is treated as "not configured".
func (c *AccountClient) SetKYCLevel(ctx context.Context, uid string, level int) error {
	if c.baseURL == "" {
		return fmt.Errorf("account internal url not configured")
	}
	buf, _ := json.Marshal(map[string]any{"uid": uid, "level": level})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/internal/account/kyc-level", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("account kyc-level: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
