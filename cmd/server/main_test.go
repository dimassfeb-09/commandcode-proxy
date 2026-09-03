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

// ponytail: satu check untuk parsing cacheRead upstream → cached_tokens.
func TestCacheReadParsing(t *testing.T) {
	ndjson := strings.NewReader(
		`{"type":"text-delta","text":"hi"}` + "\n" +
			`{"type":"finish","finishReason":"end_turn","rawFinishReason":"end_turn","totalUsage":{"inputTokens":1000,"outputTokens":10,"inputTokenDetails":{"cacheReadTokens":800,"cacheWriteTokens":200}}}` + "\n")
	_, u, raw, _ := collect(ndjson)
	if raw != "end_turn" {
		t.Fatalf("bad raw: %q", raw)
	}
	if u.inN != 1000 || u.cached != 800 {
		t.Fatalf("bad usage: %+v", u)
	}
	out := openaiUsage(u)
	d := out["prompt_tokens_details"].(map[string]any)
	if d["cached_tokens"] != 800 {
		t.Fatalf("bad cached_tokens: %v", out)
	}
}
