package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// wireRequest bundles everything both chat handlers need from one OpenAI request.
type wireRequest struct {
	key     string
	respID  string
	session string
	model   string
	body    []byte
}

// handleChat serves POST /v1/chat/completions, dispatching to the buffered
// or SSE handler based on the stream flag.
func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	raw, _ := io.ReadAll(r.Body)
	var req chatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	key, ok := requireKey(w, r)
	if !ok {
		return
	}
	wire := wireRequest{
		key:     key,
		respID:  newRespID(),
		session: sessionAffinity(req),
		model:   req.Model,
	}
	if wire.model == "" {
		wire.model = upstreamModel
	}
	wire.body = buildWireBody(req, wire.model)
	if !req.Stream {
		serveChat(w, r, wire)
		return
	}
	serveStream(w, r, wire)
}

// buildWireBody renders the gateway /alpha/generate body for a request.
// Faithful to the CLI: fixed agent config, undefined threadId, and only the
// params the gateway understands (model/messages/tools/system/max_tokens
// plus temperature and reasoning_effort when set).
func buildWireBody(req chatRequest, model string) []byte {
	system, msgs := toWireMessages(req.Messages)
	if msgs == nil {
		msgs = []any{}
	}
	params := map[string]any{
		"model": model, "messages": msgs, "tools": toWireTools(req.Tools),
		"max_tokens": resolveMaxTokens(req), "stream": true,
	}
	// tool_choice:"none" means send no tools (the only value with an upstream
	// counterpart; auto/required/named have none, so they are ignored).
	if choice, ok := req.ToolChoice.(string); ok && strings.EqualFold(choice, "none") {
		params["tools"] = []any{}
	}
	if system != "" {
		params["system"] = system
	}
	if req.Temperature != nil {
		params["temperature"] = *req.Temperature
	}
	if req.ReasoningEffort != nil {
		params["reasoning_effort"] = req.ReasoningEffort
	}
	wire := map[string]any{
		"config": map[string]any{"workingDir": "/tmp", "date": time.Now().Format("2006-01-02"), "environment": "linux", "structure": []string{}, "isGitRepo": false, "currentBranch": "", "mainBranch": "", "gitStatus": "", "recentCommits": []string{}},
		"memory": nil, "taste": nil, "skills": nil,
		"permissionMode": "standard", "mode": "agent", "params": params,
	}
	data, _ := json.Marshal(wire)
	return data
}

// overflowError is the OpenAI error clients match to compact/retry on their own.
func overflowError() map[string]any {
	return openaiError("context_length_exceeded",
		"This model's maximum context length was exceeded. Compact the conversation and retry.")
}

// serveChat handles non-streaming requests: it re-POSTs the SAME body while
// the gateway pauses (raw=="pause_turn", up to maxAttempts, like the CLI),
// accumulating text and usage, then answers with one chat.completion.
func serveChat(w http.ResponseWriter, r *http.Request, wire wireRequest) {
	var text strings.Builder
	var use usage
	var calls []tcOut
	var finalRaw, fingerprint string
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := doUpstream(r, wire.key, wire.session, wire.body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if resp.StatusCode != 200 {
			upstreamBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			w.Header().Set("Content-Type", "application/json")
			if isOverflow(resp.StatusCode, upstreamBody) {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(overflowError())
				return
			}
			w.WriteHeader(resp.StatusCode)
			w.Write(upstreamBody)
			return
		}
		chunk, delta, raw, fp, toolCalls := collect(resp.Body)
		resp.Body.Close()
		text.WriteString(chunk)
		use = usage{
			prompt:     use.prompt + delta.prompt,
			completion: use.completion + delta.completion,
			cached:     use.cached + delta.cached,
			reasoning:  use.reasoning + delta.reasoning,
		}
		calls = append(calls, toolCalls...)
		finalRaw, fingerprint = raw, fp
		if raw != "pause_turn" {
			break
		}
	}
	reply := text.String()
	if len(calls) == 0 {
		if clean, recovered := extractXMLToolCalls(reply); len(recovered) > 0 {
			reply, calls = clean, recovered
		}
	}
	msg := map[string]any{"role": "assistant", "content": reply, "refusal": nil}
	finish := mapFinish(len(calls), finalRaw)
	if finish == "tool_calls" {
		msg["tool_calls"] = calls
	}
	out := map[string]any{
		"id": wire.respID, "object": "chat.completion", "created": time.Now().Unix(), "model": wire.model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish, "logprobs": nil}},
		"usage":   openaiUsage(use),
	}
	if fingerprint != "" {
		out["system_fingerprint"] = fingerprint
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
