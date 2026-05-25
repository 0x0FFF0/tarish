package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"tarish-server/models"
)

// ForeverMute is the sentinel timestamp stored in bark_settings.mute_until
// when the user picks "mute forever". Anything past this in the UI is
// rendered as "muted indefinitely".
var ForeverMute = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)

const defaultBarkCheckIntervalS = 60
const defaultBarkThrottleMinutes = 60

func (s *Store) migrateBark() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS bark_settings (
			id                INTEGER PRIMARY KEY CHECK (id = 1),
			enabled           INTEGER  NOT NULL DEFAULT 0,
			token             TEXT     NOT NULL DEFAULT '',
			check_interval_s  INTEGER  NOT NULL DEFAULT 60,
			throttle_minutes  INTEGER  NOT NULL DEFAULT 60,
			notify_recovery   INTEGER  NOT NULL DEFAULT 1,
			mute_until        DATETIME,
			updated_at        DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS alert_state (
			miner_id         TEXT PRIMARY KEY,
			last_state       TEXT NOT NULL,
			offline_since    DATETIME,
			last_alerted_at  DATETIME,
			alert_count      INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS alert_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			miner_id    TEXT,
			kind        TEXT NOT NULL,
			title       TEXT NOT NULL,
			body        TEXT NOT NULL,
			ok          INTEGER NOT NULL,
			error       TEXT NOT NULL DEFAULT '',
			created_at  DATETIME NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_alert_log_created ON alert_log(created_at DESC);
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO bark_settings (id, updated_at) VALUES (1, ?)
	`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}

	return tx.Commit()
}

// GetBarkSettings returns the singleton row, seeding defaults on first read.
func (s *Store) GetBarkSettings() (*models.BarkSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	settings := &models.BarkSettings{}
	var muteUntil sql.NullString
	var updatedAt string
	var enabled, notifyRecovery int

	err := s.db.QueryRow(`
		SELECT enabled, token, check_interval_s, throttle_minutes,
		       notify_recovery, mute_until, updated_at
		FROM bark_settings WHERE id = 1
	`).Scan(&enabled, &settings.Token, &settings.CheckIntervalS,
		&settings.ThrottleMinutes, &notifyRecovery, &muteUntil, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &models.BarkSettings{
				CheckIntervalS:  defaultBarkCheckIntervalS,
				ThrottleMinutes: defaultBarkThrottleMinutes,
				NotifyRecovery:  true,
			}, nil
		}
		return nil, err
	}

	settings.Enabled = enabled != 0
	settings.NotifyRecovery = notifyRecovery != 0
	settings.UpdatedAt = parseTime(updatedAt)
	if muteUntil.Valid && muteUntil.String != "" {
		t := parseTime(muteUntil.String)
		settings.MuteUntil = &t
	}
	if settings.CheckIntervalS <= 0 {
		settings.CheckIntervalS = defaultBarkCheckIntervalS
	}
	if settings.ThrottleMinutes <= 0 {
		settings.ThrottleMinutes = defaultBarkThrottleMinutes
	}
	return settings, nil
}

// UpdateBarkSettings applies a partial input, returning the new state.
func (s *Store) UpdateBarkSettings(input models.BarkSettingsInput) (*models.BarkSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.getBarkSettingsLocked()
	if err != nil {
		return nil, err
	}

	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	if input.Token != nil {
		current.Token = strings.TrimSpace(*input.Token)
	}
	if input.CheckIntervalS != nil {
		v := *input.CheckIntervalS
		if v < 10 {
			v = 10
		}
		if v > 3600 {
			v = 3600
		}
		current.CheckIntervalS = v
	}
	if input.ThrottleMinutes != nil {
		v := *input.ThrottleMinutes
		if v < 1 {
			v = 1
		}
		if v > 1440 {
			v = 1440
		}
		current.ThrottleMinutes = v
	}
	if input.NotifyRecovery != nil {
		current.NotifyRecovery = *input.NotifyRecovery
	}
	current.UpdatedAt = time.Now().UTC()

	if err := s.writeBarkSettingsLocked(current); err != nil {
		return nil, err
	}
	return current, nil
}

// SetMuteUntil writes the mute timestamp (nil clears mute).
func (s *Store) SetMuteUntil(until *time.Time) (*models.BarkSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.getBarkSettingsLocked()
	if err != nil {
		return nil, err
	}
	current.MuteUntil = until
	current.UpdatedAt = time.Now().UTC()

	if err := s.writeBarkSettingsLocked(current); err != nil {
		return nil, err
	}
	return current, nil
}

// GetAlertState returns the per-miner alerter row, or nil if none exists.
func (s *Store) GetAlertState(minerID string) (*models.AlertState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT miner_id, last_state, offline_since, last_alerted_at, alert_count
		FROM alert_state WHERE miner_id = ?
	`, minerID)

	state := &models.AlertState{}
	var offlineSince, lastAlertedAt sql.NullString
	if err := row.Scan(&state.MinerID, &state.LastState, &offlineSince,
		&lastAlertedAt, &state.AlertCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if offlineSince.Valid {
		t := parseTime(offlineSince.String)
		state.OfflineSince = &t
	}
	if lastAlertedAt.Valid {
		t := parseTime(lastAlertedAt.String)
		state.LastAlertedAt = &t
	}
	return state, nil
}

// UpsertAlertState writes the per-miner row.
func (s *Store) UpsertAlertState(state *models.AlertState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var offlineSince, lastAlertedAt interface{}
	if state.OfflineSince != nil {
		offlineSince = state.OfflineSince.UTC().Format(time.RFC3339)
	}
	if state.LastAlertedAt != nil {
		lastAlertedAt = state.LastAlertedAt.UTC().Format(time.RFC3339)
	}

	_, err := s.db.Exec(`
		INSERT INTO alert_state (miner_id, last_state, offline_since, last_alerted_at, alert_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(miner_id) DO UPDATE SET
			last_state=excluded.last_state,
			offline_since=excluded.offline_since,
			last_alerted_at=excluded.last_alerted_at,
			alert_count=excluded.alert_count
	`, state.MinerID, state.LastState, offlineSince, lastAlertedAt, state.AlertCount)
	return err
}

// RecordAlertLog appends one outgoing-attempt row.
func (s *Store) RecordAlertLog(entry *models.AlertLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ok := 0
	if entry.OK {
		ok = 1
	}
	created := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`
		INSERT INTO alert_log (miner_id, kind, title, body, ok, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, entry.MinerID, entry.Kind, entry.Title, entry.Body, ok, entry.Error, created)
	if err != nil {
		return err
	}
	if id, err := res.LastInsertId(); err == nil {
		entry.ID = id
	}
	entry.CreatedAt = parseTime(created)
	return nil
}

// GetRecentAlerts returns the most recent N alert_log rows.
func (s *Store) GetRecentAlerts(limit int) ([]*models.AlertLogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, miner_id, kind, title, body, ok, error, created_at
		FROM alert_log ORDER BY id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*models.AlertLogEntry
	for rows.Next() {
		entry := &models.AlertLogEntry{}
		var minerID sql.NullString
		var ok int
		var createdAt string
		if err := rows.Scan(&entry.ID, &minerID, &entry.Kind, &entry.Title,
			&entry.Body, &ok, &entry.Error, &createdAt); err != nil {
			return nil, err
		}
		if minerID.Valid {
			entry.MinerID = minerID.String
		}
		entry.OK = ok != 0
		entry.CreatedAt = parseTime(createdAt)
		out = append(out, entry)
	}
	return out, rows.Err()
}

// internal helpers; both assume the caller holds s.mu in write mode.

func (s *Store) getBarkSettingsLocked() (*models.BarkSettings, error) {
	settings := &models.BarkSettings{}
	var muteUntil sql.NullString
	var updatedAt string
	var enabled, notifyRecovery int

	err := s.db.QueryRow(`
		SELECT enabled, token, check_interval_s, throttle_minutes,
		       notify_recovery, mute_until, updated_at
		FROM bark_settings WHERE id = 1
	`).Scan(&enabled, &settings.Token, &settings.CheckIntervalS,
		&settings.ThrottleMinutes, &notifyRecovery, &muteUntil, &updatedAt)
	if err != nil {
		return nil, err
	}
	settings.Enabled = enabled != 0
	settings.NotifyRecovery = notifyRecovery != 0
	settings.UpdatedAt = parseTime(updatedAt)
	if muteUntil.Valid && muteUntil.String != "" {
		t := parseTime(muteUntil.String)
		settings.MuteUntil = &t
	}
	if settings.CheckIntervalS <= 0 {
		settings.CheckIntervalS = defaultBarkCheckIntervalS
	}
	if settings.ThrottleMinutes <= 0 {
		settings.ThrottleMinutes = defaultBarkThrottleMinutes
	}
	return settings, nil
}

func (s *Store) writeBarkSettingsLocked(settings *models.BarkSettings) error {
	enabled := 0
	if settings.Enabled {
		enabled = 1
	}
	notifyRecovery := 0
	if settings.NotifyRecovery {
		notifyRecovery = 1
	}
	var muteUntil interface{}
	if settings.MuteUntil != nil {
		muteUntil = settings.MuteUntil.UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`
		UPDATE bark_settings
		SET enabled = ?, token = ?, check_interval_s = ?, throttle_minutes = ?,
		    notify_recovery = ?, mute_until = ?, updated_at = ?
		WHERE id = 1
	`, enabled, settings.Token, settings.CheckIntervalS, settings.ThrottleMinutes,
		notifyRecovery, muteUntil, settings.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}
