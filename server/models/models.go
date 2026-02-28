package models

import "time"

type HashrateData struct {
	Current float64 `json:"current"`
	Average float64 `json:"average"`
	Max     float64 `json:"max"`
}

type Miner struct {
	ID            string                 `json:"id"`
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
	TarishVersion string                 `json:"tarish_version"`
	UptimeSeconds int64                  `json:"uptime_seconds"`
	Hashrate      *HashrateData          `json:"hashrate,omitempty"`
	Config        map[string]interface{} `json:"config,omitempty"`
	LastSeen      time.Time              `json:"last_seen"`
	Status        string                 `json:"status"` // online, stale, offline
}

type ConfigOverride struct {
	MinerID   string                 `json:"miner_id"`
	Override  map[string]interface{} `json:"override"`
	CreatedAt time.Time              `json:"created_at"`
	AppliedAt *time.Time             `json:"applied_at,omitempty"`
}

type HashrateHistory struct {
	MinerID   string    `json:"miner_id"`
	Timestamp time.Time `json:"timestamp"`
	Current   float64   `json:"current"`
	Average   float64   `json:"average"`
	Max       float64   `json:"max"`
}

type OverviewResponse struct {
	TotalHashrate   float64  `json:"total_hashrate"`
	AverageHashrate float64  `json:"average_hashrate"`
	ActiveMiners    int      `json:"active_miners"`
	TotalMiners     int      `json:"total_miners"`
	TopMiners       []*Miner `json:"top_miners"`
}

type AgentReport struct {
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
	Hashrate      *HashrateData          `json:"hashrate,omitempty"`
	Config        map[string]interface{} `json:"config,omitempty"`
	TarishVersion string                 `json:"tarish_version"`
}

type ReportResponse struct {
	OK             bool                   `json:"ok"`
	ConfigOverride map[string]interface{} `json:"config_override,omitempty"`
}
