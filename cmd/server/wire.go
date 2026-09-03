package main

import (
	"encoding/json"
	"strings"
)

// ── OpenAI → gateway wire mapping (mirrors the CLI's toWire* functions) ──

// toWireTools converts OpenAI tools[] to the gateway's tool schema.
// Non-function tools and nameless entries are dropped.
func toWireTools(tools []toolDef) []any {
	out := []any{}
	for _, tool := range tools {
		if tool.Type != "" && tool.Type != "function" {
			continue
		}
		if tool.Function.Name == "" {
			continue
		}
		out = append(out, map[string]any{
			"name": tool.Function.Name, "description": tool.Function.Description, "input_schema": tool.Function.Parameters,
		})
	}
	return out
}

// toWireMessages converts OpenAI messages to gateway wire messages.
// System/developer messages merge into one system prompt; the rest map by
// role (assistant tool calls and tool results included). Returns the system
// prompt and the wire message list.
func toWireMessages(msgs []chatMessage) (system string, out []any) {
	var systems []string
	out = []any{}
	nameByID := map[string]string{}
	for _, msg := range msgs {
		switch msg.Role {
		case "system", "developer":
			systems = append(systems, msg.Content.text)
		case "assistant":
			var content []any
			if msg.Content.text != "" {
				content = append(content, map[string]any{"type": "text", "text": msg.Content.text})
			}
			for _, tc := range msg.ToolCalls {
				var input any = map[string]any{}
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				nameByID[tc.ID] = tc.Function.Name
				content = append(content, map[string]any{
					"type": "tool-call", "toolCallId": tc.ID, "toolName": tc.Function.Name, "input": input,
				})
			}
			if content == nil {
				content = []any{}
			}
			out = append(out, map[string]any{"role": "assistant", "content": content})
		case "tool":
			name := nameByID[msg.ToolCallID]
			if name == "" {
				name = "unknown" // same as CLI
			}
			out = append(out, map[string]any{"role": "tool", "content": []any{map[string]any{
				"type": "tool-result", "toolCallId": msg.ToolCallID, "toolName": name,
				"output": map[string]any{"type": "text", "value": msg.Content.text},
			}}})
		default:
			out = append(out, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": msg.Content.text}}})
		}
	}
	return strings.Join(systems, "\n"), out
}

// ── Gateway NDJSON event shapes ──

// wireEvent is one NDJSON line from /alpha/generate.
type wireEvent struct {
	Type             string         `json:"type"`
	Text             string         `json:"text"`
	FinishReason     string         `json:"finishReason"`
	RawFinishReason  string         `json:"rawFinishReason"`
	ToolName         string         `json:"toolName"`
	ToolCallID       string         `json:"toolCallId"`
	Input            any            `json:"input"`
	TotalUsage       *wireUsage     `json:"totalUsage"`
	ProviderMetadata map[string]any `json:"providerMetadata"`
	Message          string         `json:"message"`
}

// wireUsage is the token count inside a finish event.
type wireUsage struct {
	InputTokens       int `json:"inputTokens"`
	OutputTokens      int `json:"outputTokens"`
	ReasoningTokens   int `json:"reasoningTokens"`
	InputTokenDetails *struct {
		CacheReadTokens  int `json:"cacheReadTokens"`
		CacheWriteTokens int `json:"cacheWriteTokens"`
	} `json:"inputTokenDetails"`
	OutputTokenDetails *struct {
		ReasoningTokens int `json:"reasoningTokens"`
	} `json:"outputTokenDetails"`
}

// wireReason extracts reasoning tokens from a finish event,
// preferring the top-level field with the details object as fallback.
func wireReason(total *wireUsage) int {
	if total.ReasoningTokens > 0 {
		return total.ReasoningTokens
	}
	if total.OutputTokenDetails != nil {
		return total.OutputTokenDetails.ReasoningTokens
	}
	return 0
}

// wireFingerprint renders provider+generationId from a provider-metadata
// event as the OpenAI system_fingerprint (clients detect backend switches
// as cache invalidation).
func wireFingerprint(meta map[string]any) string {
	gateway, _ := meta["gateway"].(map[string]any)
	if gateway == nil {
		return ""
	}
	gen, _ := gateway["generationId"].(string)
	prov, _ := gateway["resolvedProvider"].(string)
	if prov == "" {
		if routing, _ := gateway["routing"].(map[string]any); routing != nil {
			prov, _ = routing["resolvedProvider"].(string)
		}
	}
	switch {
	case prov != "" && gen != "":
		return prov + ":" + gen
	case gen != "":
		return gen
	default:
		return prov
	}
}

// wireInputArgs renders a tool-call event input (object or pre-stringified)
// as the JSON string OpenAI clients expect in arguments.
func wireInputArgs(input any) string {
	if input == nil {
		return "{}"
	}
	if str, ok := input.(string); ok {
		if str == "" {
			return "{}"
		}
		return str
	}
	raw, _ := json.Marshal(input)
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}
