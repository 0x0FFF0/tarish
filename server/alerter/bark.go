package alerter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BarkSender is the interface the alerter calls to deliver a notification.
// It exists so tests can substitute a fake.
type BarkSender interface {
	Send(ctx context.Context, token, title, body, group, sound string) error
}

// BarkClient talks to the Bark push service.
type BarkClient struct {
	baseURL    string
	httpClient *http.Client
}

const defaultBarkBaseURL = "https://bark.ws"

// NewBarkClient returns a Bark sender with a 5-second send timeout.
func NewBarkClient() *BarkClient {
	return &BarkClient{
		baseURL:    defaultBarkBaseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Send fires GET https://bark.ws/<token>?title=...&body=...&group=...&sound=...
// The token is treated as the device key and is never logged.
func (c *BarkClient) Send(ctx context.Context, token, title, body, group, sound string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("bark token not configured")
	}

	endpoint := fmt.Sprintf("%s/%s", strings.TrimRight(c.baseURL, "/"), url.PathEscape(token))
	q := url.Values{}
	q.Set("title", title)
	q.Set("body", body)
	if group != "" {
		q.Set("group", group)
	}
	if sound != "" {
		q.Set("sound", sound)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("create bark request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bark request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Don't echo the URL (contains the token) — only host + status.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		snippet := strings.TrimSpace(string(body))
		if snippet != "" {
			return fmt.Errorf("bark returned %d: %s", resp.StatusCode, snippet)
		}
		return fmt.Errorf("bark returned %d", resp.StatusCode)
	}
	return nil
}
