package models

import "time"

// BarkSettings is the on-disk representation of the alerter configuration.
type BarkSettings struct {
	Enabled         bool       `json:"enabled"`
	Token           string     `json:"-"` // never serialized; use BarkSettingsView for API
	CheckIntervalS  int        `json:"check_interval_s"`
	ThrottleMinutes int        `json:"throttle_minutes"`
	NotifyRecovery  bool       `json:"notify_recovery"`
	MuteUntil       *time.Time `json:"mute_until,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// BarkSettingsView is the redacted DTO returned to the browser.
type BarkSettingsView struct {
	Enabled         bool       `json:"enabled"`
	TokenSet        bool       `json:"token_set"`
	TokenLast4      string     `json:"token_last4,omitempty"`
	CheckIntervalS  int        `json:"check_interval_s"`
	ThrottleMinutes int        `json:"throttle_minutes"`
	NotifyRecovery  bool       `json:"notify_recovery"`
	MuteUntil       *time.Time `json:"mute_until,omitempty"`
	MuteForever     bool       `json:"mute_forever,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// BarkSettingsInput is the PUT body. Pointer fields let callers omit values
// to mean "unchanged"; explicit empty Token clears the stored device key.
type BarkSettingsInput struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	Token           *string `json:"token,omitempty"`
	CheckIntervalS  *int    `json:"check_interval_s,omitempty"`
	ThrottleMinutes *int    `json:"throttle_minutes,omitempty"`
	NotifyRecovery  *bool   `json:"notify_recovery,omitempty"`
}

// AlertState tracks per-miner alerter bookkeeping.
type AlertState struct {
	MinerID       string     `json:"miner_id"`
	LastState     string     `json:"last_state"` // "online" | "offline"
	OfflineSince  *time.Time `json:"offline_since,omitempty"`
	LastAlertedAt *time.Time `json:"last_alerted_at,omitempty"`
	AlertCount    int        `json:"alert_count"`
}

// AlertLogEntry is one row of the recent-alerts feed.
type AlertLogEntry struct {
	ID        int64     `json:"id"`
	MinerID   string    `json:"miner_id,omitempty"`
	Kind      string    `json:"kind"` // "offline" | "recovery" | "test" | "offline_suppressed"
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// MuteRequest is the body for POST /api/settings/bark/mute.
type MuteRequest struct {
	Minutes   int  `json:"minutes,omitempty"`
	Permanent bool `json:"permanent,omitempty"`
}
