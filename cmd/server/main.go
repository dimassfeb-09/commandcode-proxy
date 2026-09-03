package main

// Command commandcode-proxy: OpenAI-compatible chat proxy in front of the
// CommandCode gateway. Stdlib only, no dependencies.

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// main wires the routes and serves. Per-request Bearer keys keep tenants
// isolated; the local COMMANDCODE_API_KEY fallback is dev-only.
func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChat)
	mux.HandleFunc("/v1/models", handleModels)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","model":"` + upstreamModel + `"}`))
	})
	fmt.Printf("commandcode-proxy → %s default=%s :%s\n", baseURL(), upstreamModel, port)
	// ReadHeaderTimeout guards slowloris; IdleTimeout reaps idle keep-alives.
	// No ReadTimeout/WriteTimeout: legitimate SSE streams idle between tokens
	// and request bodies run large — those timeouts would kill valid requests.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
}
