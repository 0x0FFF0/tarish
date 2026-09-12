package agent

import "strings"

func preferredAgentID(minerID, workerID, ip, hostname string) string {
	if usableAgentID(workerID) {
		return strings.TrimSpace(workerID)
	}
	if id := normalizedAgentIPID(ip); id != "" {
		return id
	}
	if usableAgentID(hostname) {
		return strings.TrimSpace(hostname)
	}
	if usableAgentID(minerID) {
		return strings.TrimSpace(minerID)
	}
	return ""
}

func usableAgentID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	switch strings.ToLower(s) {
	case "unknown", "none", "null", "undefined", "n/a", "na":
		return false
	}
	return true
}

func normalizedAgentIPID(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return strings.ReplaceAll(ip, ".", "-")
}
