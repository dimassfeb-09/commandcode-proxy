package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── OpenAI request shapes ──

// toolFunc is one OpenAI function tool definition.
type toolFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// toolCall is one OpenAI message tool call.
type toolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function toolFunc `json:"function"`
}

// chatMessage is one OpenAI conversation message.
type chatMessage struct {
	Role       string      `json:"role"`
	Content    flexContent `json:"content"`
	ToolCalls  []toolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// flexContent accepts OpenAI's string|array content. Text parts are joined;
// image/audio parts are dropped (lean prefix, stable cache).
type flexContent struct{ text string }

// UnmarshalJSON implements string|array[{type,text,...}] content.
func (f *flexContent) UnmarshalJSON(data []byte) error {
	var str string
	if json.Unmarshal(data, &str) == nil {
		f.text = str
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	var buf strings.Builder
	for _, part := range parts {
		if part.Type == "" || part.Type == "text" {
			buf.WriteString(part.Text)
		}
	}
	f.text = buf.String()
	return nil
}

// functionDef is the inner schema of an OpenAI function tool.
type functionDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// toolDef is one OpenAI tools[] entry.
type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

// streamOptions mirrors OpenAI's stream_options object.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatRequest is the subset of CreateChatCompletionRequest the proxy honors.
// Fields without an upstream counterpart are accepted but ignored.
type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []chatMessage  `json:"messages"`
	Tools               []toolDef      `json:"tools,omitempty"`
	Stream              bool           `json:"stream"`
	MaxTokens           *int           `json:"max_tokens"`
	MaxCompletionTokens *int           `json:"max_completion_tokens"`
	Temperature         *float64       `json:"temperature"`
	ReasoningEffort     any            `json:"reasoning_effort"`
	ToolChoice          any            `json:"tool_choice"`
	User                string         `json:"user"`
	PromptCacheKey      string         `json:"prompt_cache_key"`
	StreamOptions       *streamOptions `json:"stream_options"`
}

// resolveMaxTokens prefers max_tokens, falls back to max_completion_tokens
// (new SDKs send only that; without the fallback every reply truncates).
func resolveMaxTokens(req chatRequest) int {
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		return *req.MaxTokens
	}
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		return *req.MaxCompletionTokens
	}
	return 1024
}

// sessionAffinity keys gateway session affinity for x-session-id
// (official prompt_cache_key, falling back to user). "" means omit.
func sessionAffinity(req chatRequest) string {
	if req.PromptCacheKey != "" {
		return req.PromptCacheKey
	}
	return req.User
}

// newRespID mints a unique per-request id (clients dedupe by id).
func newRespID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("chatcmpl-cc-%d", time.Now().UnixNano())
	}
	return "chatcmpl-cc-" + hex.EncodeToString(b)
}

// mapFinish maps gateway outcome to OpenAI finish_reason: tool_calls wins,
// then length, else stop. (Raw "length" surfaces as-is so clients continue
// instead of mistaking a truncation for a final answer.)
func mapFinish(numCalls int, raw string) string {
	if numCalls > 0 {
		return "tool_calls"
	}
	if raw == "length" || raw == "max_tokens" {
		return "length"
	}
	return "stop"
}

// openaiError builds the official OpenAI {error:{message,type,code}} shape.
func openaiError(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{
		"message": msg, "type": "invalid_request_error", "code": code}}
}

// usage accumulates gateway token counts across pause_turn continuations.
type usage struct {
	prompt     int
	completion int
	cached     int
	reasoning  int
}

// openaiUsage renders the official CompletionUsage shape:
// prompt_tokens_details.cached_tokens counts cache hits,
// completion_tokens_details.reasoning_tokens counts reasoning.
func openaiUsage(use usage) map[string]any {
	return map[string]any{
		"prompt_tokens": use.prompt, "completion_tokens": use.completion,
		"total_tokens":              use.prompt + use.completion,
		"prompt_tokens_details":     map[string]any{"cached_tokens": use.cached},
		"completion_tokens_details": map[string]any{"reasoning_tokens": use.reasoning},
	}
}
