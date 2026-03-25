package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"tarish-server/models"
	storepkg "tarish-server/store"
)

type guideEditSessionRequest struct {
	ChallengeID string `json:"challenge_id"`
	Answer      string `json:"answer"`
}

type guideDocumentRequest struct {
	Confirmed bool `json:"confirmed"`
	models.GuideDocumentInput
}

type guideRollbackRequest struct {
	Confirmed bool `json:"confirmed"`
}

func (s *Server) handleGetGuideDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := s.store.GetGuideDocuments()
	if err != nil {
		http.Error(w, "failed to get guide documents", http.StatusInternalServerError)
		return
	}

	if docs == nil {
		docs = []*models.GuideDocument{}
	}

	writeJSON(w, docs)
}

func (s *Server) handleStartGuideEditChallenge(w http.ResponseWriter, r *http.Request) {
	challenge, err := s.guideEditor.StartChallenge()
	if err != nil {
		http.Error(w, "failed to start guide edit challenge", http.StatusInternalServerError)
		return
	}

	writeJSON(w, challenge)
}

func (s *Server) handleCreateGuideEditSession(w http.ResponseWriter, r *http.Request) {
	var req guideEditSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	session, err := s.guideEditor.VerifyChallenge(req.ChallengeID, req.Answer)
	if err != nil {
		status := http.StatusUnauthorized
		if !errors.Is(err, ErrInvalidGuideEditChallenge) {
			status = http.StatusInternalServerError
		}
		http.Error(w, err.Error(), status)
		return
	}

	writeJSON(w, session)
}

func (s *Server) handleCreateGuideDocument(w http.ResponseWriter, r *http.Request) {
	var req guideDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !req.Confirmed {
		http.Error(w, "confirmed save required", http.StatusBadRequest)
		return
	}
	if err := validateGuideDocumentInput(req.GuideDocumentInput); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	doc, err := s.store.CreateGuideDocument(req.GuideDocumentInput)
	if err != nil {
		http.Error(w, "failed to create guide document: "+err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, doc)
}

func (s *Server) handleUpdateGuideDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	var req guideDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !req.Confirmed {
		http.Error(w, "confirmed save required", http.StatusBadRequest)
		return
	}
	if err := validateGuideDocumentInput(req.GuideDocumentInput); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	doc, err := s.store.UpdateGuideDocument(id, req.GuideDocumentInput)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, sql.ErrNoRows):
			status = http.StatusNotFound
		case errors.Is(err, storepkg.ErrNoGuideRevision):
			status = http.StatusConflict
		}
		http.Error(w, "failed to update guide document: "+err.Error(), status)
		return
	}

	writeJSON(w, doc)
}

func (s *Server) handleRollbackGuideDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	var req guideRollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !req.Confirmed {
		http.Error(w, "confirmed rollback required", http.StatusBadRequest)
		return
	}

	doc, err := s.store.RollbackGuideDocument(id)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, sql.ErrNoRows):
			status = http.StatusNotFound
		case errors.Is(err, storepkg.ErrNoGuideRevision):
			status = http.StatusConflict
		}
		http.Error(w, "failed to rollback guide document: "+err.Error(), status)
		return
	}

	writeJSON(w, doc)
}

func validateGuideDocumentInput(input models.GuideDocumentInput) error {
	for _, link := range input.Links {
		if !isAllowedGuideURL(link.URL) {
			return errors.New("guide links must start with /, http://, or https://")
		}
	}
	return nil
}

func isAllowedGuideURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return strings.HasPrefix(trimmed, "/") ||
		strings.HasPrefix(trimmed, "http://") ||
		strings.HasPrefix(trimmed, "https://")
}
