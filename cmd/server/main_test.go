package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if u.prompt != 1000 || u.cached != 800 {
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
	if got := resolveMaxTokens(chatRequest{MaxTokens: p(50), MaxCompletionTokens: p(99)}); got != 50 {
		t.Fatalf("max_tokens must win, got %d", got)
	}
	if got := resolveMaxTokens(chatRequest{MaxCompletionTokens: p(200)}); got != 200 {
		t.Fatalf("max_completion_tokens fallback, got %d", got)
	}
	if got := resolveMaxTokens(chatRequest{}); got != 1024 {
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
	if got := sessionAffinity(chatRequest{PromptCacheKey: "pc", User: "u"}); got != "pc" {
		t.Fatalf("prompt_cache_key wins, got %q", got)
	}
	if got := sessionAffinity(chatRequest{User: "u"}); got != "u" {
		t.Fatalf("user fallback, got %q", got)
	}
	if got := sessionAffinity(chatRequest{}); got != "" {
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

const fakeNDJSON = `{"type":"text-delta","text":"apple"}` + "\n" +
	`{"type":"finish","finishReason":"end_turn","rawFinishReason":"end_turn","totalUsage":{"inputTokens":500,"outputTokens":2,"inputTokenDetails":{"cacheReadTokens":100,"cacheWriteTokens":50}}}` + "\n" +
	`{"type":"provider-metadata","providerMetadata":{"gateway":{"generationId":"gen_t","routing":{"resolvedProvider":"xiaomi"}}}}` + "\n"

func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "cli" {
			t.Errorf("missing cli user-agent")
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Write([]byte(fakeNDJSON))
	}))
	t.Setenv("COMMANDCODE_API_URL", s.URL)
	return s
}

func postChat(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer testkey")
	rr := httptest.NewRecorder()
	handleChat(rr, req)
	return rr
}

// ponytail: satu check end-to-end bentuk response (array content in, spec fields out).
func TestHandlerShapes(t *testing.T) {
	fakeUpstream(t)
	rr := postChat(t, `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"hi "},{"type":"image_url","image_url":{"url":"data:x"}}]}],"stream":false}`)
	if rr.Code != 200 {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var d map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	ch := d["choices"].([]any)[0].(map[string]any)
	msg := ch["message"].(map[string]any)
	if msg["content"] != "apple" {
		t.Fatalf("bad content: %v", msg)
	}
	if _, ok := msg["refusal"]; !ok {
		t.Fatal("message.refusal missing (spec required)")
	}
	if _, ok := ch["logprobs"]; !ok {
		t.Fatal("choice.logprobs missing (spec required)")
	}
	u := d["usage"].(map[string]any)
	if u["prompt_tokens_details"].(map[string]any)["cached_tokens"] != 100.0 {
		t.Fatalf("bad cached: %v", u)
	}
	if d["system_fingerprint"] != "xiaomi:gen_t" {
		t.Fatalf("bad fp: %v", d["system_fingerprint"])
	}
}

// ponytail: satu check SSE (role di delta pertama, usage chunk choices kosong).
func TestHandlerStreamShapes(t *testing.T) {
	fakeUpstream(t)
	rr := postChat(t, `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`)
	if rr.Code != 200 {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var firstRole string
	var useChunk map[string]any
	for _, line := range strings.Split(rr.Body.String(), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if line == "" || line == "[DONE]" || !strings.HasPrefix(line, "{") {
			continue
		}
		var c map[string]any
		if json.Unmarshal([]byte(line), &c) != nil {
			continue
		}
		chs, _ := c["choices"].([]any)
		if len(chs) > 0 {
			if d, _ := chs[0].(map[string]any)["delta"].(map[string]any); d != nil {
				if r, ok := d["role"].(string); ok && firstRole == "" {
					firstRole = r
				}
			}
		}
		if u, ok := c["usage"]; ok && u != nil {
			useChunk = c
		}
	}
	if firstRole != "assistant" {
		t.Fatalf("first delta must carry role assistant, got %q", firstRole)
	}
	if useChunk == nil {
		t.Fatal("missing usage chunk")
	}
	if len(useChunk["choices"].([]any)) != 0 {
		t.Fatal("usage chunk choices must be []")
	}
}
