package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"tarish/cpu"
	"tarish/xmrig"
)

type HashrateReport struct {
	Current float64 `json:"current"`
	Average float64 `json:"average"`
	Max     float64 `json:"max"`
}

type StatusReport struct {
	MinerID       string                 `json:"miner_id"`
	WorkerID      string                 `json:"worker_id"`
	Hostname      string                 `json:"hostname"`
	IP            string                 `json:"ip"`
	CPUModel      string                 `json:"cpu_model"`
	CPUFamily     string                 `json:"cpu_family"`
	Cores         int                    `json:"cores"`
	OS            string                 `json:"os"`
	Arch          string                 `json:"arch"`
	XmrigVersion  string                 `json:"xmrig_version"`
	UptimeSeconds int64                  `json:"uptime_seconds"`
	Hashrate      *HashrateReport        `json:"hashrate,omitempty"`
	Config        map[string]interface{} `json:"config,omitempty"`
	TarishVersion string                 `json:"tarish_version"`
}

func buildReport(cpuInfo *cpu.Info, version string) *StatusReport {
	hostname, _ := os.Hostname()

	report := &StatusReport{
		Hostname:      hostname,
		CPUModel:      cpuInfo.RawModel,
		CPUFamily:     cpuInfo.Family,
		Cores:         cpuInfo.Cores,
		OS:            cpuInfo.OS,
		Arch:          cpuInfo.Arch,
		TarishVersion: version,
	}

	// Get miner_id and worker_id from the runtime config file (these don't change)
	runtimePath := xmrig.GetRuntimeConfigPath()
	if data, err := os.ReadFile(runtimePath); err == nil {
		var raw map[string]interface{}
		if json.Unmarshal(data, &raw) == nil {
			if api, ok := raw["api"].(map[string]interface{}); ok {
				if id, ok := api["id"].(string); ok {
					report.MinerID = id
				}
				if wid, ok := api["worker-id"].(string); ok {
					report.WorkerID = wid
				}
			}
		}
	}

	// Read LIVE config from xmrig API (reflects applied overrides)
	port, accessToken := xmrig.GetHTTPConfigFromRuntime()
	liveConfig := fetchLiveConfig(port, accessToken)
	if liveConfig != nil {
		report.Config = liveConfig
	}

	report.IP = detectLANIP()
	if report.IP == "" && report.WorkerID != "" {
		report.IP = workerIDToIP(report.WorkerID)
	}

	apiStatus := fetchLocalXmrigAPI()
	if apiStatus != nil {
		report.XmrigVersion = apiStatus.Version
		report.UptimeSeconds = apiStatus.Uptime
		if len(apiStatus.Hashrate.Total) >= 3 {
			report.Hashrate = &HashrateReport{
				Current: apiStatus.Hashrate.Total[0],
				Average: apiStatus.Hashrate.Total[1],
				Max:     apiStatus.Hashrate.Total[2],
			}
		}
	}

	return report
}

func fetchLiveConfig(port int, accessToken string) map[string]interface{} {
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/1/config", port)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var cfg map[string]interface{}
	if json.Unmarshal(body, &cfg) != nil {
		return nil
	}
	return cfg
}

// detectLANIP returns a real LAN IP address, skipping VPN/tunnel interfaces.
// Prefers RFC1918 addresses (192.168.x, 10.x, 172.16-31.x).
func detectLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Skip common VPN/tunnel interface names
		name := iface.Name
		if isVPNInterface(name) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}
			if isPrivateIP(ip) {
				return ip.String()
			}
			if fallback == "" {
				fallback = ip.String()
			}
		}
	}
	return fallback
}

func isVPNInterface(name string) bool {
	prefixes := []string{"tun", "tap", "utun", "wg", "tailscale", "nordlynx", "proton", "mullvad"}
	for _, p := range prefixes {
		if len(name) >= len(p) && name[:len(p)] == p {
			return true
		}
	}
	return false
}

func isPrivateIP(ip net.IP) bool {
	private := []net.IPNet{
		{IP: net.IP{10, 0, 0, 0}, Mask: net.CIDRMask(8, 32)},
		{IP: net.IP{172, 16, 0, 0}, Mask: net.CIDRMask(12, 32)},
		{IP: net.IP{192, 168, 0, 0}, Mask: net.CIDRMask(16, 32)},
	}
	for _, n := range private {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func workerIDToIP(workerID string) string {
	ip := ""
	for _, c := range workerID {
		if c == '-' {
			ip += "."
		} else {
			ip += string(c)
		}
	}
	return ip
}

func fetchLocalXmrigAPI() *xmrig.APIResponse {
	port, accessToken := xmrig.GetHTTPConfigFromRuntime()

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/1/summary", port)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var apiResp xmrig.APIResponse
	if json.Unmarshal(body, &apiResp) != nil {
		return nil
	}
	return &apiResp
}
