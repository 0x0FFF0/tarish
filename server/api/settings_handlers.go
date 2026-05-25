package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"tarish-server/models"
	"tarish-server/store"
)

type barkTestRequest struct {
	Token string `json:"token,omitempty"`
}

type barkTestResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (s *Server) handleGetBarkSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetBarkSettings()
	if err != nil {
		http.Error(w, "failed to get bark settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, viewFromSettings(settings))
}

func (s *Server) handleUpdateBarkSettings(w http.ResponseWriter, r *http.Request) {
	var input models.BarkSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	settings, err := s.store.UpdateBarkSettings(input)
	if err != nil {
		http.Error(w, "failed to update bark settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if s.alerter != nil {
		s.alerter.Reload()
	}

	writeJSON(w, viewFromSettings(settings))
}

func (s *Server) handleTestBark(w http.ResponseWriter, r *http.Request) {
	var req barkTestRequest
	// Body is optional; ignore decode errors when body is empty.
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	if s.alerter == nil {
		http.Error(w, "alerter not configured", http.StatusServiceUnavailable)
		return
	}
	err := s.alerter.SendTest(ctx, req.Token)
	resp := barkTestResponse{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	}
	writeJSON(w, resp)
}

func (s *Server) handleSetMute(w http.ResponseWriter, r *http.Request) {
	var req models.MuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var until time.Time
	switch {
	case req.Permanent:
		until = store.ForeverMute
	case req.Minutes > 0:
		until = time.Now().UTC().Add(time.Duration(req.Minutes) * time.Minute)
	default:
		http.Error(w, "minutes or permanent required", http.StatusBadRequest)
		return
	}

	settings, err := s.store.SetMuteUntil(&until)
	if err != nil {
		http.Error(w, "failed to set mute", http.StatusInternalServerError)
		return
	}
	if s.alerter != nil {
		s.alerter.Reload()
	}
	writeJSON(w, viewFromSettings(settings))
}

func (s *Server) handleClearMute(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.SetMuteUntil(nil)
	if err != nil {
		http.Error(w, "failed to clear mute", http.StatusInternalServerError)
		return
	}
	if s.alerter != nil {
		s.alerter.Reload()
	}
	writeJSON(w, viewFromSettings(settings))
}

func (s *Server) handleGetRecentAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}

	alerts, err := s.store.GetRecentAlerts(limit)
	if err != nil {
		http.Error(w, "failed to get alerts", http.StatusInternalServerError)
		return
	}
	if alerts == nil {
		alerts = []*models.AlertLogEntry{}
	}
	writeJSON(w, alerts)
}

// viewFromSettings is the redaction layer between persistent BarkSettings
// (which holds the raw token) and the JSON we return to the browser.
func viewFromSettings(s *models.BarkSettings) *models.BarkSettingsView {
	v := &models.BarkSettingsView{
		Enabled:         s.Enabled,
		CheckIntervalS:  s.CheckIntervalS,
		ThrottleMinutes: s.ThrottleMinutes,
		NotifyRecovery:  s.NotifyRecovery,
		MuteUntil:       s.MuteUntil,
		UpdatedAt:       s.UpdatedAt,
	}
	if s.Token != "" {
		v.TokenSet = true
		if len(s.Token) >= 4 {
			v.TokenLast4 = s.Token[len(s.Token)-4:]
		}
	}
	if s.MuteUntil != nil && !s.MuteUntil.Before(store.ForeverMute) {
		v.MuteForever = true
	}
	return v
}
