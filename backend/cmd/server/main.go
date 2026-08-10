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
	"github.com/happySpartan/azuredevops-permissions-visualiser/backend/store"
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

	// Open (or create) the local SQLite store.
	if err := ensureDataDir(); err != nil {
		log.Fatalf("store: create data dir: %v", err)
	}
	st, err := store.Open(store.DefaultDBPath())
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	// API routes (v1)
	mux.Handle("/api/", apiRoutes(st))

	// Serve embedded frontend for all non-API routes
	sub, err := fs.Sub(backend.WebFS, "web/dist")
	if err != nil {
		mux.Handle("/", http.NotFoundHandler())
	} else {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}

	addr := bind + ":" + port
	log.Printf("Listening on http://%s (data: %s)", addr, store.DefaultDBPath())

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
}

// apiRoutes returns the v1 JSON API handler.
func apiRoutes(st *store.Store) http.Handler {
	org := os.Getenv("AZDO_ORG")
	mux := http.NewServeMux()

	// reusableClient builds an azdo client from the configured org, or nil.
	reusableClient := func() (*azdo.Client, error) {
		if org == "" {
			return nil, errNoOrg
		}
		return azdo.NewClient(org)
	}

	mux.HandleFunc("/api/run/current", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, err := st.LatestRunID(r.Context())
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		if id == 0 {
			json.NewEncoder(w).Encode(map[string]any{"run": nil})
			return
		}
		run, err := st.RunByID(r.Context(), id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"run": run})
	})

	mux.HandleFunc("/api/run/collect", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		client, err := reusableClient()
		if err != nil {
			httpError(w, http.StatusServiceUnavailable, err)
			return
		}
		if err := requireAzAuth(r); err != nil {
			httpError(w, http.StatusServiceUnavailable, err)
			return
		}
		collector := newCollector(client, st)
		res, err := collector.Collect(r.Context(), org)
		if err != nil {
			httpError(w, http.StatusBadGateway, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"runID":  res.RunID,
			"counts": res.Counts,
		})
	})

	mux.HandleFunc("/api/run/delete", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := st.DeleteAll(r.Context()); err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	})

	return mux
}

func httpError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
