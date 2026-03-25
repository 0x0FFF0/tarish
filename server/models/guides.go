package models

import "time"

const (
	GuideCategoryGeneral  = "general"
	GuideCategoryScript   = "script"
	GuideCategoryTerminal = "terminal"

	GuideContentTypeText = "text"
	GuideContentTypeCode = "code"
)

type GuideLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type GuideDocument struct {
	ID            string      `json:"id"`
	Slug          string      `json:"slug"`
	Title         string      `json:"title"`
	Summary       string      `json:"summary"`
	Category      string      `json:"category"`
	ContentType   string      `json:"content_type"`
	Content       string      `json:"content"`
	Links         []GuideLink `json:"links"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	RevisionCount int         `json:"revision_count"`
	CanRollback   bool        `json:"can_rollback"`
}

type GuideDocumentInput struct {
	Title       string      `json:"title"`
	Summary     string      `json:"summary"`
	Category    string      `json:"category"`
	ContentType string      `json:"content_type"`
	Content     string      `json:"content"`
	Links       []GuideLink `json:"links"`
}

type GuideEditChallenge struct {
	ChallengeID string    `json:"challenge_id"`
	Prompt      string    `json:"prompt"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type GuideEditSession struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
