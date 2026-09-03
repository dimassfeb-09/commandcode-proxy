# commandcode-proxy

OpenAI-compatible proxy for the CommandCode API. Stdlib-only Go server, no dependencies.

It exposes `POST /v1/chat/completions` (streaming and non-streaming, with tool calls), `GET /v1/models`, and `GET /health`, and forwards requests to CommandCode `/alpha/generate`.

Each caller sends their own CommandCode key as the standard OpenAI `Authorization: Bearer <key>` header. The proxy forwards that key to upstream. No shared proxy key, nothing to mint or distribute. This matches the OpenAI spec: auth is a header layer, never a body field.

## Requirements

- Go 1.25+
- A CommandCode API key per user
- Optional: Docker, Fly.io CLI

## Clone

```bash
git clone https://github.com/dimassfeb-09/commandcode-proxy.git
cd commandcode-proxy
```

## Configure

Each user already has a CommandCode key. They pass it per request as the Bearer header. No server-side key setup needed for deployment.

For local single-user dev only, you may set a fallback key on the server:

```bash
export COMMANDCODE_API_KEY="your-key-here"
```

Fallbacks, in order:

1. `Authorization: Bearer <key>` header (per request, forwarded to upstream)
2. `COMMANDCODE_API_KEY`
3. `COMMAND_CODE_API_KEY`
4. `~/.commandcode/auth.json` (`{ "apiKey": "..." }`)

Do not set `COMMANDCODE_API_KEY` on a shared deploy, otherwise requests without a header would silently bill your key.

Optional variables:

| Variable | Default | Description |
|---|---|---|
| `COMMANDCODE_API_KEY` | - | Fallback key for local dev only |
| `COMMAND_CODE_API_KEY` | - | Fallback key (alternate name) |
| `COMMANDCODE_API_URL` | `https://api.commandcode.ai` | Upstream base URL |
| `PORT` | `8080` | Listen port |

## Build and Run

Run directly:

```bash
go run ./cmd/server
```

Build a binary:

```bash
go build -o server ./cmd/server
./server
```

Run tests:

```bash
go test ./...
```

Docker:

```bash
docker build -t commandcode-proxy .
docker run -p 8080:8080 commandcode-proxy
```

Fly.io:

```bash
fly launch
fly deploy
```

No secrets to set on the server. Each caller brings their own key.

Health check (open, no auth):

```bash
curl http://localhost:8080/health
```

## Endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/v1/chat/completions` | Bearer (CommandCode key) | OpenAI Chat Completions, `stream: true/false` |
| `GET` | `/v1/models` | Bearer (any non-empty value accepted) | Model list in OpenAI format |
| `GET` | `/health` | none | `{"status":"ok"}` |

## Usage with AI Agents

Point any OpenAI-compatible client at this server:

- Base URL: `http://localhost:8080/v1`
- API key: your CommandCode key (sent as `Authorization: Bearer`, forwarded upstream)
- Model: any id from `GET /v1/models`, e.g. `xiaomi/mimo-v2.5`

List models:

```bash
curl -H "Authorization: Bearer $COMMANDCODE_API_KEY" http://localhost:8080/v1/models
```

Non-streaming completion:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $COMMANDCODE_API_KEY" \
  -d '{"model":"xiaomi/mimo-v2.5","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Streaming completion:

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $COMMANDCODE_API_KEY" \
  -d '{"model":"xiaomi/mimo-v2.5","messages":[{"role":"user","content":"hi"}],"stream":true}'
```

Python (`openai` package):

```python
import os
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key=os.environ["COMMANDCODE_API_KEY"])
resp = client.chat.completions.create(
    model="xiaomi/mimo-v2.5",
    messages=[{"role": "user", "content": "hi"}],
)
print(resp.choices[0].message.content)
```

Node.js (`openai` package):

```js
import OpenAI from "openai";

const client = new OpenAI({ baseURL: "http://localhost:8080/v1", apiKey: process.env.COMMANDCODE_API_KEY });
const resp = await client.chat.completions.create({
  model: "xiaomi/mimo-v2.5",
  messages: [{ role: "user", content: "hi" }],
});
console.log(resp.choices[0].message.content);
```

Tool calling works the standard OpenAI way: pass `tools` with `type: "function"`, receive `tool_calls` with `finish_reason: "tool_calls"`, then send results back as `role: "tool"` messages with matching `tool_call_id`.

## Sessions and cache hits

There is no server-side session. The proxy is stateless: continuity comes from the client resending the full message history on every request, exactly like the OpenAI API. (The CLI's `sess_*` id is local-only telemetry; upstream only accepts UUID `threadId`, so it is never sent.)

Upstream prefix-caches identical prompt prefixes. To get cache hits across turns:

- Keep `model`, `system`, `tools`, and message history byte-identical; only append new messages.
- Do not reorder, rephrase, or drop history between turns.
- Within one request the proxy re-POSTs the identical body while upstream reports `pause_turn` (up to 6 attempts), same as the CLI.

`usage.prompt_tokens_details.cached_tokens` reports how many prompt tokens were cache hits. Verified: two identical requests scored 6144 then 7360 of 7397 prompt tokens cached.

## Usage and context window (OpenAI-compatible)

- Non-streaming: standard `usage` object with `prompt_tokens`, `completion_tokens`, `total_tokens`, plus `prompt_tokens_details.cached_tokens` and `completion_tokens_details.reasoning_tokens`.
- Streaming: pass `"stream_options": {"include_usage": true}`. Intermediate chunks carry no `usage`; the final chunk before `data: [DONE]` has `"choices": []` and the populated `usage` object, exactly per the OpenAI spec. Verified with the official `openai` Python client (v2.24.0) for both modes.
- Context window: `GET /v1/models` returns `context_length` per model (OpenRouter-style extension; official OpenAI omits it). Remaining window = `context_length - prompt_tokens`.

DeepSeek-style harness or any agent framework with an OpenAI-compatible provider: set provider base URL to `http://localhost:8080/v1`, api key to the user's CommandCode key, and model to one of the listed ids. Fetching is plain HTTP POST to `/v1/chat/completions` with SSE (`text/event-stream`) when `stream: true`, ending with `data: [DONE]`.
