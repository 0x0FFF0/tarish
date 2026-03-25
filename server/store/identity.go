package store

import (
	"strings"

	"tarish-server/models"
)

// ResolveMinerID returns the stable server-side identity for an agent.
// worker_id is preferred because api.id/miner_id can collide across hosts.
func ResolveMinerID(report *models.AgentReport) string {
	if report == nil {
		return ""
	}
	if id := strings.TrimSpace(report.WorkerID); id != "" {
		return id
	}
	if id := normalizedIPID(report.IP); id != "" {
		return id
	}
	if id := strings.TrimSpace(report.Hostname); id != "" {
		return id
	}
	return strings.TrimSpace(report.MinerID)
}

func normalizedIPID(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	return strings.ReplaceAll(ip, ".", "-")
}

func sameNonEmpty(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	return a != "" && a == b
}
