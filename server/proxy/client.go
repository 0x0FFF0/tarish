package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL     string
	accessToken string
	httpClient  *http.Client
}

type ProxySummary struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Uptime   int64  `json:"uptime"`
	Workers  WorkerCount `json:"workers"`
	Hashrate struct {
		Total []float64 `json:"total"`
	} `json:"hashrate"`
}

type WorkerCount struct {
	Now int `json:"now"`
	Max int `json:"max"`
}

type ProxyWorker struct {
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	Hashrate []float64 `json:"hashrate"`
	Accepted int64     `json:"accepted"`
	Rejected int64     `json:"rejected"`
	Hashes   int64     `json:"hashes"`
	LastSeen int64     `json:"last_seen"`
}

type WorkersResponse struct {
	Workers []ProxyWorker `json:"workers"`
}

func NewClient(baseURL, accessToken string) *Client {
	return &Client{
		baseURL:     baseURL,
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) GetSummary() (*ProxySummary, error) {
	body, err := c.get("/1/summary")
	if err != nil {
		return nil, err
	}

	var summary ProxySummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("parse summary: %w", err)
	}
	return &summary, nil
}

func (c *Client) GetWorkers() ([]ProxyWorker, error) {
	body, err := c.get("/1/workers")
	if err != nil {
		return nil, err
	}

	// xmrig-proxy returns workers as a JSON object with a "workers" array
	var resp WorkersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// Try as raw array
		var workers []ProxyWorker
		if err2 := json.Unmarshal(body, &workers); err2 != nil {
			return nil, fmt.Errorf("parse workers: %w", err)
		}
		return workers, nil
	}
	return resp.Workers, nil
}

func (c *Client) get(path string) ([]byte, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("proxy URL not configured")
	}

	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return body, nil
}
