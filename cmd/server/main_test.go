package main

import "testing"

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
