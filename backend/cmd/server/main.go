package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	mux.HandleFunc("/api/explorer/resources", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		resources, err := st.ResourcesByRun(r.Context(), runID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"projects": resources})
	})

	mux.HandleFunc("/api/explorer/subjects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		limit, err := queryInt(r, "limit", 50)
		if err != nil {
			httpError(w, http.StatusBadRequest, err)
			return
		}
		offset, err := queryInt(r, "offset", 0)
		if err != nil {
			httpError(w, http.StatusBadRequest, err)
			return
		}
		page, err := st.SubjectsByRun(r.Context(), runID, store.SubjectQuery{
			Search: r.URL.Query().Get("search"),
			Kind:   r.URL.Query().Get("kind"),
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(page)
	})

	mux.HandleFunc("/api/explorer/subjects/permissions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		descriptor := r.URL.Query().Get("descriptor")
		if descriptor == "" {
			httpError(w, http.StatusBadRequest, errors.New("descriptor is required"))
			return
		}
		detail, err := st.SubjectPermissionsByRun(r.Context(), runID, descriptor)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(detail)
	})

	mux.HandleFunc("/api/explorer/subjects/explanation", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		descriptor := r.URL.Query().Get("descriptor")
		token := r.URL.Query().Get("token")
		bit, err := queryInt(r, "bit", 0)
		if descriptor == "" || token == "" || err != nil || bit <= 0 {
			httpError(w, http.StatusBadRequest, errors.New("descriptor, token, and positive bit are required"))
			return
		}
		explanation, err := st.PermissionExplanationByRun(r.Context(), runID, descriptor, token, int64(bit))
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(explanation)
	})

	mux.HandleFunc("/api/explorer/resources/permissions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		token := r.URL.Query().Get("token")
		if token == "" {
			httpError(w, http.StatusBadRequest, errors.New("token is required"))
			return
		}
		detail, err := st.ResourcePermissionsByRun(r.Context(), runID, token)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(detail)
	})

	mux.HandleFunc("/api/explorer/matrix", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		projectID := r.URL.Query().Get("projectId")
		bit, err := queryInt(r, "bit", 0)
		if projectID == "" || err != nil || bit <= 0 {
			httpError(w, http.StatusBadRequest, errors.New("projectId and positive bit are required"))
			return
		}
		matrix, err := st.PermissionMatrixByRun(r.Context(), runID, projectID, int64(bit))
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
		json.NewEncoder(w).Encode(matrix)
	})

	// CSV export endpoints
	mux.HandleFunc("/api/run/export/effective-permissions", func(w http.ResponseWriter, r *http.Request) {
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=effective-permissions.csv")
		if err := st.ExportEffectivePermissionsCSV(r.Context(), runID, w); err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
	})

	mux.HandleFunc("/api/run/export/assignments", func(w http.ResponseWriter, r *http.Request) {
		runID, ok := latestRunID(w, r, st)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=assignments.csv")
		if err := st.ExportSubjectAssignmentsCSV(r.Context(), runID, w); err != nil {
			httpError(w, http.StatusInternalServerError, err)
			return
		}
	})

	return mux
}

func latestRunID(w http.ResponseWriter, r *http.Request, st *store.Store) (int64, bool) {
	id, err := st.LatestRunID(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return 0, false
	}
	if id == 0 {
		httpError(w, http.StatusNotFound, store.ErrNotFound)
		return 0, false
	}
	return id, true
}

func queryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("invalid " + name)
	}
	return value, nil
}

func httpError(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
