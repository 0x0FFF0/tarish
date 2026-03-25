package agent

import "strings"

func preferredAgentID(minerID, workerID, ip, hostname string) string {
	if id := strings.TrimSpace(workerID); id != "" {
		return id
	}
	if id := normalizedAgentIPID(ip); id != "" {
		return id
	}
	if id := strings.TrimSpace(hostname); id != "" {
		return id
	}
	return strings.TrimSpace(minerID)
}

func normalizedAgentIPID(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return strings.ReplaceAll(ip, ".", "-")
}
