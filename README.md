# commandcode-proxy

OpenAI-compatible proxy for the CommandCode API. Stdlib-only Go server, no dependencies.

It exposes `POST /v1/chat/completions` (streaming and non-streaming, with tool calls), `GET /v1/models`, and `GET /health`, and forwards requests to CommandCode `/alpha/generate`.

## Requirements

- Go 1.25+
- A CommandCode API key
- Optional: Docker, Fly.io CLI

## Clone

```bash
git clone https://github.com/dimassfeb-09/commandcode-proxy.git
cd commandcode-proxy
```

## Configure

Set your API key via environment variable:

```bash
export COMMANDCODE_API_KEY="your-key-here"
```

Fallbacks, in order:

1. `COMMANDCODE_API_KEY`
2. `COMMAND_CODE_API_KEY`
3. `~/.commandcode/auth.json` (`{ "apiKey": "..." }`)

Optional variables:

| Variable | Default | Description |
|---|---|---|
| `COMMANDCODE_API_KEY` | - | API key (preferred) |
| `COMMAND_CODE_API_KEY` | - | API key (alternate name) |
| `COMMANDCODE_API_URL` | `https://api.commandcode.ai` | Upstream base URL |
| `PROXY_API_KEY` | generated per run | Bearer key required by `/v1/*` clients |
| `PORT` | `8080` | Listen port |

On startup the server prints the generated key when `PROXY_API_KEY` is not set:

```text
PROXY_API_KEY not set — generated: ccp_abc123...
```

All `/v1/*` endpoints require `Authorization: Bearer <key>`. `/health` stays open for load balancer and container health checks. For a stable key across deploys, set `PROXY_API_KEY` explicitly (e.g. `fly secrets set PROXY_API_KEY="$(openssl rand -hex 32 | sed 's/^/ccp_/')"`).

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
docker run -p 8080:8080 -e COMMANDCODE_API_KEY="$COMMANDCODE_API_KEY" commandcode-proxy
```

Fly.io:

```bash
fly launch
fly secrets set COMMANDCODE_API_KEY="your-key-here"
fly deploy
```

Health check:

```bash
curl http://localhost:8080/health
```

## Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions, `stream: true/false` |
| `GET` | `/v1/models` | Model list in OpenAI format |
| `GET` | `/health` | `{"status":"ok"}` |

## Usage with AI Agents

Point any OpenAI-compatible client at this server:

- Base URL: `http://localhost:8080/v1`
- API key: the `PROXY_API_KEY` value (generated at startup or set via env)
- Model: any id from `GET /v1/models`, e.g. `xiaomi/mimo-v2.5`

List models:

```bash
curl -H "Authorization: Bearer $PROXY_API_KEY" http://localhost:8080/v1/models
```

Non-streaming completion:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -d '{"model":"xiaomi/mimo-v2.5","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Streaming completion:

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $PROXY_API_KEY" \
  -d '{"model":"xiaomi/mimo-v2.5","messages":[{"role":"user","content":"hi"}],"stream":true}'
```

Python (`openai` package):

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key=os.environ["PROXY_API_KEY"])
resp = client.chat.completions.create(
    model="xiaomi/mimo-v2.5",
    messages=[{"role": "user", "content": "hi"}],
)
print(resp.choices[0].message.content)
```

Node.js (`openai` package):

```js
import OpenAI from "openai";

const client = new OpenAI({ baseURL: "http://localhost:8080/v1", apiKey: process.env.PROXY_API_KEY });
const resp = await client.chat.completions.create({
  model: "xiaomi/mimo-v2.5",
  messages: [{ role: "user", content: "hi" }],
});
console.log(resp.choices[0].message.content);
```

Tool calling works the standard OpenAI way: pass `tools` with `type: "function"`, receive `tool_calls` with `finish_reason: "tool_calls"`, then send results back as `role: "tool"` messages with matching `tool_call_id`.

DeepSeek-style harness or any agent framework with an OpenAI-compatible provider: set provider base URL to `http://localhost:8080/v1` and model to one of the listed ids. Fetching is plain HTTP POST to `/v1/chat/completions` with SSE (`text/event-stream`) when `stream: true`, ending with `data: [DONE]`.
