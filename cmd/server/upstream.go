package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// reOverflow matches the gateway's "prompt too long" signals, mirroring the
// CLI's classify regex. Matched responses become 400 context_length_exceeded.
var reOverflow = regexp.MustCompile(`(?i)prompt is too long|context.{0,20}(length|window)|maximum.{0,20}tokens|too many tokens|exceeds.*context`)

// isOverflow reports whether an upstream rejection means the context window
// is full. OpenAI clients match HTTP 400 + context_length_exceeded to
// compact/retry on their own, so this mapping keeps them working.
func isOverflow(status int, body []byte) bool {
	if status == http.StatusRequestEntityTooLarge {
		return true
	}
	return reOverflow.Match(body)
}

// doUpstream POSTs one wire body to /alpha/generate. The caller's key travels
// as the gateway Bearer token; session pins gateway KV-cache affinity.
func doUpstream(r *http.Request, key, session string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), "POST", baseURL()+"/alpha/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "cli") // required: without this → 403 Cloudflare 1010
	req.Header.Set("x-command-code-version", cliVersion)
	if session != "" {
		req.Header.Set("x-session-id", session)
	}
	return upstreamClient.Do(req)
}

// pumpStream pumps ONE upstream attempt, calling onText per text-delta.
// It returns the raw finish reason ("pause_turn" means call again),
// an error message for error events, accumulated usage, the backend
// fingerprint, and any structured tool calls.
func pumpStream(body io.Reader, onText func(string)) (raw, errMsg string, prompt, completion, cached, reasoning int, fingerprint string, calls []tcOut) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	seq := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event wireEvent
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		switch event.Type {
		case "text-delta":
			if event.Text != "" {
				onText(event.Text)
			}
		case "tool-call":
			seq++
			id := event.ToolCallID
			if id == "" {
				id = fmt.Sprintf("call_%d", seq)
			}
			name := event.ToolName
			if name == "" {
				name = "unknown"
			}
			calls = append(calls, newTCCall(id, name, wireInputArgs(event.Input)))
		case "finish":
			if event.TotalUsage != nil {
				prompt, completion, reasoning = event.TotalUsage.InputTokens, event.TotalUsage.OutputTokens, wireReason(event.TotalUsage)
				if event.TotalUsage.InputTokenDetails != nil {
					cached = event.TotalUsage.InputTokenDetails.CacheReadTokens
				}
			}
			raw = event.RawFinishReason
		case "provider-metadata":
			if fp := wireFingerprint(event.ProviderMetadata); fp != "" {
				fingerprint = fp
			}
		case "error":
			errMsg = event.Message
			if errMsg == "" {
				errMsg = line
			}
			return
		}
	}
	return
}

// collect is pumpStream's non-streaming twin: it buffers the whole reply text
// instead of emitting deltas. Same return contract.
func collect(r io.Reader) (text string, use usage, raw, fingerprint string, calls []tcOut) {
	var buf strings.Builder
	seq := 0
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event wireEvent
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "text-delta" {
			buf.WriteString(event.Text)
		} else if event.Type == "tool-call" {
			seq++
			id := event.ToolCallID
			if id == "" {
				id = fmt.Sprintf("call_%d", seq)
			}
			name := event.ToolName
			if name == "" {
				name = "unknown"
			}
			calls = append(calls, newTCCall(id, name, wireInputArgs(event.Input)))
		} else if event.Type == "finish" {
			if event.TotalUsage != nil {
				use = usage{prompt: event.TotalUsage.InputTokens, completion: event.TotalUsage.OutputTokens, reasoning: wireReason(event.TotalUsage)}
				if event.TotalUsage.InputTokenDetails != nil {
					use.cached = event.TotalUsage.InputTokenDetails.CacheReadTokens
				}
			}
			raw = event.RawFinishReason
		} else if event.Type == "provider-metadata" {
			if fp := wireFingerprint(event.ProviderMetadata); fp != "" {
				fingerprint = fp
			}
		}
	}
	return buf.String(), use, raw, fingerprint, calls
}
