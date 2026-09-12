package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"tarish/config"
	"tarish/proxy"
	"tarish/snellspec"
)

func pollSnellLoop(stop <-chan struct{}) {
	agentID := readAgentID()
	if agentID == "" {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !config.IsServerEnabled() {
				return
			}
			serverURL := config.GetServerURL()
			if serverURL == "" {
				return
			}
			checkPendingSnell(client, serverURL, agentID)
		}
	}
}

func checkPendingSnell(client *http.Client, serverURL, agentID string) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/miners/%s/snell/pending", serverURL, agentID), nil)
	if err != nil {
		return
	}
	applyServerAuthHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	if strings.Contains(string(body), `"url"`) && strings.Contains(strings.ToLower(string(body)), "subscribe") {
		fmt.Println("[agent] rejected snell snapshot: unexpected url field")
		return
	}
	applied, err := proxy.ApplyPending(body, proxy.ApplyHooks{})
	code := proxy.ErrorCode(err)
	ack := snellspec.ManagedDocument{}
	_ = json.Unmarshal(body, &ack)
	ackSnell(serverURL, agentID, ack.PolicyVersion, err == nil, code)
	if err != nil {
		fmt.Printf("[agent] snell apply: %s\n", code)
		return
	}
	if applied {
		fmt.Println("[agent] applied snell snapshot")
	}
}

func ackSnell(serverURL, agentID string, version int, ok bool, errCode string) {
	payload, _ := json.Marshal(map[string]any{
		"policy_version": version,
		"ok":             ok,
		"error_code":     errCode,
	})
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/miners/%s/snell/ack", serverURL, agentID), bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	applyServerAuthHeaders(req)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}
