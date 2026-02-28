package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"tarish-server/models"
)

type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

func New(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS miners (
			id TEXT PRIMARY KEY,
			miner_id TEXT NOT NULL,
			worker_id TEXT NOT NULL,
			hostname TEXT DEFAULT '',
			ip TEXT DEFAULT '',
			cpu_model TEXT DEFAULT '',
			cpu_family TEXT DEFAULT '',
			cores INTEGER DEFAULT 0,
			os TEXT DEFAULT '',
			arch TEXT DEFAULT '',
			xmrig_version TEXT DEFAULT '',
			tarish_version TEXT DEFAULT '',
			uptime_seconds INTEGER DEFAULT 0,
			hashrate_current REAL DEFAULT 0,
			hashrate_average REAL DEFAULT 0,
			hashrate_max REAL DEFAULT 0,
			config_json TEXT DEFAULT '{}',
			last_seen DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS config_overrides (
			miner_id TEXT PRIMARY KEY,
			override_json TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			applied_at DATETIME
		);

		CREATE TABLE IF NOT EXISTS hashrate_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			miner_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			current REAL DEFAULT 0,
			average REAL DEFAULT 0,
			max REAL DEFAULT 0
		);

		CREATE INDEX IF NOT EXISTS idx_hashrate_history_miner_ts
			ON hashrate_history(miner_id, timestamp);
	`)
	return err
}

func (s *Store) UpsertMiner(report *models.AgentReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := report.MinerID
	if id == "" {
		id = report.WorkerID
	}

	configJSON := "{}"
	if report.Config != nil {
		if data, err := json.Marshal(report.Config); err == nil {
			configJSON = string(data)
		}
	}

	var hCurrent, hAverage, hMax float64
	if report.Hashrate != nil {
		hCurrent = report.Hashrate.Current
		hAverage = report.Hashrate.Average
		hMax = report.Hashrate.Max
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.Exec(`
		INSERT INTO miners (id, miner_id, worker_id, hostname, ip, cpu_model, cpu_family,
			cores, os, arch, xmrig_version, tarish_version, uptime_seconds,
			hashrate_current, hashrate_average, hashrate_max, config_json, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			miner_id=excluded.miner_id,
			worker_id=excluded.worker_id,
			hostname=excluded.hostname,
			ip=excluded.ip,
			cpu_model=excluded.cpu_model,
			cpu_family=excluded.cpu_family,
			cores=excluded.cores,
			os=excluded.os,
			arch=excluded.arch,
			xmrig_version=excluded.xmrig_version,
			tarish_version=excluded.tarish_version,
			uptime_seconds=excluded.uptime_seconds,
			hashrate_current=excluded.hashrate_current,
			hashrate_average=excluded.hashrate_average,
			hashrate_max=excluded.hashrate_max,
			config_json=excluded.config_json,
			last_seen=excluded.last_seen
	`, id, report.MinerID, report.WorkerID, report.Hostname, report.IP,
		report.CPUModel, report.CPUFamily, report.Cores, report.OS, report.Arch,
		report.XmrigVersion, report.TarishVersion, report.UptimeSeconds,
		hCurrent, hAverage, hMax, configJSON, now)

	if err != nil {
		return err
	}

	// Record hashrate history (sample every report)
	if report.Hashrate != nil {
		_, err = s.db.Exec(`
			INSERT INTO hashrate_history (miner_id, timestamp, current, average, max)
			VALUES (?, ?, ?, ?, ?)
		`, id, now, hCurrent, hAverage, hMax)
	}

	return err
}

func (s *Store) GetMiners() ([]*models.Miner, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, miner_id, worker_id, hostname, ip, cpu_model, cpu_family,
			cores, os, arch, xmrig_version, tarish_version, uptime_seconds,
			hashrate_current, hashrate_average, hashrate_max, config_json, last_seen
		FROM miners ORDER BY hashrate_current DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var miners []*models.Miner
	for rows.Next() {
		m, err := scanMiner(rows)
		if err != nil {
			return nil, err
		}
		miners = append(miners, m)
	}
	return miners, rows.Err()
}

func (s *Store) GetMiner(id string) (*models.Miner, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, miner_id, worker_id, hostname, ip, cpu_model, cpu_family,
			cores, os, arch, xmrig_version, tarish_version, uptime_seconds,
			hashrate_current, hashrate_average, hashrate_max, config_json, last_seen
		FROM miners WHERE id = ?
	`, id)

	m := &models.Miner{}
	var configJSON string
	var lastSeen string
	var hCurrent, hAverage, hMax float64

	err := row.Scan(&m.ID, &m.MinerID, &m.WorkerID, &m.Hostname, &m.IP,
		&m.CPUModel, &m.CPUFamily, &m.Cores, &m.OS, &m.Arch,
		&m.XmrigVersion, &m.TarishVersion, &m.UptimeSeconds,
		&hCurrent, &hAverage, &hMax, &configJSON, &lastSeen)
	if err != nil {
		return nil, err
	}

	m.Hashrate = &models.HashrateData{Current: hCurrent, Average: hAverage, Max: hMax}
	m.LastSeen = parseTime(lastSeen)
	m.Status = minerStatus(m.LastSeen)

	if configJSON != "" && configJSON != "{}" {
		json.Unmarshal([]byte(configJSON), &m.Config)
	}

	return m, nil
}

func (s *Store) SetConfigOverride(minerID string, override map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(override)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO config_overrides (miner_id, override_json, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(miner_id) DO UPDATE SET
			override_json=excluded.override_json,
			created_at=excluded.created_at,
			applied_at=NULL
	`, minerID, string(data), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetConfigOverride(minerID string) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var overrideJSON string
	var appliedAt sql.NullString

	err := s.db.QueryRow(`
		SELECT override_json, applied_at FROM config_overrides WHERE miner_id = ?
	`, minerID).Scan(&overrideJSON, &appliedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Only return override if not yet applied
	if appliedAt.Valid {
		return nil, nil
	}

	var override map[string]interface{}
	if err := json.Unmarshal([]byte(overrideJSON), &override); err != nil {
		return nil, err
	}
	return override, nil
}

func (s *Store) GetLastOverride(minerID string) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var overrideJSON string
	err := s.db.QueryRow(`
		SELECT override_json FROM config_overrides WHERE miner_id = ?
	`, minerID).Scan(&overrideJSON)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var override map[string]interface{}
	if err := json.Unmarshal([]byte(overrideJSON), &override); err != nil {
		return nil, err
	}
	return override, nil
}

func (s *Store) MarkConfigApplied(minerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE config_overrides SET applied_at = ? WHERE miner_id = ?
	`, time.Now().UTC().Format(time.RFC3339), minerID)
	return err
}

func (s *Store) DeleteConfigOverride(minerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM config_overrides WHERE miner_id = ?`, minerID)
	return err
}

func (s *Store) GetHashrateHistory(minerID string, since time.Time) ([]*models.HashrateHistory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT miner_id, timestamp, current, average, max
		FROM hashrate_history WHERE timestamp > ?
	`
	args := []interface{}{since.Format(time.RFC3339)}

	if minerID != "" {
		query += " AND miner_id = ?"
		args = append(args, minerID)
	}
	query += " ORDER BY timestamp ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*models.HashrateHistory
	for rows.Next() {
		h := &models.HashrateHistory{}
		var ts string
		if err := rows.Scan(&h.MinerID, &ts, &h.Current, &h.Average, &h.Max); err != nil {
			return nil, err
		}
		h.Timestamp = parseTime(ts)
		history = append(history, h)
	}
	return history, rows.Err()
}

func (s *Store) GetOverview() (*models.OverviewResponse, error) {
	miners, err := s.GetMiners()
	if err != nil {
		return nil, err
	}

	overview := &models.OverviewResponse{
		TotalMiners: len(miners),
	}

	for _, m := range miners {
		if m.Status == "online" {
			overview.ActiveMiners++
			if m.Hashrate != nil {
				overview.TotalHashrate += m.Hashrate.Current
				overview.AverageHashrate += m.Hashrate.Average
			}
		}
	}

	// Top 5 miners by hashrate
	limit := 5
	if len(miners) < limit {
		limit = len(miners)
	}
	overview.TopMiners = miners[:limit]

	return overview, nil
}

func (s *Store) PruneHistory(olderThan time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	_, err := s.db.Exec(`DELETE FROM hashrate_history WHERE timestamp < ?`, cutoff)
	return err
}

func scanMiner(rows *sql.Rows) (*models.Miner, error) {
	m := &models.Miner{}
	var configJSON, lastSeen string
	var hCurrent, hAverage, hMax float64

	err := rows.Scan(&m.ID, &m.MinerID, &m.WorkerID, &m.Hostname, &m.IP,
		&m.CPUModel, &m.CPUFamily, &m.Cores, &m.OS, &m.Arch,
		&m.XmrigVersion, &m.TarishVersion, &m.UptimeSeconds,
		&hCurrent, &hAverage, &hMax, &configJSON, &lastSeen)
	if err != nil {
		return nil, err
	}

	m.Hashrate = &models.HashrateData{Current: hCurrent, Average: hAverage, Max: hMax}
	m.LastSeen = parseTime(lastSeen)
	m.Status = minerStatus(m.LastSeen)

	if configJSON != "" && configJSON != "{}" {
		json.Unmarshal([]byte(configJSON), &m.Config)
	}

	return m, nil
}

// parseTime tries multiple formats that SQLite/go-sqlite3 may produce
func parseTime(s string) time.Time {
	for _, fmt := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(fmt, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func minerStatus(lastSeen time.Time) string {
	since := time.Since(lastSeen)
	if since < 90*time.Second {
		return "online"
	}
	if since < 5*time.Minute {
		return "stale"
	}
	return "offline"
}
