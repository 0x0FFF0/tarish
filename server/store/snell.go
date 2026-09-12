package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tarish-server/models"
	"tarish-server/snell"
	"tarish/snellspec"
)

type restorePoint struct {
	Mode          string   `json:"mode"`
	Enabled       *bool    `json:"enabled,omitempty"`
	CustomNodeIDs []string `json:"custom_node_ids,omitempty"`
}

func (s *Store) migrateSnell() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS snell_sources (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL,
			format TEXT NOT NULL DEFAULT 'surge',
			etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '',
			last_success_at DATETIME,
			last_failure_at DATETIME,
			last_error TEXT NOT NULL DEFAULT '',
			validated_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS snell_nodes (
			id TEXT PRIMARY KEY,
			source_id TEXT,
			name TEXT NOT NULL,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			psk TEXT NOT NULL,
			version TEXT NOT NULL,
			obfs_mode TEXT NOT NULL DEFAULT '',
			obfs_host TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			origin TEXT NOT NULL,
			builtin INTEGER NOT NULL DEFAULT 0,
			validated_at DATETIME,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS snell_pool (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			include_source_ids TEXT NOT NULL DEFAULT '[]',
			include_manual_ids TEXT NOT NULL DEFAULT '[]',
			managed_proxy_enabled INTEGER NOT NULL DEFAULT 0,
			policy_version INTEGER NOT NULL DEFAULT 1,
			updated_at DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS miner_snell (
			miner_id TEXT PRIMARY KEY,
			mode TEXT NOT NULL DEFAULT 'local',
			capable INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER,
			custom_node_ids TEXT NOT NULL DEFAULT '[]',
			expected_version INTEGER NOT NULL DEFAULT 0,
			applied_version INTEGER NOT NULL DEFAULT 0,
			applied_at DATETIME,
			apply_error TEXT NOT NULL DEFAULT '',
			selected_node TEXT NOT NULL DEFAULT '',
			restore_json TEXT,
			proxy_json TEXT NOT NULL DEFAULT ''
		);
	`); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`INSERT OR IGNORE INTO snell_pool (id, updated_at) VALUES (1, ?)`, now); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO miner_snell (miner_id, mode, capable)
		SELECT id, 'local', 0 FROM miners
	`); err != nil {
		return err
	}

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS snell_migrations (
			name TEXT PRIMARY KEY
		);
	`); err != nil {
		return err
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO snell_migrations (name) VALUES ('clear_inherit_enroll_enabled')`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if _, err := tx.Exec(`UPDATE miner_snell SET enabled = NULL WHERE mode = 'inherit'`); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *Store) ListSources() ([]*models.SnellSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`
		SELECT id, name, url, format, last_success_at, last_failure_at, last_error, validated_at, created_at,
			(SELECT COUNT(*) FROM snell_nodes n WHERE n.source_id = snell_sources.id)
		FROM snell_sources ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SnellSource
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func scanSource(rows *sql.Rows) (*models.SnellSource, error) {
	src := &models.SnellSource{}
	var success, fail, validated, created sql.NullString
	if err := rows.Scan(&src.ID, &src.Name, &src.URL, &src.Format, &success, &fail, &src.LastError, &validated, &created, &src.NodeCount); err != nil {
		return nil, err
	}
	src.LastSuccessAt = nullTime(success)
	src.LastFailureAt = nullTime(fail)
	src.ValidatedAt = nullTime(validated)
	src.CreatedAt = parseTime(created.String)
	return src, nil
}

func (s *Store) GetSource(id string) (*models.SnellSource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getSourceTx(s.db, id)
}

func (s *Store) getSourceTx(q queryer, id string) (*models.SnellSource, error) {
	row := q.QueryRow(`
		SELECT id, name, url, format, etag, last_modified, last_success_at, last_failure_at, last_error, validated_at, created_at,
			(SELECT COUNT(*) FROM snell_nodes n WHERE n.source_id = snell_sources.id)
		FROM snell_sources WHERE id = ?
	`, id)
	src := &models.SnellSource{}
	var success, fail, validated, created sql.NullString
	if err := row.Scan(&src.ID, &src.Name, &src.URL, &src.Format, &src.ETag, &src.LastModified, &success, &fail, &src.LastError, &validated, &created, &src.NodeCount); err != nil {
		return nil, err
	}
	src.LastSuccessAt = nullTime(success)
	src.LastFailureAt = nullTime(fail)
	src.ValidatedAt = nullTime(validated)
	src.CreatedAt = parseTime(created.String)
	return src, nil
}

type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (s *Store) AddSource(name, rawURL, format string) (*models.SnellSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	if format == "" {
		format = "surge"
	}
	if _, err := s.db.Exec(`
		INSERT INTO snell_sources (id, name, url, format, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, name, rawURL, format, now, now); err != nil {
		return nil, err
	}
	return s.getSourceTx(s.db, id)
}

func (s *Store) DeleteSource(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM snell_nodes WHERE source_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM snell_sources WHERE id = ?`, id); err != nil {
		return err
	}
	if err := removeIDFromPoolLocked(tx, id, true); err != nil {
		return err
	}
	if err := bumpPolicyTx(tx, true, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordSourceFailure(id, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE snell_sources SET last_failure_at = ?, last_error = ?, updated_at = ? WHERE id = ?
	`, now, code, now, id)
	return err
}

func (s *Store) TouchSourceNotModified(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE snell_sources SET last_success_at = ?, last_error = '', validated_at = ?, updated_at = ? WHERE id = ?
	`, nowStr, nowStr, nowStr, id)
	return err
}

func (s *Store) PublishSourceSync(id string, snap *snellspec.Snapshot, etag, lastModified string, fetchedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	preserved := map[string]int{}
	rows, err := tx.Query(`SELECT id, enabled FROM snell_nodes WHERE source_id = ?`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var nid string
		var en int
		if err := rows.Scan(&nid, &en); err != nil {
			rows.Close()
			return err
		}
		preserved[nid] = en
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if _, err := tx.Exec(`DELETE FROM snell_nodes WHERE source_id = ?`, id); err != nil {
		return err
	}
	now := fetchedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowStr := now.Format(time.RFC3339)
	for _, n := range snap.Nodes {
		n = snellspec.FinalizeNode(n)
		origin := "subscription"
		builtin := 0
		if snellspec.IsSeed(n) {
			builtin = 1
		}
		en := 1
		if prev, ok := preserved[n.ID]; ok {
			en = prev
		}
		if _, err := tx.Exec(`
			INSERT INTO snell_nodes (id, source_id, name, host, port, psk, version, obfs_mode, obfs_host, enabled, origin, builtin, validated_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				source_id=excluded.source_id,
				name=excluded.name,
				host=excluded.host,
				port=excluded.port,
				psk=excluded.psk,
				version=excluded.version,
				obfs_mode=excluded.obfs_mode,
				obfs_host=excluded.obfs_host,
				origin=excluded.origin,
				builtin=excluded.builtin,
				validated_at=excluded.validated_at,
				updated_at=excluded.updated_at,
				enabled=excluded.enabled
		`, n.ID, id, n.Name, n.Host, n.Port, n.PSK, n.Version, n.ObfsMode, n.ObfsHost, en, origin, builtin, nowStr, nowStr, nowStr); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		UPDATE snell_sources SET etag = ?, last_modified = ?, last_success_at = ?, last_failure_at = last_failure_at,
			last_error = '', validated_at = ?, updated_at = ? WHERE id = ?
	`, etag, lastModified, nowStr, nowStr, nowStr, id); err != nil {
		return err
	}
	if err := bumpPolicyTx(tx, true, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) catalogNodes() ([]snell.CatalogNode, error) {
	rows, err := s.db.Query(`
		SELECT id, source_id, name, host, port, psk, version, obfs_mode, obfs_host, enabled, origin, builtin, validated_at
		FROM snell_nodes
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []snell.CatalogNode
	for rows.Next() {
		var n snell.CatalogNode
		var sourceID sql.NullString
		var enabled, builtin int
		var origin string
		var validated sql.NullString
		if err := rows.Scan(&n.Spec.ID, &sourceID, &n.Spec.Name, &n.Spec.Host, &n.Spec.Port, &n.Spec.PSK, &n.Spec.Version, &n.Spec.ObfsMode, &n.Spec.ObfsHost, &enabled, &origin, &builtin, &validated); err != nil {
			return nil, err
		}
		n.SourceID = sourceID.String
		n.Manual = origin == "manual"
		n.Enabled = enabled == 1
		n.Spec.Builtin = builtin == 1
		if t := nullTime(validated); t != nil {
			n.ValidatedAt = *t
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (s *Store) ListNodeViews() ([]*models.SnellNodeView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listNodeViewsTx()
}

func (s *Store) listNodeViewsTx() ([]*models.SnellNodeView, error) {
	rows, err := s.db.Query(`
		SELECT n.id, n.source_id, n.name, n.host, n.port, n.psk, n.version, n.obfs_mode, n.obfs_host, n.enabled, n.origin, n.builtin,
			COALESCE(s.name, '')
		FROM snell_nodes n
		LEFT JOIN snell_sources s ON s.id = n.source_id
		ORDER BY n.builtin DESC, n.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SnellNodeView
	for rows.Next() {
		v := &models.SnellNodeView{}
		var sourceID sql.NullString
		var enabled, builtin int
		var sourceName string
		if err := rows.Scan(&v.ID, &sourceID, &v.Name, &v.Host, &v.Port, &v.PSK, &v.Version, &v.ObfsMode, &v.ObfsHost, &enabled, &v.Origin, &builtin, &sourceName); err != nil {
			return nil, err
		}
		v.SourceID = sourceID.String
		v.Enabled = enabled == 1
		v.Builtin = builtin == 1
		v.Manual = v.Origin == "manual"
		v.PSKSet = v.PSK != ""
		if v.Builtin {
			v.Role = snellspec.RoleBuiltin
			v.Source = snellspec.RoleBuiltin
		} else if v.Manual {
			v.Source = "manual"
		} else if sourceName != "" {
			v.Source = sourceName
		} else {
			v.Source = "subscription"
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) AddManualNode(name, host string, port int, psk, version, obfsMode, obfsHost string) (*models.SnellNodeView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spec := snellspec.FinalizeNode(snellspec.NodeSpec{
		Name:     name,
		Host:     host,
		Port:     port,
		PSK:      psk,
		Version:  version,
		ObfsMode: obfsMode,
		ObfsHost: obfsHost,
	})
	if spec.PSK == "" || spec.Host == "" {
		return nil, fmt.Errorf("invalid node")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`
		INSERT INTO snell_nodes (id, source_id, name, host, port, psk, version, obfs_mode, obfs_host, enabled, origin, builtin, validated_at, created_at, updated_at)
		VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, 1, 'manual', 0, ?, ?, ?)
	`, spec.ID, spec.Name, spec.Host, spec.Port, spec.PSK, spec.Version, spec.ObfsMode, spec.ObfsHost, now, now, now); err != nil {
		return nil, err
	}
	if err := bumpPolicyTx(tx, false, []string{spec.ID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &models.SnellNodeView{
		ID: spec.ID, Name: spec.Name, Version: spec.Version, Enabled: true,
		Origin: "manual", Manual: true, Source: "manual", PSKSet: true,
	}, nil
}

func (s *Store) UpdateNode(id, name string, enabled *bool, host string, port int, psk, version, obfsMode, obfsHost string, editTransport bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var origin string
	err = tx.QueryRow(`SELECT origin FROM snell_nodes WHERE id = ?`, id).Scan(&origin)
	if err != nil {
		return err
	}
	if origin != "manual" && editTransport {
		return fmt.Errorf("imported nodes cannot edit connection fields")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if origin == "manual" && editTransport {
		spec := snellspec.FinalizeNode(snellspec.NodeSpec{
			Name: name, Host: host, Port: port, PSK: psk, Version: version, ObfsMode: obfsMode, ObfsHost: obfsHost,
		})
		en := 1
		if enabled != nil && !*enabled {
			en = 0
		}
		if _, err := tx.Exec(`
			UPDATE snell_nodes SET id = ?, name = ?, host = ?, port = ?, psk = ?, version = ?, obfs_mode = ?, obfs_host = ?, enabled = ?, validated_at = ?, updated_at = ?
			WHERE id = ?
		`, spec.ID, spec.Name, spec.Host, spec.Port, spec.PSK, spec.Version, spec.ObfsMode, spec.ObfsHost, en, now, now, id); err != nil {
			return err
		}
		if spec.ID != id {
			if err := rewriteManualPoolID(tx, id, spec.ID); err != nil {
				return err
			}
		}
	} else {
		if name != "" {
			if _, err := tx.Exec(`UPDATE snell_nodes SET name = ?, updated_at = ? WHERE id = ?`, name, now, id); err != nil {
				return err
			}
		}
		if enabled != nil {
			en := 0
			if *enabled {
				en = 1
			}
			if _, err := tx.Exec(`UPDATE snell_nodes SET enabled = ?, updated_at = ? WHERE id = ?`, en, now, id); err != nil {
				return err
			}
		}
	}
	if err := bumpPolicyTx(tx, true, []string{id}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteNode(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM snell_nodes WHERE id = ?`, id); err != nil {
		return err
	}
	if err := removeIDFromPoolLocked(tx, id, false); err != nil {
		return err
	}
	if err := bumpPolicyTx(tx, true, []string{id}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetPool() (*models.SnellPoolView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getPoolTx()
}

func (s *Store) getPoolTx() (*models.SnellPoolView, error) {
	var srcJSON, manJSON string
	var enabled, version int
	err := s.db.QueryRow(`SELECT include_source_ids, include_manual_ids, managed_proxy_enabled, policy_version FROM snell_pool WHERE id = 1`).
		Scan(&srcJSON, &manJSON, &enabled, &version)
	if err != nil {
		return nil, err
	}
	view := &models.SnellPoolView{
		IncludeSourceIDs:    decodeIDs(srcJSON),
		IncludeManualIDs:    decodeIDs(manJSON),
		ManagedProxyEnabled: enabled == 1,
		PolicyVersion:       version,
	}
	nodes, err := s.catalogNodes()
	if err != nil {
		return nil, err
	}
	cfg := snell.PoolConfig{IncludeSourceIDs: view.IncludeSourceIDs, IncludeManualIDs: view.IncludeManualIDs, ManagedProxyEnabled: view.ManagedProxyEnabled, PolicyVersion: view.PolicyVersion}
	view.Valid = snell.PoolValid(nodes, cfg)
	n := 0
	for _, c := range nodes {
		if !c.Enabled || snellspec.IsSeed(c.Spec) {
			continue
		}
		if c.Manual {
			for _, id := range view.IncludeManualIDs {
				if id == c.Spec.ID {
					n++
				}
			}
			continue
		}
		for _, id := range view.IncludeSourceIDs {
			if id == c.SourceID {
				n++
			}
		}
	}
	view.NodeCount = n
	return view, nil
}

func (s *Store) UpdatePool(includeSources, includeManual []string, managedEnabled bool) (*models.SnellPoolView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	en := 0
	if managedEnabled {
		en = 1
	}
	src, _ := json.Marshal(includeSources)
	man, _ := json.Marshal(includeManual)
	if _, err := tx.Exec(`
		UPDATE snell_pool SET include_source_ids = ?, include_manual_ids = ?, managed_proxy_enabled = ?, updated_at = ? WHERE id = 1
	`, string(src), string(man), en, now); err != nil {
		return nil, err
	}
	if err := bumpPolicyTx(tx, true, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getPoolTx()
}

func nextPolicyVersion(tx *sql.Tx) (int, error) {
	var poolVer int
	if err := tx.QueryRow(`SELECT policy_version FROM snell_pool WHERE id = 1`).Scan(&poolVer); err != nil {
		return 0, err
	}
	var maxExp sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(expected_version) FROM miner_snell`).Scan(&maxExp); err != nil {
		return 0, err
	}
	next := poolVer
	if maxExp.Valid && int(maxExp.Int64) > next {
		next = int(maxExp.Int64)
	}
	return next + 1, nil
}

func stampPoolVersion(tx *sql.Tx, version int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.Exec(`UPDATE snell_pool SET policy_version = MAX(policy_version, ?), updated_at = ? WHERE id = 1`, version, now)
	return err
}

func stampMinerExpectedTx(tx *sql.Tx, minerID string) (int, error) {
	next, err := nextPolicyVersion(tx)
	if err != nil {
		return 0, err
	}
	if err := stampPoolVersion(tx, next); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`UPDATE miner_snell SET expected_version = ? WHERE miner_id = ?`, next, minerID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("unknown miner")
	}
	return next, nil
}

func bumpPolicyTx(tx *sql.Tx, inherit bool, customNodeIDs []string) error {
	version, err := nextPolicyVersion(tx)
	if err != nil {
		return err
	}
	if err := stampPoolVersion(tx, version); err != nil {
		return err
	}
	if inherit {
		if _, err := tx.Exec(`UPDATE miner_snell SET expected_version = MAX(expected_version, ?) WHERE mode = 'inherit'`, version); err != nil {
			return err
		}
	}
	if len(customNodeIDs) > 0 {
		rows, err := tx.Query(`SELECT miner_id, custom_node_ids FROM miner_snell WHERE mode = 'custom'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		want := map[string]bool{}
		for _, id := range customNodeIDs {
			want[id] = true
		}
		var ids []string
		for rows.Next() {
			var minerID, customJSON string
			if err := rows.Scan(&minerID, &customJSON); err != nil {
				return err
			}
			for _, id := range decodeIDs(customJSON) {
				if want[id] {
					ids = append(ids, minerID)
					break
				}
			}
		}
		rows.Close()
		for _, minerID := range ids {
			if _, err := tx.Exec(`UPDATE miner_snell SET expected_version = MAX(expected_version, ?) WHERE miner_id = ?`, version, minerID); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeIDFromPoolLocked(tx *sql.Tx, id string, source bool) error {
	var srcJSON, manJSON string
	if err := tx.QueryRow(`SELECT include_source_ids, include_manual_ids FROM snell_pool WHERE id = 1`).Scan(&srcJSON, &manJSON); err != nil {
		return err
	}
	if source {
		srcJSON = encodeIDs(filterID(decodeIDs(srcJSON), id))
	} else {
		manJSON = encodeIDs(filterID(decodeIDs(manJSON), id))
	}
	_, err := tx.Exec(`UPDATE snell_pool SET include_source_ids = ?, include_manual_ids = ? WHERE id = 1`, srcJSON, manJSON)
	return err
}

func rewriteManualPoolID(tx *sql.Tx, oldID, newID string) error {
	var manJSON string
	if err := tx.QueryRow(`SELECT include_manual_ids FROM snell_pool WHERE id = 1`).Scan(&manJSON); err != nil {
		return err
	}
	ids := decodeIDs(manJSON)
	for i, id := range ids {
		if id == oldID {
			ids[i] = newID
		}
	}
	_, err := tx.Exec(`UPDATE snell_pool SET include_manual_ids = ? WHERE id = 1`, encodeIDs(ids))
	return err
}

func (s *Store) EnrollMinerSnell(minerID string, capable, isNew bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := enrollMinerSnellTx(tx, minerID, capable, isNew, s); err != nil {
		return err
	}
	return tx.Commit()
}

func enrollMinerSnellTx(tx *sql.Tx, minerID string, capable, isNew bool, s *Store) error {
	var mode string
	err := tx.QueryRow(`SELECT mode FROM miner_snell WHERE miner_id = ?`, minerID).Scan(&mode)
	capInt := 0
	if capable {
		capInt = 1
	}
	if err == nil {
		if capable {
			_, err = tx.Exec(`UPDATE miner_snell SET capable = 1 WHERE miner_id = ?`, minerID)
			return err
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	mode = snellspec.ModeLocal
	if isNew && capable {
		if _, perr := s.getPoolUnlocked(tx); perr != nil {
			return perr
		}
		mode = snellspec.ModeInherit
	}
	_, err = tx.Exec(`
		INSERT INTO miner_snell (miner_id, mode, capable, enabled, expected_version)
		VALUES (?, ?, ?, NULL, 0)
	`, minerID, mode, capInt)
	if err != nil {
		return err
	}
	if mode == snellspec.ModeInherit {
		_, err = stampMinerExpectedTx(tx, minerID)
	}
	return err
}

func (s *Store) getPoolUnlocked(tx *sql.Tx) (*models.SnellPoolView, error) {
	var srcJSON, manJSON string
	var enabled, version int
	if err := tx.QueryRow(`SELECT include_source_ids, include_manual_ids, managed_proxy_enabled, policy_version FROM snell_pool WHERE id = 1`).
		Scan(&srcJSON, &manJSON, &enabled, &version); err != nil {
		return nil, err
	}
	view := &models.SnellPoolView{
		IncludeSourceIDs:    decodeIDs(srcJSON),
		IncludeManualIDs:    decodeIDs(manJSON),
		ManagedProxyEnabled: enabled == 1,
		PolicyVersion:       version,
	}
	rows, err := tx.Query(`SELECT id, source_id, name, host, port, psk, version, obfs_mode, obfs_host, enabled, origin, builtin, validated_at FROM snell_nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []snell.CatalogNode
	for rows.Next() {
		var n snell.CatalogNode
		var sourceID sql.NullString
		var en, builtin int
		var origin string
		var validated sql.NullString
		if err := rows.Scan(&n.Spec.ID, &sourceID, &n.Spec.Name, &n.Spec.Host, &n.Spec.Port, &n.Spec.PSK, &n.Spec.Version, &n.Spec.ObfsMode, &n.Spec.ObfsHost, &en, &origin, &builtin, &validated); err != nil {
			return nil, err
		}
		n.SourceID = sourceID.String
		n.Manual = origin == "manual"
		n.Enabled = en == 1
		n.Spec.Builtin = builtin == 1
		nodes = append(nodes, n)
	}
	cfg := snell.PoolConfig{IncludeSourceIDs: view.IncludeSourceIDs, IncludeManualIDs: view.IncludeManualIDs, ManagedProxyEnabled: view.ManagedProxyEnabled, PolicyVersion: view.PolicyVersion}
	view.Valid = snell.PoolValid(nodes, cfg)
	return view, nil
}

func (s *Store) SetMinerSnell(minerID, mode string, enabled *bool, custom []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	customJSON := encodeIDs(custom)
	var en any
	if enabled == nil {
		en = nil
	} else if *enabled {
		en = 1
	} else {
		en = 0
	}
	if _, err := tx.Exec(`
		INSERT INTO miner_snell (miner_id, mode, enabled, custom_node_ids, expected_version, capable)
		VALUES (?, ?, ?, ?, 0, 1)
		ON CONFLICT(miner_id) DO UPDATE SET
			mode=excluded.mode,
			enabled=excluded.enabled,
			custom_node_ids=excluded.custom_node_ids
	`, minerID, mode, en, customJSON); err != nil {
		return err
	}
	if _, err := stampMinerExpectedTx(tx, minerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) BulkEnroll(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := s.getPoolUnlocked(tx); err != nil {
		return err
	}
	for _, id := range ids {
		var mode string
		var enabled sql.NullInt64
		var custom string
		err := tx.QueryRow(`SELECT mode, enabled, custom_node_ids FROM miner_snell WHERE miner_id = ?`, id).Scan(&mode, &enabled, &custom)
		if err == sql.ErrNoRows {
			mode = snellspec.ModeLocal
			custom = "[]"
		} else if err != nil {
			return err
		}
		rp := restorePoint{Mode: mode, CustomNodeIDs: decodeIDs(custom)}
		if enabled.Valid {
			v := enabled.Int64 == 1
			rp.Enabled = &v
		}
		raw, _ := json.Marshal(rp)
		if _, err := tx.Exec(`
			INSERT INTO miner_snell (miner_id, mode, capable, enabled, expected_version, restore_json)
			VALUES (?, 'inherit', 1, NULL, 0, ?)
			ON CONFLICT(miner_id) DO UPDATE SET
				restore_json=excluded.restore_json,
				mode='inherit',
				enabled=NULL,
				capable=1
		`, id, string(raw)); err != nil {
			return err
		}
		if _, err := stampMinerExpectedTx(tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ExitManaged(minerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var restore sql.NullString
	if err := tx.QueryRow(`SELECT restore_json FROM miner_snell WHERE miner_id = ?`, minerID).Scan(&restore); err != nil && err != sql.ErrNoRows {
		return err
	}
	mode := snellspec.ModeLocal
	var enabled any
	custom := "[]"
	if restore.Valid && restore.String != "" {
		var rp restorePoint
		if json.Unmarshal([]byte(restore.String), &rp) == nil {
			if rp.Mode != "" {
				mode = rp.Mode
			}
			if rp.Enabled != nil {
				if *rp.Enabled {
					enabled = 1
				} else {
					enabled = 0
				}
			}
			custom = encodeIDs(rp.CustomNodeIDs)
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO miner_snell (miner_id, mode, enabled, custom_node_ids, expected_version, restore_json)
		VALUES (?, ?, ?, ?, 0, NULL)
		ON CONFLICT(miner_id) DO UPDATE SET
			mode=excluded.mode,
			enabled=excluded.enabled,
			custom_node_ids=excluded.custom_node_ids,
			restore_json=NULL
	`, minerID, mode, enabled, custom); err != nil {
		return err
	}
	if _, err := stampMinerExpectedTx(tx, minerID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AckSnell(minerID string, ack models.SnellAck) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	errCode := ack.ErrorCode
	if ack.OK {
		errCode = ""
	}
	_, err := s.db.Exec(`
		UPDATE miner_snell SET applied_version = ?, applied_at = ?, apply_error = ?, selected_node = ?
		WHERE miner_id = ?
	`, ack.PolicyVersion, now, errCode, ack.SelectedNode, minerID)
	return err
}

func (s *Store) RecordProxyHeartbeat(minerID string, capable bool, proxyJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	capInt := 0
	if capable {
		capInt = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO miner_snell (miner_id, mode, capable, proxy_json)
		VALUES (?, 'local', ?, ?)
		ON CONFLICT(miner_id) DO UPDATE SET
			capable=CASE WHEN excluded.capable = 1 THEN 1 ELSE miner_snell.capable END,
			proxy_json=excluded.proxy_json
	`, minerID, capInt, proxyJSON)
	return err
}

func (s *Store) EffectiveSnapshot(minerID string) (*snellspec.ManagedDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var mode string
	var capable int
	var enabled sql.NullInt64
	var custom string
	var expected int
	err := s.db.QueryRow(`SELECT mode, capable, enabled, custom_node_ids, expected_version FROM miner_snell WHERE miner_id = ?`, minerID).
		Scan(&mode, &capable, &enabled, &custom, &expected)
	if err == sql.ErrNoRows {
		return &snellspec.ManagedDocument{ProtocolVersion: snellspec.ProtocolVersion, Supported: false, Mode: snellspec.ModeLocal}, nil
	}
	if err != nil {
		return nil, err
	}
	nodes, err := s.catalogNodes()
	if err != nil {
		return nil, err
	}
	pool, err := s.getPoolTx()
	if err != nil {
		return nil, err
	}
	mp := snell.MinerPolicy{
		Mode:            mode,
		Capable:         capable == 1,
		CustomNodeIDs:   decodeIDs(custom),
		ExpectedVersion: expected,
	}
	if enabled.Valid {
		v := enabled.Int64 == 1
		mp.Enabled = &v
	}
	cfg := snell.PoolConfig{
		IncludeSourceIDs:    pool.IncludeSourceIDs,
		IncludeManualIDs:    pool.IncludeManualIDs,
		ManagedProxyEnabled: pool.ManagedProxyEnabled,
		PolicyVersion:       pool.PolicyVersion,
	}
	return snell.ComputeEffectiveSnapshot(nodes, cfg, mp, time.Now()), nil
}

func (s *Store) Catalog() (*models.SnellCatalog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sources, err := s.listSourcesNoLock()
	if err != nil {
		return nil, err
	}
	nodes, err := s.listNodeViewsTx()
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		n.PSK = ""
		n.Host = ""
		n.Port = 0
		n.ObfsMode = ""
		n.ObfsHost = ""
	}
	pool, err := s.getPoolTx()
	if err != nil {
		return nil, err
	}
	machines, err := s.listMachinesTx()
	if err != nil {
		return nil, err
	}
	seed := snellspec.SeedNode()
	return &models.SnellCatalog{
		Sources:  sources,
		Nodes:    nodes,
		Pool:     pool,
		Machines: machines,
		Seed: &models.SnellNodeView{
			ID: seed.ID, Name: snellspec.RoleBuiltin, Version: seed.Version,
			Origin: "seed", Builtin: true, Role: snellspec.RoleBuiltin, Source: snellspec.RoleBuiltin,
		},
	}, nil
}

func (s *Store) listSourcesNoLock() ([]*models.SnellSource, error) {
	rows, err := s.db.Query(`
		SELECT id, name, url, format, last_success_at, last_failure_at, last_error, validated_at, created_at,
			(SELECT COUNT(*) FROM snell_nodes n WHERE n.source_id = snell_sources.id)
		FROM snell_sources ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.SnellSource
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		src.URL = snell.RedactURL(src.URL)
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) ListMachines() ([]*models.MinerSnellView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listMachinesTx()
}

func (s *Store) listMachinesTx() ([]*models.MinerSnellView, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.hostname, COALESCE(ms.mode, 'local'), COALESCE(ms.capable, 0), ms.enabled,
			COALESCE(ms.custom_node_ids, '[]'), COALESCE(ms.expected_version, 0), COALESCE(ms.applied_version, 0),
			COALESCE(ms.selected_node, ''), COALESCE(ms.apply_error, ''), COALESCE(ms.proxy_json, '')
		FROM miners m
		LEFT JOIN miner_snell ms ON ms.miner_id = m.id
		ORDER BY m.hostname ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.MinerSnellView
	for rows.Next() {
		v := &models.MinerSnellView{}
		var capable int
		var enabled sql.NullInt64
		var custom, proxyJSON string
		if err := rows.Scan(&v.MinerID, &v.Hostname, &v.Mode, &capable, &enabled, &custom, &v.ExpectedVersion, &v.AppliedVersion, &v.SelectedNode, &v.ApplyError, &proxyJSON); err != nil {
			return nil, err
		}
		v.Capable = capable == 1
		if !v.Capable {
			v.SupportedLabel = "不支持"
		}
		if enabled.Valid {
			b := enabled.Int64 == 1
			v.Enabled = &b
		}
		v.CustomNodeIDs = decodeIDs(custom)
		if proxyJSON != "" {
			var p map[string]any
			if json.Unmarshal([]byte(proxyJSON), &p) == nil {
				if r, ok := p["route"].(string); ok {
					v.Route = r
				}
				if e, ok := p["proxy_enabled"].(bool); ok {
					v.ProxyEnabled = e
				}
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetMinerSnell(id string) (*models.MinerSnellView, error) {
	machines, err := s.ListMachines()
	if err != nil {
		return nil, err
	}
	for _, m := range machines {
		if m.MinerID == id {
			return m, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *Store) SourceValidatedAtUnchanged(id string, before *time.Time) (bool, error) {
	src, err := s.GetSource(id)
	if err != nil {
		return false, err
	}
	if before == nil && src.ValidatedAt == nil {
		return true, nil
	}
	if before == nil || src.ValidatedAt == nil {
		return false, nil
	}
	return before.Equal(*src.ValidatedAt), nil
}

func decodeIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return []string{}
	}
	return ids
}

func encodeIDs(ids []string) string {
	if ids == nil {
		ids = []string{}
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func filterID(ids []string, drop string) []string {
	var out []string
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}

func nullTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parseTime(v.String)
	if t.IsZero() {
		return nil
	}
	return &t
}
