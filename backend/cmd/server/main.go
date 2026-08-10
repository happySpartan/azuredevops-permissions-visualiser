package main

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/happySpartan/azuredevops-permissions-visualiser/backend"
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/azdo"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	bind := os.Getenv("BIND_ADDR")
	if bind == "" {
		bind = "127.0.0.1"
	}

	mux := http.NewServeMux()

	// Health / status
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"app":    "azuredevops-permissions-visualiser",
		})
	})

	// API routes (v1)
	mux.Handle("/api/", apiRoutes())

	// Serve embedded frontend for all non-API routes
	sub, err := fs.Sub(backend.WebFS, "web/dist")
	if err != nil {
		// Fallback: no frontend embedded
		mux.Handle("/", http.NotFoundHandler())
	} else {
		fileServer := http.FileServer(http.FS(sub))
		mux.Handle("/", fileServer)
	}

	// Spinner
	addr := bind + ":" + port
	log.Printf("Listening on http://%s", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
}

// apiRoutes returns the v1 JSON API handler. Organization is provided via the
// ORG environment variable (Azure CLI must be authenticated).
func apiRoutes() http.Handler {
	org := os.Getenv("AZDO_ORG")
	var client *azdo.Client
	if org != "" {
		var err error
		client, err = azdo.NewClient(org)
		if err != nil {
			log.Printf("azdo: unable to create client for org %q: %v", org, err)
			client = nil
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if client == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error":   "no organization configured",
				"details": "set AZDO_ORG and authenticate with Azure CLI",
			})
			return
		}
		projects, err := client.Projects(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"value": projects})
	})

	return mux
}
