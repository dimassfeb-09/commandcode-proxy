package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// requestKey returns the caller's CommandCode key: the Bearer token they sent,
// falling back to the local-dev key only when the header is absent.
func requestKey(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		if key := strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")); key != "" {
			return key
		}
	}
	return apiKey()
}

// requireKey writes 401 when no key is available and reports whether to proceed.
func requireKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := requestKey(r)
	if key == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"message": "missing api key: send your CommandCode key as Authorization: Bearer <key>",
			"type":    "invalid_request_error", "code": "invalid_api_key"}})
		return "", false
	}
	return key, true
}
