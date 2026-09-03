package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// tcOut is one OpenAI tool_call for responses.
type tcOut struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function toolFunc `json:"function"`
}

// newTCCall builds one function tool call with the given id, name and JSON arguments.
func newTCCall(id, name, args string) tcOut {
	return tcOut{ID: id, Type: "function", Function: toolFunc{Name: name, Arguments: args}}
}

var (
	reXMLCall  = regexp.MustCompile(`(?s)<tool_call>\s*<function=([^>]+)>(.*?)</function>\s*</tool_call>`)
	reXMLParam = regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)
)

// extractXMLToolCalls recovers tool calls from models that write
// <tool_call><function=NAME><parameter=K>V</parameter>...</function></tool_call>
// as plain TEXT. It returns the cleaned text plus the calls (IDs call_1, ...).
// Simple regex recovery, not a real XML parser.
func extractXMLToolCalls(text string) (clean string, calls []tcOut) {
	cleaned := reXMLCall.ReplaceAllStringFunc(text, func(block string) string {
		match := reXMLCall.FindStringSubmatch(block)
		if match == nil {
			return block
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			return block
		}
		args := map[string]string{}
		for _, param := range reXMLParam.FindAllStringSubmatch(match[2], -1) {
			args[strings.TrimSpace(param[1])] = strings.TrimSpace(param[2])
		}
		encoded, _ := json.Marshal(args)
		calls = append(calls, newTCCall(fmt.Sprintf("call_%d", len(calls)+1), name, string(encoded)))
		return ""
	})
	return strings.TrimSpace(cleaned), calls
}
