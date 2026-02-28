package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"tarish-server/models"
)

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	var report models.AgentReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if report.MinerID == "" && report.WorkerID == "" {
		http.Error(w, "miner_id or worker_id required", http.StatusBadRequest)
		return
	}

	if err := s.store.UpsertMiner(&report); err != nil {
		http.Error(w, "failed to store report", http.StatusInternalServerError)
		return
	}

	id := report.MinerID
	if id == "" {
		id = report.WorkerID
	}

	response := models.ReportResponse{OK: true}

	override, err := s.store.GetConfigOverride(id)
	if err == nil && override != nil {
		response.ConfigOverride = override
		log.Printf("[report] dispatching config override to %s", id)
	}

	writeJSON(w, response)
}

func (s *Server) handleGetMiners(w http.ResponseWriter, r *http.Request) {
	miners, err := s.store.GetMiners()
	if err != nil {
		http.Error(w, "failed to get miners", http.StatusInternalServerError)
		return
	}

	if miners == nil {
		miners = []*models.Miner{}
	}

	writeJSON(w, miners)
}

func (s *Server) handleGetMiner(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	miner, err := s.store.GetMiner(id)
	if err != nil {
		http.Error(w, "miner not found", http.StatusNotFound)
		return
	}

	// If there's a pending override, show it as the miner's config so the
	// dashboard reflects the desired state immediately.
	pending, err := s.store.GetConfigOverride(id)
	if err == nil && pending != nil {
		miner.Config = pending
	} else if miner.Config != nil {
		// Override already applied — xmrig's live API strips some fields
		// (e.g. max-threads-hint). Backfill them from the last override.
		if last, err := s.store.GetLastOverride(id); err == nil && last != nil {
			backfillCPUFields(miner.Config, last)
		}
	}

	writeJSON(w, miner)
}

func (s *Server) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	var override map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&override); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.store.SetConfigOverride(id, override); err != nil {
		http.Error(w, "failed to set config", http.StatusInternalServerError)
		return
	}

	log.Printf("[config] stored config override for %s", id)
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (s *Server) handleAckConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	if err := s.store.MarkConfigApplied(id); err != nil {
		http.Error(w, "failed to ack config", http.StatusInternalServerError)
		return
	}

	log.Printf("[config] config override acknowledged by %s", id)
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (s *Server) handleGetPendingConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	response := models.ReportResponse{OK: true}

	override, err := s.store.GetConfigOverride(id)
	if err == nil && override != nil {
		response.ConfigOverride = override
	}

	writeJSON(w, response)
}

func (s *Server) handleDeleteConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	if err := s.store.DeleteConfigOverride(id); err != nil {
		http.Error(w, "failed to delete config", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"ok": true})
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.store.GetOverview()
	if err != nil {
		http.Error(w, "failed to get overview", http.StatusInternalServerError)
		return
	}

	writeJSON(w, overview)
}

func (s *Server) handleHashrateHistory(w http.ResponseWriter, r *http.Request) {
	minerID := r.URL.Query().Get("miner_id")
	hoursStr := r.URL.Query().Get("hours")

	hours := 24
	if hoursStr != "" {
		if h, err := time.ParseDuration(hoursStr + "h"); err == nil {
			hours = int(h.Hours())
		}
	}

	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)

	history, err := s.store.GetHashrateHistory(minerID, since)
	if err != nil {
		http.Error(w, "failed to get history", http.StatusInternalServerError)
		return
	}

	if history == nil {
		history = []*models.HashrateHistory{}
	}

	writeJSON(w, history)
}

func (s *Server) handleProxySummary(w http.ResponseWriter, r *http.Request) {
	if s.proxyClient == nil {
		http.Error(w, "proxy not configured", http.StatusServiceUnavailable)
		return
	}

	summary, err := s.proxyClient.GetSummary()
	if err != nil {
		http.Error(w, "failed to get proxy summary: "+err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, summary)
}

func (s *Server) handleProxyWorkers(w http.ResponseWriter, r *http.Request) {
	if s.proxyClient == nil {
		http.Error(w, "proxy not configured", http.StatusServiceUnavailable)
		return
	}

	workers, err := s.proxyClient.GetWorkers()
	if err != nil {
		http.Error(w, "failed to get proxy workers: "+err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, workers)
}

// backfillCPUFields copies fields that xmrig's live API strips (like
// max-threads-hint) from the last override into the live config.
func backfillCPUFields(live, override map[string]interface{}) {
	liveCPU, _ := live["cpu"].(map[string]interface{})
	overrideCPU, _ := override["cpu"].(map[string]interface{})
	if liveCPU == nil || overrideCPU == nil {
		return
	}

	for _, key := range []string{"max-threads-hint"} {
		if _, exists := liveCPU[key]; !exists {
			if val, ok := overrideCPU[key]; ok {
				liveCPU[key] = val
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
