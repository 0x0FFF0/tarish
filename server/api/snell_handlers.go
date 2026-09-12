package api

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"tarish-server/models"
	"tarish-server/snell"
	"tarish/snellspec"
)

type snellSourceInput struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Format string `json:"format"`
}

type snellDocumentInput struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Format   string `json:"format"`
	Document string `json:"document"`
}

type snellNodeInput struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	PSK      string `json:"psk"`
	Version  string `json:"version"`
	ObfsMode string `json:"obfs_mode"`
	ObfsHost string `json:"obfs_host"`
	Enabled  *bool  `json:"enabled"`
}

type snellPoolInput struct {
	IncludeSourceIDs    []string `json:"include_source_ids"`
	IncludeManualIDs    []string `json:"include_manual_ids"`
	ManagedProxyEnabled bool     `json:"managed_proxy_enabled"`
}

type snellMinerInput struct {
	Mode          string   `json:"mode"`
	Enabled       *bool    `json:"enabled"`
	CustomNodeIDs []string `json:"custom_node_ids"`
}

type snellEnrollInput struct {
	MinerIDs []string `json:"miner_ids"`
}

func (s *Server) handleSnellCatalog(w http.ResponseWriter, r *http.Request) {
	cat, err := s.store.Catalog()
	if err != nil {
		http.Error(w, "failed to load catalog", http.StatusInternalServerError)
		return
	}
	if cat.Sources == nil {
		cat.Sources = []*models.SnellSource{}
	}
	if cat.Nodes == nil {
		cat.Nodes = []*models.SnellNodeView{}
	}
	if cat.Machines == nil {
		cat.Machines = []*models.MinerSnellView{}
	}
	writeJSON(w, cat)
}

func (s *Server) handleSnellListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListSources()
	if err != nil {
		http.Error(w, "failed to list sources", http.StatusInternalServerError)
		return
	}
	for _, src := range sources {
		src.URL = snell.RedactURL(src.URL)
	}
	if sources == nil {
		sources = []*models.SnellSource{}
	}
	writeJSON(w, sources)
}

func (s *Server) handleSnellAddSource(w http.ResponseWriter, r *http.Request) {
	var in snellSourceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.URL)), "https://") {
		http.Error(w, "https_required", http.StatusBadRequest)
		return
	}
	src, err := s.store.AddSource(in.Name, strings.TrimSpace(in.URL), in.Format)
	if err != nil {
		http.Error(w, "failed to add source", http.StatusInternalServerError)
		return
	}
	if err := s.syncSource(r.Context(), src.ID); err != nil {
		log.Printf("[snell] initial sync %s: %s", src.ID, snellspec.CodeOf(err))
	}
	src, _ = s.store.GetSource(src.ID)
	if src != nil {
		src.URL = snell.RedactURL(src.URL)
	}
	writeJSON(w, src)
}

func (s *Server) handleSnellTestSource(w http.ResponseWriter, r *http.Request) {
	var in snellSourceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	format, err := snellspec.ParseFormat(in.Format)
	if err != nil {
		http.Error(w, snellspec.CodeOf(err), http.StatusBadRequest)
		return
	}
	fetchCtx, cancel := context.WithTimeout(context.Background(), snell.FetchTimeout)
	defer cancel()
	res, err := s.snellFetch.Get(fetchCtx, strings.TrimSpace(in.URL), "", "")
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": snellspec.CodeOf(err)})
		return
	}
	snap, err := snellspec.ParseDocument(res.Body, format)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": snellspec.CodeOf(err)})
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"nodes":   snap.NodeCount,
		"skipped": snap.Skipped,
		"format":  snap.Format,
		"summary": snellspec.RedactedSummary(snap),
	})
}

func (s *Server) handleSnellImportDocument(w http.ResponseWriter, r *http.Request) {
	var in snellDocumentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.URL)), "https://") {
		http.Error(w, "https_required", http.StatusBadRequest)
		return
	}
	format, err := snellspec.ParseFormat(in.Format)
	if err != nil {
		http.Error(w, snellspec.CodeOf(err), http.StatusBadRequest)
		return
	}
	snap, err := snellspec.ParseDocument([]byte(in.Document), format)
	if err != nil {
		http.Error(w, snellspec.CodeOf(err), http.StatusBadRequest)
		return
	}
	src, err := s.store.AddSource(in.Name, strings.TrimSpace(in.URL), string(format))
	if err != nil {
		http.Error(w, "failed to add source", http.StatusInternalServerError)
		return
	}
	if err := s.store.PublishSourceSync(src.ID, snap, "", "", time.Now().UTC()); err != nil {
		http.Error(w, "failed to publish", http.StatusInternalServerError)
		return
	}
	src, _ = s.store.GetSource(src.ID)
	if src != nil {
		src.URL = snell.RedactURL(src.URL)
	}
	writeJSON(w, map[string]any{"ok": true, "source": src, "nodes": snap.NodeCount, "skipped": snap.Skipped})
}

func (s *Server) handleSnellDeleteSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteSource(id); err != nil {
		http.Error(w, "failed to delete source", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSnellSyncSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.syncSource(r.Context(), id); err != nil {
		src, _ := s.store.GetSource(id)
		if src != nil {
			src.URL = snell.RedactURL(src.URL)
		}
		writeJSON(w, map[string]any{"ok": false, "error": snellspec.CodeOf(err), "source": src})
		return
	}
	src, _ := s.store.GetSource(id)
	if src != nil {
		src.URL = snell.RedactURL(src.URL)
	}
	writeJSON(w, map[string]any{"ok": true, "source": src})
}

func (s *Server) handleSnellListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodeViews()
	if err != nil {
		http.Error(w, "failed to list nodes", http.StatusInternalServerError)
		return
	}
	for _, n := range nodes {
		n.PSK = ""
		n.Host = ""
		n.Port = 0
	}
	if nodes == nil {
		nodes = []*models.SnellNodeView{}
	}
	writeJSON(w, nodes)
}

func (s *Server) handleSnellAddNode(w http.ResponseWriter, r *http.Request) {
	var in snellNodeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	ver := in.Version
	if ver == "4" {
		ver = "v4"
	}
	if ver == "5" {
		ver = "v5"
	}
	node, err := s.store.AddManualNode(in.Name, in.Host, in.Port, in.PSK, ver, in.ObfsMode, in.ObfsHost)
	if err != nil {
		http.Error(w, "failed to add node", http.StatusBadRequest)
		return
	}
	writeJSON(w, node)
}

func (s *Server) handleSnellUpdateNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in snellNodeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	editTransport := in.Host != "" || in.PSK != "" || in.Port != 0
	if err := s.store.UpdateNode(id, in.Name, in.Enabled, in.Host, in.Port, in.PSK, in.Version, in.ObfsMode, in.ObfsHost, editTransport); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSnellDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteNode(id); err != nil {
		http.Error(w, "failed to delete node", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSnellGetPool(w http.ResponseWriter, r *http.Request) {
	pool, err := s.store.GetPool()
	if err != nil {
		http.Error(w, "failed to get pool", http.StatusInternalServerError)
		return
	}
	writeJSON(w, pool)
}

func (s *Server) handleSnellUpdatePool(w http.ResponseWriter, r *http.Request) {
	var in snellPoolInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	pool, err := s.store.UpdatePool(in.IncludeSourceIDs, in.IncludeManualIDs, in.ManagedProxyEnabled)
	if err != nil {
		http.Error(w, "failed to update pool", http.StatusInternalServerError)
		return
	}
	writeJSON(w, pool)
}

func (s *Server) handleSnellMachines(w http.ResponseWriter, r *http.Request) {
	machines, err := s.store.ListMachines()
	if err != nil {
		http.Error(w, "failed to list machines", http.StatusInternalServerError)
		return
	}
	if machines == nil {
		machines = []*models.MinerSnellView{}
	}
	writeJSON(w, machines)
}

func (s *Server) handleSnellSetMiner(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in snellMinerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	switch in.Mode {
	case snellspec.ModeInherit, snellspec.ModeCustom, snellspec.ModeLocal:
	default:
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	if err := s.store.SetMinerSnell(id, in.Mode, in.Enabled, in.CustomNodeIDs); err != nil {
		http.Error(w, "failed to update miner", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSnellBulkEnroll(w http.ResponseWriter, r *http.Request) {
	var in snellEnrollInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.store.BulkEnroll(in.MinerIDs); err != nil {
		http.Error(w, "failed to enroll", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSnellExitManaged(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.ExitManaged(id); err != nil {
		http.Error(w, "failed to exit managed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSnellPending(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	doc, err := s.store.EffectiveSnapshot(id)
	if err != nil {
		http.Error(w, "failed to build snapshot", http.StatusInternalServerError)
		return
	}
	if !doc.Supported {
		writeJSON(w, doc)
		return
	}
	writeJSON(w, doc)
}

func (s *Server) handleSnellAck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var ack models.SnellAck
	if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.store.AckSnell(id, ack); err != nil {
		http.Error(w, "failed to ack", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) syncSource(ctx context.Context, id string) error {
	src, err := s.store.GetSource(id)
	if err != nil {
		return err
	}
	format, err := snellspec.ParseFormat(src.Format)
	if err != nil {
		_ = s.store.RecordSourceFailure(id, snellspec.CodeOf(err))
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	fetchCtx, cancel := context.WithTimeout(context.Background(), snell.FetchTimeout)
	defer cancel()
	res, err := s.snellFetch.Get(fetchCtx, src.URL, src.ETag, src.LastModified)
	if err != nil {
		_ = s.store.RecordSourceFailure(id, snellspec.CodeOf(err))
		return err
	}
	if res.NotModified {
		return s.store.TouchSourceNotModified(id)
	}
	snap, err := snellspec.ParseDocument(res.Body, format)
	if err != nil {
		_ = s.store.RecordSourceFailure(id, snellspec.CodeOf(err))
		return err
	}
	return s.store.PublishSourceSync(id, snap, res.ETag, res.LastModified, time.Now().UTC())
}

func (s *Server) RunSnellSyncLoop(ctx context.Context) {
	timer := time.NewTimer(snellJitteredInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			sources, err := s.store.ListSources()
			if err == nil {
				for _, src := range sources {
					if err := s.syncSource(ctx, src.ID); err != nil {
						log.Printf("[snell] sync %s: %s", src.ID, snellspec.CodeOf(err))
					}
				}
			}
			timer.Reset(snellJitteredInterval())
		}
	}
}

func snellJitteredInterval() time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<53))
	u := 0.5
	if err == nil {
		u = float64(n.Int64()) / float64(1<<53)
	} else {
		var b [8]byte
		_, _ = rand.Read(b[:])
		u = float64(binary.BigEndian.Uint64(b[:])>>11) / float64(1<<53)
	}
	j := (u*2 - 1) * snell.RefreshJitter
	return time.Duration(float64(snell.RefreshBase) * (1 + j))
}
