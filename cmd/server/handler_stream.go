package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// serveStream handles streaming requests: the SSE channel stays open across
// pause_turn continuations and [DONE] is sent exactly once at the end.
func serveStream(w http.ResponseWriter, r *http.Request, wire wireRequest) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)
	var use usage
	first := true // the first content delta carries role:assistant per spec
	emit := func(delta string, finish any) {
		patch := map[string]any{"content": delta}
		if first && delta != "" {
			patch["role"] = "assistant"
			first = false
		}
		chunk := map[string]any{"id": wire.respID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": wire.model,
			"choices": []any{map[string]any{"index": 0, "delta": patch, "finish_reason": finish, "logprobs": nil}}}
		payload, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	// emitToolCalls replays buffered tool calls OpenAI-style: one chunk with
	// id+name, then one with the arguments, so clients can reassemble by index.
	emitToolCalls := func(calls []tcOut, base int) {
		for offset, call := range calls {
			index := base + offset
			for _, patch := range []any{
				map[string]any{"tool_calls": []any{map[string]any{"index": index, "id": call.ID, "type": "function", "function": map[string]any{"name": call.Function.Name, "arguments": ""}}}},
				map[string]any{"tool_calls": []any{map[string]any{"index": index, "function": map[string]any{"arguments": call.Function.Arguments}}}},
			} {
				payload, _ := json.Marshal(map[string]any{"id": wire.respID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": wire.model,
					"choices": []any{map[string]any{"index": 0, "delta": patch, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\n", payload)
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}
	streamErr := ""
	var failStatus int
	var failBody []byte
	var fullText strings.Builder
	var calls []tcOut
	var finalRaw, fingerprint string
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := doUpstream(r, wire.key, wire.session, wire.body)
		if err != nil {
			streamErr = err.Error()
			break
		}
		if resp.StatusCode != 200 {
			upstreamBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			streamErr = fmt.Sprintf("upstream %d: %s", resp.StatusCode, string(upstreamBody))
			failStatus, failBody = resp.StatusCode, upstreamBody
			break
		}
		raw, eventErr, prompt, completion, cached, reasoning, fp, toolCalls := pumpStream(resp.Body, func(text string) {
			fullText.WriteString(text)
			emit(text, nil)
		})
		resp.Body.Close()
		use = usage{
			prompt:     use.prompt + prompt,
			completion: use.completion + completion,
			cached:     use.cached + cached,
			reasoning:  use.reasoning + reasoning,
		}
		calls = append(calls, toolCalls...)
		finalRaw, fingerprint = raw, fp
		if eventErr != "" {
			streamErr = eventErr
			break
		}
		if raw != "pause_turn" {
			break
		}
	}
	if streamErr != "" {
		emit("", "error")
		if isOverflow(failStatus, failBody) {
			payload, _ := json.Marshal(overflowError())
			fmt.Fprintf(w, "data: %s\n\n", payload)
		} else {
			fmt.Fprintf(w, "data: {\"error\":%q}\n\n", streamErr)
		}
	} else {
		if len(calls) == 0 {
			// Tag text already streamed; what matters is the client gets tool_calls.
			if _, recovered := extractXMLToolCalls(fullText.String()); len(recovered) > 0 {
				calls = recovered
			}
		}
		finish := mapFinish(len(calls), finalRaw)
		if finish == "tool_calls" {
			emitToolCalls(calls, 0)
		}
		finishChunk := map[string]any{"id": wire.respID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": wire.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": ""}, "finish_reason": finish, "logprobs": nil}}}
		usageChunk := map[string]any{"id": wire.respID, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": wire.model,
			"choices": []any{}, "usage": openaiUsage(use)}
		if fingerprint != "" {
			finishChunk["system_fingerprint"], usageChunk["system_fingerprint"] = fingerprint, fingerprint
		}
		// Finish chunk first, then the spec-compliant usage chunk with choices=[].
		for _, chunk := range []map[string]any{finishChunk, usageChunk} {
			payload, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
}
