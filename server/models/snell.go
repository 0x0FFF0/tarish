package models

import "time"

type SnellSource struct {
	ID            string     `json:"id"`
	Name          string     `json:"name,omitempty"`
	URL           string     `json:"url,omitempty"`
	Format        string     `json:"format"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	ValidatedAt   *time.Time `json:"validated_at,omitempty"`
	NodeCount     int        `json:"node_count"`
	ETag          string     `json:"-"`
	LastModified  string     `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
}

type SnellNodeView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SourceID string `json:"source_id,omitempty"`
	Source   string `json:"source"`
	Version  string `json:"version"`
	Enabled  bool   `json:"enabled"`
	Origin   string `json:"origin"`
	Builtin  bool   `json:"builtin,omitempty"`
	Role     string `json:"role,omitempty"`
	PSKSet   bool   `json:"psk_set"`
	Host     string `json:"-"`
	Port     int    `json:"-"`
	PSK      string `json:"-"`
	ObfsMode string `json:"-"`
	ObfsHost string `json:"-"`
	Manual   bool   `json:"manual"`
}

type SnellPoolView struct {
	IncludeSourceIDs    []string `json:"include_source_ids"`
	IncludeManualIDs    []string `json:"include_manual_ids"`
	ManagedProxyEnabled bool     `json:"managed_proxy_enabled"`
	PolicyVersion       int      `json:"policy_version"`
	Valid               bool     `json:"valid"`
	NodeCount           int      `json:"node_count"`
}

type MinerSnellView struct {
	MinerID         string   `json:"miner_id"`
	Hostname        string   `json:"hostname,omitempty"`
	Mode            string   `json:"mode"`
	Capable         bool     `json:"capable"`
	SupportedLabel  string   `json:"supported_label,omitempty"`
	Enabled         *bool    `json:"enabled,omitempty"`
	CustomNodeIDs   []string `json:"custom_node_ids,omitempty"`
	ExpectedVersion int      `json:"expected_version"`
	AppliedVersion  int      `json:"applied_version"`
	SelectedNode    string   `json:"selected_node,omitempty"`
	ApplyError      string   `json:"apply_error,omitempty"`
	ProxyEnabled    bool     `json:"proxy_enabled,omitempty"`
	Route           string   `json:"route,omitempty"`
}

type SnellCatalog struct {
	Sources  []*SnellSource    `json:"sources"`
	Nodes    []*SnellNodeView  `json:"nodes"`
	Pool     *SnellPoolView    `json:"pool"`
	Machines []*MinerSnellView `json:"machines"`
	Seed     *SnellNodeView    `json:"seed"`
}

type SnellAck struct {
	PolicyVersion int    `json:"policy_version"`
	OK            bool   `json:"ok"`
	ErrorCode     string `json:"error_code,omitempty"`
	SelectedNode  string `json:"selected_node,omitempty"`
}
