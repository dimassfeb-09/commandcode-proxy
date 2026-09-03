package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ponytail: stdlib only, no deps. Fixed model, no gateway, no Responses API.

const (
	upstreamModel = "xiaomi/mimo-v2.5"
	cliVersion    = "1.39.2"
	defaultPort   = "8080"
	// ponytail: mirror CLI Jg=5 — re-POST while rawFinishReason=="pause_turn".
	maxAttempts = 6
)

func baseURL() string {
	if u := os.Getenv("COMMANDCODE_API_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return "https://api.commandcode.ai"
}

func apiKey() string {
	for _, k := range []string{"COMMANDCODE_API_KEY", "COMMAND_CODE_API_KEY"} {
		if v := os.Getenv(k); v != "" {
			return strings.TrimSpace(v)
		}
	}
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".commandcode", "auth.json"))
	if err != nil {
		return ""
	}
	var a struct {
		APIKey string `json:"apiKey"`
	}
	if json.Unmarshal(b, &a) != nil {
		return ""
	}
	return a.APIKey
}

// ── OpenAI in ──

type tcFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type tcCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function tcFunc `json:"function"`
}

type chatMsg struct {
	Role       string   `json:"role"`
	Content    string   `json:"content"`
	ToolCalls  []tcCall `json:"tool_calls,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
}

type fnDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type toolDef struct {
	Type     string `json:"type"`
	Function fnDef  `json:"function"`
}

type chatReq struct {
	Model       string    `json:"model"`
	Messages    []chatMsg `json:"messages"`
	Tools       []toolDef `json:"tools,omitempty"`
	Stream      bool      `json:"stream"`
	MaxTokens   *int      `json:"max_tokens"`
	Temperature *float64  `json:"temperature"`
}

// ── Wire out ──

func toWireTools(ts []toolDef) []any {
	out := []any{}
	for _, t := range ts {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		if t.Function.Name == "" {
			continue
		}
		out = append(out, map[string]any{
			"name": t.Function.Name, "description": t.Function.Description, "input_schema": t.Function.Parameters,
		})
	}
	return out
}

func toWireMessages(ms []chatMsg) (system string, out []any) {
	var sys []string
	out = []any{}
	nameByID := map[string]string{}
	for _, m := range ms {
		switch m.Role {
		case "system", "developer":
			sys = append(sys, m.Content)
		case "assistant":
			var c []any
			if m.Content != "" {
				c = append(c, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any = map[string]any{}
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				}
				nameByID[tc.ID] = tc.Function.Name
				c = append(c, map[string]any{
					"type": "tool-call", "toolCallId": tc.ID, "toolName": tc.Function.Name, "input": input,
				})
			}
			if c == nil {
				c = []any{}
			}
			out = append(out, map[string]any{"role": "assistant", "content": c})
		case "tool":
			name := nameByID[m.ToolCallID]
			if name == "" {
				name = "unknown" // sama kayak CLI
			}
			out = append(out, map[string]any{"role": "tool", "content": []any{map[string]any{
				"type": "tool-result", "toolCallId": m.ToolCallID, "toolName": name,
				"output": map[string]any{"type": "text", "value": m.Content},
			}}})
		default:
			out = append(out, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": m.Content}}})
		}
	}
	return strings.Join(sys, "\n"), out
}

// ── Upstream NDJSON ──

type wireEvent struct {
	Type            string `json:"type"`
	Text            string `json:"text"`
	FinishReason    string `json:"finishReason"`
	RawFinishReason string `json:"rawFinishReason"`
	ToolName        string `json:"toolName"`
	ToolCallID      string `json:"toolCallId"`
	Input           any    `json:"input"`
	TotalUsage      *struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"totalUsage"`
	Message string `json:"message"`
}

// tcOut = OpenAI tool_call untuk response.
type tcOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function tcFunc `json:"function"`
}

func newTCCall(id, name, args string) tcOut {
	return tcOut{ID: id, Type: "function", Function: tcFunc{Name: name, Arguments: args}}
}

// wireInputArgs: event input (objek/map) → string JSON arguments.
func wireInputArgs(v any) string {
	if v == nil {
		return "{}"
	}
	if s, ok := v.(string); ok {
		if s == "" {
			return "{}"
		}
		return s
	}
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return "{}"
	}
	return string(b)
}

var (
	reXMLCall  = regexp.MustCompile(`(?s)<tool_call>\s*<function=([^>]+)>(.*?)</function>\s*</tool_call>`)
	reXMLParam = regexp.MustCompile(`(?s)<parameter=([^>]+)>(.*?)</parameter>`)
)

// extractXMLToolCalls: fallback untuk model bandel yang menulis
// <tool_call><function=NAME><parameter=K>V</parameter>...</function></tool_call>
// sebagai TEKS. Return teks bersih + tool_calls. ID: call_1, call_2, ...
// ponytail: regex sederhana, bukan parser XML beneran.
func extractXMLToolCalls(text string) (string, []tcOut) {
	var calls []tcOut
	clean := reXMLCall.ReplaceAllStringFunc(text, func(block string) string {
		m := reXMLCall.FindStringSubmatch(block)
		if m == nil {
			return block
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			return block
		}
		args := map[string]string{}
		for _, p := range reXMLParam.FindAllStringSubmatch(m[2], -1) {
			args[strings.TrimSpace(p[1])] = strings.TrimSpace(p[2])
		}
		b, _ := json.Marshal(args)
		calls = append(calls, newTCCall(fmt.Sprintf("call_%d", len(calls)+1), name, string(b)))
		return ""
	})
	return strings.TrimSpace(clean), calls
}

func doUpstream(r *http.Request, body []byte) (*http.Response, error) {
	key := apiKey()
	if key == "" {
		return nil, fmt.Errorf("missing api key: set COMMANDCODE_API_KEY or login via cmd")
	}
	req, err := http.NewRequestWithContext(r.Context(), "POST", baseURL()+"/alpha/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("User-Agent", "cli") // required: without this → 403 Cloudflare 1010
	req.Header.Set("x-command-code-version", cliVersion)
	return http.DefaultClient.Do(req)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var in chatReq
	if err := json.Unmarshal(raw, &in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	maxTok := 1024
	if in.MaxTokens != nil && *in.MaxTokens > 0 {
		maxTok = *in.MaxTokens
	}
	model := in.Model
	if model == "" {
		model = upstreamModel
	}
	system, msgs := toWireMessages(in.Messages)
	if msgs == nil {
		msgs = []any{}
	}
	params := map[string]any{
		"model": model, "messages": msgs, "tools": toWireTools(in.Tools),
		"max_tokens": maxTok, "stream": true,
	}
	if system != "" {
		params["system"] = system
	}
	if in.Temperature != nil {
		params["temperature"] = *in.Temperature
	}
	wire := map[string]any{
		"config": map[string]any{"workingDir": "/tmp", "date": time.Now().Format("2006-01-02"), "environment": "linux", "structure": []string{}, "isGitRepo": false, "currentBranch": "", "mainBranch": "", "gitStatus": "", "recentCommits": []string{}},
		"memory": nil, "taste": nil, "skills": nil,
		"permissionMode": "standard", "mode": "agent", "params": params,
	}
	data, _ := json.Marshal(wire)

	// Continuation loop, faithful ke CLI (createModelClient.complete):
	// - threadId dikirim undefined (sessionId sess_* gagal validasi uuid,
	//   sama persis kayak CLI) → server yang pegang state pause.
	// - tiap attempt POST body yang SAMA (messages tidak di-append),
	//   teks + usage diakumulasi, berhenti kecuali raw=="pause_turn".
	if !in.Stream {
		var sb strings.Builder
		var u usage
		var calls []tcOut
		for attempt := 0; attempt < maxAttempts; attempt++ {
			resp, err := doUpstream(r, data)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if resp.StatusCode != 200 {
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				w.Write(b)
				return
			}
			text, uu, raw, cc := collect(resp.Body)
			resp.Body.Close()
			sb.WriteString(text)
			u = usage{u.inN + uu.inN, u.out + uu.out}
			calls = append(calls, cc...)
			if raw != "pause_turn" {
				break
			}
		}
		if len(calls) == 0 {
			if clean, xc := extractXMLToolCalls(sb.String()); len(xc) > 0 {
				sb.Reset()
				sb.WriteString(clean)
				calls = xc
			}
		}
		msg := map[string]any{"role": "assistant", "content": sb.String()}
		finish := "stop"
		if len(calls) > 0 {
			msg["tool_calls"] = calls
			finish = "tool_calls"
		}
		out := map[string]any{
			"id": "chatcmpl-cc", "object": "chat.completion", "created": time.Now().Unix(), "model": model,
			"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
			"usage":   map[string]any{"prompt_tokens": u.inN, "completion_tokens": u.out, "total_tokens": u.inN + u.out},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	var inN, outN int
	emit := func(delta string, finish any, usage any) {
		chunk := map[string]any{"id": "chatcmpl-cc", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": delta}, "finish_reason": finish}}}
		if usage != nil {
			chunk["usage"] = usage
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	// SSE tetap terbuka antar attempt; [DONE] cuma sekali di akhir.
	streamErr := ""
	var fullText strings.Builder
	var calls []tcOut
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := doUpstream(r, data)
		if err != nil {
			streamErr = err.Error()
			break
		}
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			streamErr = fmt.Sprintf("upstream %d: %s", resp.StatusCode, string(b))
			break
		}
		raw, emsg, i, o, cc := pumpStream(resp.Body, func(t string) { fullText.WriteString(t); emit(t, nil, nil) })
		resp.Body.Close()
		inN += i
		outN += o
		calls = append(calls, cc...)
		if emsg != "" {
			streamErr = emsg
			break
		}
		if raw != "pause_turn" {
			break
		}
	}
	emitTC := func(cc []tcOut, base int) {
		for j, c := range cc {
			idx := base + j
			for _, d := range []any{
				map[string]any{"tool_calls": []any{map[string]any{"index": idx, "id": c.ID, "type": "function", "function": map[string]any{"name": c.Function.Name, "arguments": ""}}}},
				map[string]any{"tool_calls": []any{map[string]any{"index": idx, "function": map[string]any{"arguments": c.Function.Arguments}}}},
			} {
				b, _ := json.Marshal(map[string]any{"id": "chatcmpl-cc", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
					"choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\n", b)
				if fl != nil {
					fl.Flush()
				}
			}
		}
	}
	if streamErr != "" {
		emit("", "error", nil)
		fmt.Fprintf(w, "data: {\"error\":%q}\n\n", streamErr)
	} else {
		if len(calls) == 0 {
			if _, xc := extractXMLToolCalls(fullText.String()); len(xc) > 0 {
				calls = xc // teks tag sudah terlanjur di-stream; yang penting client dapat tool_calls
			}
		}
		finish := "stop"
		if len(calls) > 0 {
			emitTC(calls, 0)
			finish = "tool_calls"
		}
		emit("", finish, map[string]any{"prompt_tokens": inN, "completion_tokens": outN, "total_tokens": inN + outN})
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

type usage struct{ inN, out int }

// pumpStream memompa SATU attempt upstream → onText per text-delta.
// return rawFinishReason ("pause_turn" = task belum kelar → panggil lagi)
// + tool_calls terstruktur bila ada event tool-call.
func pumpStream(body io.Reader, onText func(string)) (raw, errMsg string, inN, outN int, calls []tcOut) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e wireEvent
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Type {
		case "text-delta":
			if e.Text != "" {
				onText(e.Text)
			}
		case "tool-call":
			n++
			id := e.ToolCallID
			if id == "" {
				id = fmt.Sprintf("call_%d", n)
			}
			name := e.ToolName
			if name == "" {
				name = "unknown"
			}
			calls = append(calls, newTCCall(id, name, wireInputArgs(e.Input)))
		case "finish":
			if e.TotalUsage != nil {
				inN, outN = e.TotalUsage.InputTokens, e.TotalUsage.OutputTokens
			}
			raw = e.RawFinishReason
		case "error":
			errMsg = e.Message
			if errMsg == "" {
				errMsg = line
			}
			return
		}
	}
	return
}

func collect(r io.Reader) (string, usage, string, []tcOut) {
	var sb strings.Builder
	var u usage
	var raw string
	var calls []tcOut
	n := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e wireEvent
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.Type == "text-delta" {
			sb.WriteString(e.Text)
		} else if e.Type == "tool-call" {
			n++
			id := e.ToolCallID
			if id == "" {
				id = fmt.Sprintf("call_%d", n)
			}
			name := e.ToolName
			if name == "" {
				name = "unknown"
			}
			calls = append(calls, newTCCall(id, name, wireInputArgs(e.Input)))
		} else if e.Type == "finish" {
			if e.TotalUsage != nil {
				u = usage{e.TotalUsage.InputTokens, e.TotalUsage.OutputTokens}
			}
			raw = e.RawFinishReason
		}
	}
	return sb.String(), u, raw, calls
}

// validModels: hasil test "hi" per model 2026-09-03 di akun ini (36/65).
// ponytail: hardcode, tidak ada endpoint upstream untuk list model.
var validModels = []string{
	"xiaomi/mimo-v2.5", "xiaomi/mimo-v2.5-pro",
	"Qwen/Qwen3.8-Max", "Qwen/Qwen3.8-Flash", "Qwen/Qwen3.8-27B",
	"Qwen/Qwen3.7-Max", "Qwen/Qwen3.7-Plus", "Qwen/Qwen3.7-Flash",
	"Qwen/Qwen3.6-Max-Preview", "Qwen/Qwen3.6-Plus",
	"zai-org/GLM-5.3", "zai-org/GLM-5.2", "zai-org/GLM-5.1", "zai-org/GLM-5",
	"zai-org/GLM-5.2-Fast", "z-ai/glm-5.3-flash",
	"moonshotai/Kimi-K3", "moonshotai/Kimi-K2.7-Code", "moonshotai/Kimi-K2.7-Code-Highspeed",
	"moonshotai/Kimi-K2.6", "moonshotai/Kimi-K2.5",
	"deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash",
	"deepseek/deepseek-v4-flash-fast", "deepseek/deepseek-v4-flash-vision-exp",
	"MiniMaxAI/MiniMax-M3", "MiniMaxAI/MiniMax-M2.5",
	"stepfun/Step-3.7-Flash", "stepfun/Step-3.5-Flash",
	"tencent/hy3-paid", "tencent/hy4-preview",
	"gpt-5.6-luna", "xai/grok-4.5",
	"thinkingmachines/inkling", "thinkingmachines/inkling-small",
	"meta/muse-spark-1.2-contributor",
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	data := make([]any, 0, len(validModels))
	for _, id := range validModels {
		m := map[string]any{"id": id, "object": "model", "created": 1700000000, "owned_by": "commandcode-proxy",
			"supported_parameters": []string{"temperature", "max_tokens", "tools"}}
		if mm, ok := modelMetas[id]; ok {
			m["name"], m["description"] = mm.name, mm.desc
			if mm.context > 0 {
				m["context_length"] = mm.context
			}
			mods := append([]string{}, mm.modalities...)
			m["architecture"] = map[string]any{"input_modalities": mods,
				"modality": strings.Join(mods, "+") + "->text", "output_modalities": []string{"text"}}
			if mm.reasoning {
				m["reasoning"] = map[string]any{"supported": true}
			}
		}
		data = append(data, m)
	}
	json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handleChat)
	mux.HandleFunc("/v1/models", handleModels)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","model":"` + upstreamModel + `"}`))
	})
	fmt.Printf("commandcode-proxy → %s default=%s :%s\n", baseURL(), upstreamModel, port)
	http.ListenAndServe(":"+port, mux)
}
