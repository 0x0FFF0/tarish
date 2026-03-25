package store

import (
	"testing"

	"tarish-server/models"
)

func TestGuideDocumentsSeeded(t *testing.T) {
	s := newTestStore(t)

	docs, err := s.GetGuideDocuments()
	if err != nil {
		t.Fatalf("GetGuideDocuments: %v", err)
	}

	if len(docs) < 3 {
		t.Fatalf("GetGuideDocuments len = %d, want at least 3", len(docs))
	}
}

func TestGuideDocumentUpdateAndRollback(t *testing.T) {
	s := newTestStore(t)

	originalDocs, err := s.GetGuideDocuments()
	if err != nil {
		t.Fatalf("GetGuideDocuments: %v", err)
	}

	var target *models.GuideDocument
	for _, doc := range originalDocs {
		if doc.ID == "remote-deployment-script" {
			target = doc
			break
		}
	}
	if target == nil {
		t.Fatal("remote-deployment-script not found")
	}

	updated, err := s.UpdateGuideDocument(target.ID, models.GuideDocumentInput{
		Title:       target.Title,
		Summary:     "Updated deployment path for canary miners.",
		Category:    target.Category,
		ContentType: target.ContentType,
		Content:     "curl -fsSL https://deploy.example/install.sh | bash",
		Links: []models.GuideLink{
			{Label: "Miners", URL: "/miners"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateGuideDocument: %v", err)
	}

	if updated.RevisionCount != 1 {
		t.Fatalf("updated.RevisionCount = %d, want 1", updated.RevisionCount)
	}
	if !updated.CanRollback {
		t.Fatal("updated.CanRollback = false, want true")
	}

	rolledBack, err := s.RollbackGuideDocument(target.ID)
	if err != nil {
		t.Fatalf("RollbackGuideDocument: %v", err)
	}

	if rolledBack.Content != target.Content {
		t.Fatalf("rolledBack.Content = %q, want %q", rolledBack.Content, target.Content)
	}
	if rolledBack.Summary != target.Summary {
		t.Fatalf("rolledBack.Summary = %q, want %q", rolledBack.Summary, target.Summary)
	}
	if rolledBack.RevisionCount != 1 {
		t.Fatalf("rolledBack.RevisionCount = %d, want 1", rolledBack.RevisionCount)
	}
}
