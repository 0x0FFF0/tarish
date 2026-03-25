package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"tarish-server/models"
)

var ErrNoGuideRevision = errors.New("no guide revision available")

var guideSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Store) migrateGuides() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS guide_documents (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			title TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT 'general',
			content_type TEXT NOT NULL DEFAULT 'text',
			content TEXT NOT NULL DEFAULT '',
			links_json TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		);

		CREATE TABLE IF NOT EXISTS guide_document_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id TEXT NOT NULL,
			title TEXT NOT NULL,
			summary TEXT NOT NULL,
			category TEXT NOT NULL,
			content_type TEXT NOT NULL,
			content TEXT NOT NULL,
			links_json TEXT NOT NULL DEFAULT '[]',
			reason TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			FOREIGN KEY(document_id) REFERENCES guide_documents(id)
		);

		CREATE INDEX IF NOT EXISTS idx_guide_revisions_document
			ON guide_document_revisions(document_id, id DESC);
	`); err != nil {
		return err
	}

	if err := seedDefaultGuideDocuments(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetGuideDocuments() ([]*models.GuideDocument, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT
			d.id,
			d.slug,
			d.title,
			d.summary,
			d.category,
			d.content_type,
			d.content,
			d.links_json,
			d.created_at,
			d.updated_at,
			COALESCE((
				SELECT COUNT(1)
				FROM guide_document_revisions r
				WHERE r.document_id = d.id
			), 0) AS revision_count
		FROM guide_documents d
		ORDER BY
			CASE d.category
				WHEN 'general' THEN 0
				WHEN 'script' THEN 1
				WHEN 'terminal' THEN 2
				ELSE 3
			END,
			d.updated_at DESC,
			d.title ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []*models.GuideDocument
	for rows.Next() {
		doc, err := scanGuideDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func (s *Store) CreateGuideDocument(input models.GuideDocumentInput) (*models.GuideDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeGuideInput(input)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	id, err := uniqueGuideIDTx(tx, normalized.Title)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	linksJSON, err := marshalGuideLinks(normalized.Links)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`
		INSERT INTO guide_documents (
			id, slug, title, summary, category, content_type, content, links_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, id, normalized.Title, normalized.Summary, normalized.Category, normalized.ContentType, normalized.Content, linksJSON, now, now); err != nil {
		return nil, err
	}

	doc, err := getGuideDocumentTx(tx, id)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *Store) UpdateGuideDocument(id string, input models.GuideDocumentInput) (*models.GuideDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeGuideInput(input)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	current, err := getGuideDocumentTx(tx, id)
	if err != nil {
		return nil, err
	}

	if guideDocumentsEqual(current, &normalized) {
		return current, nil
	}

	if err := insertGuideRevisionTx(tx, current, "update"); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	linksJSON, err := marshalGuideLinks(normalized.Links)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`
		UPDATE guide_documents
		SET title = ?, summary = ?, category = ?, content_type = ?, content = ?, links_json = ?, updated_at = ?
		WHERE id = ?
	`, normalized.Title, normalized.Summary, normalized.Category, normalized.ContentType, normalized.Content, linksJSON, now, id); err != nil {
		return nil, err
	}

	doc, err := getGuideDocumentTx(tx, id)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *Store) RollbackGuideDocument(id string) (*models.GuideDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	current, err := getGuideDocumentTx(tx, id)
	if err != nil {
		return nil, err
	}

	var revisionID int64
	var restored models.GuideDocument
	var linksJSON string
	if err := tx.QueryRow(`
		SELECT id, title, summary, category, content_type, content, links_json
		FROM guide_document_revisions
		WHERE document_id = ?
		ORDER BY id DESC
		LIMIT 1
	`, id).Scan(&revisionID, &restored.Title, &restored.Summary, &restored.Category, &restored.ContentType, &restored.Content, &linksJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoGuideRevision
		}
		return nil, err
	}

	if err := insertGuideRevisionTx(tx, current, "rollback-backup"); err != nil {
		return nil, err
	}

	links, err := unmarshalGuideLinks(linksJSON)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	restoredLinksJSON, err := marshalGuideLinks(links)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`
		UPDATE guide_documents
		SET title = ?, summary = ?, category = ?, content_type = ?, content = ?, links_json = ?, updated_at = ?
		WHERE id = ?
	`, restored.Title, restored.Summary, restored.Category, restored.ContentType, restored.Content, restoredLinksJSON, now, id); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(`DELETE FROM guide_document_revisions WHERE id = ?`, revisionID); err != nil {
		return nil, err
	}

	doc, err := getGuideDocumentTx(tx, id)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return doc, nil
}

func seedDefaultGuideDocuments(tx *sql.Tx) error {
	defaults := []models.GuideDocument{
		{
			ID:          "miner-install-guide",
			Slug:        "miner-install-guide",
			Title:       "Tarish Miner Install Guide",
			Summary:     "Operator-facing install notes for onboarding a miner into the Tarish dashboard.",
			Category:    models.GuideCategoryGeneral,
			ContentType: models.GuideContentTypeText,
			Content: strings.TrimSpace(`
Keep this document as the canonical workflow for installing Tarish Miner.

Suggested flow:
1. Open the remote deployment script and copy the command block you want the operator to run.
2. Share the terminal deployment document when the install requires Tarish Server keys or alternate shell variables.
3. Ask the operator to confirm the miner appears in the dashboard after the command completes.
4. Update this guide whenever the installation path, binary source, or server endpoint changes.
			`),
			Links: []models.GuideLink{
				{Label: "Dashboard Overview", URL: "/"},
				{Label: "Miner Fleet", URL: "/miners"},
			},
		},
		{
			ID:          "remote-deployment-script",
			Slug:        "remote-deployment-script",
			Title:       "Remote Deployment Script",
			Summary:     "Paste the exact install command or bootstrap script operators should run on a target miner.",
			Category:    models.GuideCategoryScript,
			ContentType: models.GuideContentTypeCode,
			Content: strings.TrimSpace(`
# Paste the canonical installer command below.
# Example:
# curl -fsSL https://your-server.example/install-tarish.sh | bash
			`),
		},
		{
			ID:          "other-terminal-deployments",
			Slug:        "other-terminal-deployments",
			Title:       "Other Terminal Deployments",
			Summary:     "Store alternate shell snippets, Tarish Server keys, or environment blocks for special installs.",
			Category:    models.GuideCategoryTerminal,
			ContentType: models.GuideContentTypeCode,
			Content: strings.TrimSpace(`
TARISH_SERVER_URL=https://your-server.example
TARISH_SERVER_KEY=replace-with-server-key
TARISH_AGENT_KEY=replace-with-agent-key
			`),
		},
	}

	for _, doc := range defaults {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM guide_documents WHERE id = ?`, doc.ID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}

		linksJSON, err := marshalGuideLinks(doc.Links)
		if err != nil {
			return err
		}

		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := tx.Exec(`
			INSERT INTO guide_documents (
				id, slug, title, summary, category, content_type, content, links_json, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, doc.ID, doc.Slug, doc.Title, doc.Summary, doc.Category, doc.ContentType, doc.Content, linksJSON, now, now); err != nil {
			return err
		}
	}

	return nil
}

func uniqueGuideIDTx(tx *sql.Tx, title string) (string, error) {
	base := slugifyGuideTitle(title)
	if base == "" {
		base = "document"
	}

	candidate := base
	for suffix := 2; ; suffix++ {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM guide_documents WHERE id = ?`, candidate).Scan(&exists); err != nil {
			return "", err
		}
		if exists == 0 {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func getGuideDocumentTx(tx *sql.Tx, id string) (*models.GuideDocument, error) {
	row := tx.QueryRow(`
		SELECT
			d.id,
			d.slug,
			d.title,
			d.summary,
			d.category,
			d.content_type,
			d.content,
			d.links_json,
			d.created_at,
			d.updated_at,
			COALESCE((
				SELECT COUNT(1)
				FROM guide_document_revisions r
				WHERE r.document_id = d.id
			), 0) AS revision_count
		FROM guide_documents d
		WHERE d.id = ?
	`, id)

	return scanGuideDocument(row)
}

func insertGuideRevisionTx(tx *sql.Tx, doc *models.GuideDocument, reason string) error {
	linksJSON, err := marshalGuideLinks(doc.Links)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO guide_document_revisions (
			document_id, title, summary, category, content_type, content, links_json, reason, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, doc.ID, doc.Title, doc.Summary, doc.Category, doc.ContentType, doc.Content, linksJSON, reason, time.Now().UTC().Format(time.RFC3339))
	return err
}

func scanGuideDocument(scanner interface {
	Scan(dest ...interface{}) error
}) (*models.GuideDocument, error) {
	doc := &models.GuideDocument{}
	var linksJSON string
	var createdAt string
	var updatedAt string

	if err := scanner.Scan(
		&doc.ID,
		&doc.Slug,
		&doc.Title,
		&doc.Summary,
		&doc.Category,
		&doc.ContentType,
		&doc.Content,
		&linksJSON,
		&createdAt,
		&updatedAt,
		&doc.RevisionCount,
	); err != nil {
		return nil, err
	}

	links, err := unmarshalGuideLinks(linksJSON)
	if err != nil {
		return nil, err
	}

	doc.Links = links
	doc.CreatedAt = parseTime(createdAt)
	doc.UpdatedAt = parseTime(updatedAt)
	doc.CanRollback = doc.RevisionCount > 0
	return doc, nil
}

func normalizeGuideInput(input models.GuideDocumentInput) (models.GuideDocumentInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Content = strings.TrimSpace(input.Content)
	input.Category = strings.TrimSpace(input.Category)
	input.ContentType = strings.TrimSpace(input.ContentType)

	if input.Title == "" {
		return input, errors.New("title is required")
	}
	if input.Category == "" {
		input.Category = models.GuideCategoryGeneral
	}
	switch input.Category {
	case models.GuideCategoryGeneral, models.GuideCategoryScript, models.GuideCategoryTerminal:
	default:
		return input, fmt.Errorf("unsupported guide category %q", input.Category)
	}

	if input.ContentType == "" {
		input.ContentType = models.GuideContentTypeText
	}
	switch input.ContentType {
	case models.GuideContentTypeText, models.GuideContentTypeCode:
	default:
		return input, fmt.Errorf("unsupported guide content type %q", input.ContentType)
	}

	normalizedLinks := make([]models.GuideLink, 0, len(input.Links))
	for _, link := range input.Links {
		label := strings.TrimSpace(link.Label)
		url := strings.TrimSpace(link.URL)
		if label == "" && url == "" {
			continue
		}
		if label == "" || url == "" {
			return input, errors.New("guide links require both label and url")
		}
		normalizedLinks = append(normalizedLinks, models.GuideLink{Label: label, URL: url})
	}
	input.Links = normalizedLinks

	return input, nil
}

func guideDocumentsEqual(current *models.GuideDocument, input *models.GuideDocumentInput) bool {
	if current == nil || input == nil {
		return false
	}
	if current.Title != input.Title ||
		current.Summary != input.Summary ||
		current.Category != input.Category ||
		current.ContentType != input.ContentType ||
		current.Content != input.Content {
		return false
	}
	if len(current.Links) != len(input.Links) {
		return false
	}
	for i := range current.Links {
		if current.Links[i] != input.Links[i] {
			return false
		}
	}
	return true
}

func marshalGuideLinks(links []models.GuideLink) (string, error) {
	if len(links) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(links)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalGuideLinks(raw string) ([]models.GuideLink, error) {
	if strings.TrimSpace(raw) == "" {
		return []models.GuideLink{}, nil
	}
	var links []models.GuideLink
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return nil, err
	}
	if links == nil {
		return []models.GuideLink{}, nil
	}
	return links, nil
}

func slugifyGuideTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = guideSlugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	return slug
}
