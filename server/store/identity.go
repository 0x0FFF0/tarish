package store

import (
	"strconv"
	"strings"

	"tarish-server/models"
	"tarish-server/snell"
)

// ResolveMinerID returns the stable server-side identity for an agent.
// worker_id is preferred because api.id/miner_id can collide across hosts.
// Placeholder values like "unknown" are skipped so LAN IP can be used.
func ResolveMinerID(report *models.AgentReport) string {
	if report == nil {
		return ""
	}
	if usableIdentity(report.WorkerID) {
		return strings.TrimSpace(report.WorkerID)
	}
	if id := normalizedIPID(report.IP); id != "" {
		return id
	}
	if usableIdentity(report.Hostname) {
		return strings.TrimSpace(report.Hostname)
	}
	if usableIdentity(report.MinerID) {
		return strings.TrimSpace(report.MinerID)
	}
	return ""
}

func usableIdentity(s string) bool {
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

func placeholderIdentity(s string) bool {
	return !usableIdentity(s)
}

func reportSnellCapable(report *models.AgentReport) bool {
	if report == nil {
		return false
	}
	for _, c := range report.Capabilities {
		if c == snell.Capability {
			return true
		}
	}
	if report.Proxy != nil {
		return true
	}
	return versionAtLeast(report.TarishVersion, 1, 1, 0)
}

func versionAtLeast(raw string, major, minor, patch int) bool {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return false
	}
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	nums := []int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return false
		}
		nums[i] = n
	}
	if nums[0] != major {
		return nums[0] > major
	}
	if nums[1] != minor {
		return nums[1] > minor
	}
	return nums[2] >= patch
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
