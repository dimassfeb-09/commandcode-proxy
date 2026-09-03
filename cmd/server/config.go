package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// upstreamModel is the default model sent to the gateway when the client omits one.
	upstreamModel = "xiaomi/mimo-v2.5"
	// cliVersion is advertised to the gateway; requests without a CLI identity get rejected.
	cliVersion = "1.44.0"
	// defaultPort is used when PORT is unset.
	defaultPort = "8080"
	// maxAttempts mirrors the CLI retry loop (Jg=5): re-POST while rawFinishReason=="pause_turn".
	maxAttempts = 6
	// maxBodyBytes caps inbound request bodies. Full-history resends can be
	// large, but one giant client must not OOM the server for everyone else.
	maxBodyBytes = 32 << 20
)

// baseURL returns the gateway base URL, overridable via COMMANDCODE_API_URL for tests.
func baseURL() string {
	if env := os.Getenv("COMMANDCODE_API_URL"); env != "" {
		return strings.TrimSuffix(env, "/")
	}
	return "https://api.commandcode.ai"
}

// apiKey returns the local-dev fallback key from env or ~/.commandcode/auth.json.
// Deployments must NOT set this; the per-request Bearer header is required instead.
func apiKey() string {
	for _, name := range []string{"COMMANDCODE_API_KEY", "COMMAND_CODE_API_KEY"} {
		if val := os.Getenv(name); val != "" {
			return strings.TrimSpace(val)
		}
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".commandcode", "auth.json"))
	if err != nil {
		return ""
	}
	var auth struct {
		APIKey string `json:"apiKey"`
	}
	if json.Unmarshal(data, &auth) != nil {
		return ""
	}
	return auth.APIKey
}

// upstreamClient is shared by all request goroutines (safe + connection reuse).
// ResponseHeaderTimeout fails fast on a stalled gateway instead of hanging a
// goroutine forever. No total timeout: legitimate SSE streams run for minutes.
var upstreamClient = &http.Client{Transport: func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 2 * time.Minute
	return transport
}()}
