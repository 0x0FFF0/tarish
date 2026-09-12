package api

import (
	"net/http"
	"strings"

	"tarish-server/alerter"
	"tarish-server/proxy"
	"tarish-server/snell"
	"tarish-server/store"
)

type Server struct {
	store       *store.Store
	proxyClient *proxy.Client
	agentKey    string
	guideEditor *guideEditVerifier
	alerter     *alerter.Alerter
	snellFetch  *snell.Fetcher
}

func NewServer(s *store.Store, pc *proxy.Client, agentKey string, ax *alerter.Alerter) *Server {
	return &Server{
		store:       s,
		proxyClient: pc,
		agentKey:    agentKey,
		guideEditor: newGuideEditVerifier(),
		alerter:     ax,
		snellFetch:  snell.NewFetcher(snell.Limits{}),
	}
}

func (s *Server) SetSnellFetcher(f *snell.Fetcher) {
	s.snellFetch = f
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/report", s.authMiddleware(s.handleReport))
	mux.HandleFunc("GET /api/miners", s.handleGetMiners)
	mux.HandleFunc("GET /api/miners/{id}", s.handleGetMiner)
	mux.HandleFunc("DELETE /api/miners/{id}", s.handleDeleteMiner)
	mux.HandleFunc("PUT /api/miners/{id}/config", s.handleSetConfig)
	mux.HandleFunc("GET /api/miners/{id}/config/pending", s.authMiddleware(s.handleGetPendingConfig))
	mux.HandleFunc("POST /api/miners/{id}/config/ack", s.authMiddleware(s.handleAckConfig))
	mux.HandleFunc("GET /api/miners/{id}/snell/pending", s.requireAgentKey(s.handleSnellPending))
	mux.HandleFunc("POST /api/miners/{id}/snell/ack", s.requireAgentKey(s.handleSnellAck))
	mux.HandleFunc("DELETE /api/miners/{id}/config", s.handleDeleteConfig)
	mux.HandleFunc("GET /api/guides/documents", s.handleGetGuideDocuments)
	mux.HandleFunc("POST /api/guides/documents", s.requireGuideEditToken(s.handleCreateGuideDocument))
	mux.HandleFunc("PUT /api/guides/documents/{id}", s.requireGuideEditToken(s.handleUpdateGuideDocument))
	mux.HandleFunc("POST /api/guides/documents/{id}/rollback", s.requireGuideEditToken(s.handleRollbackGuideDocument))
	mux.HandleFunc("POST /api/guides/edit-challenge", s.handleStartGuideEditChallenge)
	mux.HandleFunc("POST /api/guides/edit-session", s.handleCreateGuideEditSession)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/hashrate/history", s.handleHashrateHistory)
	mux.HandleFunc("GET /api/proxy/summary", s.handleProxySummary)
	mux.HandleFunc("GET /api/proxy/workers", s.handleProxyWorkers)

	mux.HandleFunc("GET /api/settings/bark", s.handleGetBarkSettings)
	mux.HandleFunc("PUT /api/settings/bark", s.handleUpdateBarkSettings)
	mux.HandleFunc("POST /api/settings/bark/test", s.handleTestBark)
	mux.HandleFunc("POST /api/settings/bark/mute", s.handleSetMute)
	mux.HandleFunc("DELETE /api/settings/bark/mute", s.handleClearMute)
	mux.HandleFunc("GET /api/settings/alerts/recent", s.handleGetRecentAlerts)

	mux.HandleFunc("GET /api/snell/", s.handleSnellCatalog)
	mux.HandleFunc("GET /api/snell/catalog", s.handleSnellCatalog)
	mux.HandleFunc("GET /api/snell/status", s.handleSnellCatalog)
	mux.HandleFunc("GET /api/snell/sources", s.handleSnellListSources)
	mux.HandleFunc("POST /api/snell/sources", s.handleSnellAddSource)
	mux.HandleFunc("POST /api/snell/sources/test", s.handleSnellTestSource)
	mux.HandleFunc("POST /api/snell/sources/import-document", s.handleSnellImportDocument)
	mux.HandleFunc("DELETE /api/snell/sources/{id}", s.handleSnellDeleteSource)
	mux.HandleFunc("POST /api/snell/sources/{id}/sync", s.handleSnellSyncSource)
	mux.HandleFunc("GET /api/snell/nodes", s.handleSnellListNodes)
	mux.HandleFunc("POST /api/snell/nodes", s.handleSnellAddNode)
	mux.HandleFunc("PUT /api/snell/nodes/{id}", s.handleSnellUpdateNode)
	mux.HandleFunc("DELETE /api/snell/nodes/{id}", s.handleSnellDeleteNode)
	mux.HandleFunc("GET /api/snell/pool", s.handleSnellGetPool)
	mux.HandleFunc("PUT /api/snell/pool", s.handleSnellUpdatePool)
	mux.HandleFunc("GET /api/snell/machines", s.handleSnellMachines)
	mux.HandleFunc("PUT /api/snell/miners/{id}", s.handleSnellSetMiner)
	mux.HandleFunc("POST /api/snell/miners/enroll", s.handleSnellBulkEnroll)
	mux.HandleFunc("POST /api/snell/miners/{id}/exit", s.handleSnellExitManaged)

	return corsMiddleware(mux)
}

func (s *Server) requireAgentKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(s.agentKey) == "" {
			http.Error(w, "managed-push refused", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+s.agentKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Guide-Edit-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.agentKey != "" {
			token := r.Header.Get("Authorization")
			if token != "Bearer "+s.agentKey {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) requireGuideEditToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.guideEditor.ValidateToken(r.Header.Get("X-Guide-Edit-Token")) {
			http.Error(w, ErrInvalidGuideEditToken.Error(), http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
