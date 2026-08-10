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

	// API routes (v1 placeholder)
	mux.Handle("/api/", http.NotFoundHandler())

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

