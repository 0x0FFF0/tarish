package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"tarish-server/api"
	"tarish-server/proxy"
	"tarish-server/store"
)

//go:embed web/dist
var embeddedWeb embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "tarish.db", "SQLite database path")
	proxyURL := flag.String("proxy-url", "", "xmrig-proxy API URL (e.g. http://127.0.0.1:8080)")
	proxyAPIToken := flag.String("proxy-api-token", "", "access token for xmrig-proxy HTTP API")
	agentKey := flag.String("agent-key", "", "shared secret for agent authentication")
	webDir := flag.String("web", "", "path to web frontend build directory (overrides embedded)")
	flag.Parse()

	// Open SQLite store
	s, err := store.New(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer s.Close()

	// Create proxy client (optional)
	var pc *proxy.Client
	if *proxyURL != "" {
		pc = proxy.NewClient(*proxyURL, *proxyAPIToken)
		log.Printf("xmrig-proxy API: %s", *proxyURL)
	}

	// Create API server
	apiServer := api.NewServer(s, pc, *agentKey)

	// Setup HTTP mux
	mux := http.NewServeMux()

	// API routes
	apiRoutes := apiServer.Routes()
	mux.Handle("/api/", apiRoutes)

	// Serve frontend: prefer --web flag, then try embedded
	if *webDir != "" {
		fileServer := http.FileServer(spaFileSystem{http.Dir(*webDir)})
		mux.Handle("/", fileServer)
		log.Printf("Serving frontend from directory: %s", *webDir)
	} else if hasEmbeddedWeb() {
		subFS, _ := fs.Sub(embeddedWeb, "web/dist")
		fileServer := http.FileServer(spaFileSystem{http.FS(subFS)})
		mux.Handle("/", fileServer)
		log.Printf("Serving embedded frontend")
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				fmt.Fprintln(w, "tarish-server is running. Build the frontend or use --web to serve it.")
				return
			}
			http.NotFound(w, r)
		})
	}

	// Background: prune old hashrate history every hour
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			if err := s.PruneHistory(7 * 24 * time.Hour); err != nil {
				log.Printf("Warning: failed to prune history: %v", err)
			}
		}
	}()

	log.Printf("tarish-server listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func hasEmbeddedWeb() bool {
	_, err := embeddedWeb.ReadFile("web/dist/index.html")
	return err == nil
}

// spaFileSystem wraps http.FileSystem to serve index.html for SPA routes
type spaFileSystem struct {
	fs http.FileSystem
}

func (s spaFileSystem) Open(name string) (http.File, error) {
	f, err := s.fs.Open(name)
	if os.IsNotExist(err) {
		return s.fs.Open("index.html")
	}
	return f, err
}
