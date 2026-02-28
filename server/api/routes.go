package api

import (
	"net/http"

	"tarish-server/proxy"
	"tarish-server/store"
)

type Server struct {
	store       *store.Store
	proxyClient *proxy.Client
	agentKey    string
}

func NewServer(s *store.Store, pc *proxy.Client, agentKey string) *Server {
	return &Server{store: s, proxyClient: pc, agentKey: agentKey}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/report", s.authMiddleware(s.handleReport))
	mux.HandleFunc("GET /api/miners", s.handleGetMiners)
	mux.HandleFunc("GET /api/miners/{id}", s.handleGetMiner)
	mux.HandleFunc("PUT /api/miners/{id}/config", s.handleSetConfig)
	mux.HandleFunc("GET /api/miners/{id}/config/pending", s.authMiddleware(s.handleGetPendingConfig))
	mux.HandleFunc("POST /api/miners/{id}/config/ack", s.authMiddleware(s.handleAckConfig))
	mux.HandleFunc("DELETE /api/miners/{id}/config", s.handleDeleteConfig)
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/hashrate/history", s.handleHashrateHistory)
	mux.HandleFunc("GET /api/proxy/summary", s.handleProxySummary)
	mux.HandleFunc("GET /api/proxy/workers", s.handleProxyWorkers)

	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

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
