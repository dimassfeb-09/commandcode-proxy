package main

import (
	"strings"
	"testing"
)

// ponytail: satu check untuk parser XML fallback.
func TestExtractXMLToolCalls(t *testing.T) {
	in := `Let me explore! <tool_call> <function=read> <parameter=filePath>/Users/dimassfeb/Project/muse-spark-zen</parameter> </function> </tool_call><tool_call> <function=glob> <parameter=pattern>/package.json</parameter> <parameter=path>/Users/dimassfeb/Project/muse-spark-zen</parameter> </function> </tool_call>`
	clean, calls := extractXMLToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("want 2 calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "read" || calls[1].Function.Name != "glob" {
		t.Fatalf("bad names: %+v", calls)
	}
	if clean != "Let me explore!" {
		t.Fatalf("bad clean text: %q", clean)
	}
	if len(calls[0].ID) == 0 || calls[0].ID == calls[1].ID {
		t.Fatalf("bad ids: %+v", calls)
	}
}

// ponytail: satu check untuk parsing cacheRead upstream → cached_tokens + fingerprint.
func TestCacheReadParsing(t *testing.T) {
	ndjson := strings.NewReader(
		`{"type":"text-delta","text":"hi"}` + "\n" +
			`{"type":"finish","finishReason":"end_turn","rawFinishReason":"end_turn","totalUsage":{"inputTokens":1000,"outputTokens":10,"inputTokenDetails":{"cacheReadTokens":800,"cacheWriteTokens":200}}}` + "\n" +
			`{"type":"provider-metadata","providerMetadata":{"gateway":{"generationId":"gen_abc","routing":{"resolvedProvider":"xiaomi"}}}}` + "\n")
	_, u, raw, fp, _ := collect(ndjson)
	if raw != "end_turn" {
		t.Fatalf("bad raw: %q", raw)
	}
	if u.inN != 1000 || u.cached != 800 {
		t.Fatalf("bad usage: %+v", u)
	}
	if fp != "xiaomi:gen_abc" {
		t.Fatalf("bad fp: %q", fp)
	}
	out := openaiUsage(u)
	d := out["prompt_tokens_details"].(map[string]any)
	if d["cached_tokens"] != 800 {
		t.Fatalf("bad cached_tokens: %v", out)
	}
}

// ponytail: satu check untuk mapping request OpenAI → wire (P0+P1).
func TestRequestMapping(t *testing.T) {
	p := func(v int) *int { return &v }
	if got := resolveMaxTokens(chatReq{MaxTokens: p(50), MaxComplTok: p(99)}); got != 50 {
		t.Fatalf("max_tokens must win, got %d", got)
	}
	if got := resolveMaxTokens(chatReq{MaxComplTok: p(200)}); got != 200 {
		t.Fatalf("max_completion_tokens fallback, got %d", got)
	}
	if got := resolveMaxTokens(chatReq{}); got != 1024 {
		t.Fatalf("default 1024, got %d", got)
	}
	if got := mapFinish(0, "length"); got != "length" {
		t.Fatalf("length must surface, got %q", got)
	}
	if got := mapFinish(2, "length"); got != "tool_calls" {
		t.Fatalf("tool_calls wins, got %q", got)
	}
	if got := mapFinish(0, "end_turn"); got != "stop" {
		t.Fatalf("default stop, got %q", got)
	}
	if got := sessionAffinity(chatReq{PromptCacheKey: "pc", User: "u"}); got != "pc" {
		t.Fatalf("prompt_cache_key wins, got %q", got)
	}
	if got := sessionAffinity(chatReq{User: "u"}); got != "u" {
		t.Fatalf("user fallback, got %q", got)
	}
	if got := sessionAffinity(chatReq{}); got != "" {
		t.Fatalf("empty affinity, got %q", got)
	}
	if a, b := newRespID(), newRespID(); a == b {
		t.Fatalf("ids must be unique: %q", a)
	}
}

// ponytail: satu check untuk overflow mapping (window penuh → 400 OpenAI).
func TestOverflowMapping(t *testing.T) {
	if !isOverflow(400, []byte(`{"error":"prompt is too long for context window"}`)) {
		t.Fatal("prompt-too-long must map")
	}
	if !isOverflow(413, []byte(`anything`)) {
		t.Fatal("413 must map")
	}
	if isOverflow(403, []byte(`{"success":false,"error":{"code":"FORBIDDEN","message":"Model/provider not recognized"}}`)) {
		t.Fatal("gateway 403 must passthrough, not map")
	}
	if isOverflow(400, []byte(`{"error":"invalid json"}`)) {
		t.Fatal("generic 400 must passthrough, not map")
	}
	e := openaiError("context_length_exceeded", "m")
	inner := e["error"].(map[string]any)
	if inner["code"] != "context_length_exceeded" || inner["type"] != "invalid_request_error" {
		t.Fatalf("bad shape: %v", e)
	}
}
