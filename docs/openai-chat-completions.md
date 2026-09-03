# OpenAI Chat Completions — required vs optional

Sumber: `openai/openai-openapi` (`openapi.yaml`/`openapi.json`) + API Reference
(`developers_openai_api_reference`), via Context7. Acuan untuk audit `commandcode-proxy`.

## Request `POST /v1/chat/completions`

Required (hanya 2): `model`, `messages` (`minItems: 1`). Auth bukan body field —
header `Authorization: Bearer` (lapisan HTTP, `ApiKeyAuth` di spec).

| Field | Wajib? | Tipe / default | Catatan |
|---|---|---|---|
| `model` | wajib | string (enum `ModelIdsShared`) | |
| `messages` | wajib | array ≥1 `ChatCompletionRequestMessage` | role `system/developer/user/assistant/tool`; `content` string atau array parts (`text`, `image_url`, `input_audio`, `file`) |
| `temperature` | opsional | number 0–2, default 1, nullable | |
| `top_p` | opsional | number, default 1 | |
| `n` | opsional | int 1–128, default 1 | selalu 1 choice di proxy |
| `stream` | opsional | bool, default false, nullable | |
| `stream_options` | opsional | `{include_usage?: bool}` | satu-satunya cara dapat `usage` saat stream |
| `stop` | opsional | string / string[] (`StopConfiguration`) | bukan `stop_sequences` |
| `max_tokens` | opsional | int, nullable, **deprecated** | diganti `max_completion_tokens` |
| `max_completion_tokens` | opsional | int, nullable | |
| `presence_penalty` | opsional | number −2–2, default 0 | |
| `frequency_penalty` | opsional | number −2–2, default 0 | |
| `logit_bias` | opsional | map token→int, nullable | |
| `logprobs` | opsional | bool, default false | + `top_logprobs` 0–20 |
| `seed` | opsional | int, nullable, **deprecated** | |
| `tools` | opsional | array `function` (+ `custom` baru) | |
| `tool_choice` | opsional | `none/auto/required/named` | default `auto` bila `tools` ada |
| `parallel_tool_calls` | opsional | bool | |
| `response_format` | opsional | `text/json_object/json_schema` | |
| `reasoning_effort` | opsional | enum | reasoning models |
| `verbosity` | opsional | enum | baru, output verbosity |
| `modalities` | opsional | enum | |
| `audio` | opsional | `{voice, format}` (keduanya wajib bila dipakai) | |
| `prediction` | opsional | `PredictionContent` | |
| `store` | opsional | bool, default false | |
| `metadata` | opsional | object | |
| `service_tier` | opsional | enum | |
| `user` | opsional | string | |
| `web_search_options` | opsional | object | |
| `prompt_cache_key` | opsional | string | cache affinity (kita pakai untuk `x-session-id`) |

## Response non-stream `chat.completion`

Required: `id`, `object` (`chat.completion`), `created`, `model`, `choices`.
Choice required: `index`, `message`, `finish_reason`, `logprobs`.
Message: `role`, `content` (bisa `null` saat tool-call), `refusal` (required, null bila tidak ada),
`tool_calls` / `function_call` (deprecated) bila `finish_reason: tool_calls`.
`finish_reason` enum: `stop | length | tool_calls | content_filter | function_call`.
Field tambahan yang sering muncul tapi opsional: `system_fingerprint`, `usage`,
`service_tier`, `seed/top_p/temperature/penalties` (echo), `tool_choice`, `tools`, `metadata`.

## Streaming `chat.completion.chunk`

- Chunk teks: `choices[0].delta = {role?, content?}`, `finish_reason: null`.
- Chunk pertama membawa `delta.role: "assistant"` (boleh `content: ""`).
- Chunk terminal: `delta: {}`, `finish_reason` terisi.
- `logprobs: null` per choice. Tool chunk: delta bertahap
  (`id+name` dulu, lalu fragmen `arguments`), dirakit client via `index`.
- Usage saat stream HANYA bila `stream_options.include_usage: true`, dikirim sebagai
  chunk terpisah dengan `choices: []` + `usage` (boleh sebelum atau sesudah chunk terminal).

## `usage` (`CompletionUsage`)

`prompt_tokens`, `completion_tokens`, `total_tokens`, plus opsional:
- `prompt_tokens_details: {cached_tokens?, audio_tokens?}`
- `completion_tokens_details: {reasoning_tokens?, accepted_prediction_tokens?, rejected_prediction_tokens?, audio_tokens?}`

## Error

`{error: {message, type, code}}`, HTTP status semantik
(400 `invalid_request_error`, 401, 403, 404, 429, 5xx). Konteks kepanjangan:
400 `context_length_exceeded`.

## Gap map vs `commandcode-proxy` (status kini)

Didukung: model/messages(array-aware)/tools/stream/include_usage/max_tokens +
max_completion_tokens/temperature/reasoning_effort/user/prompt_cache_key,
`refusal/logprobs/role` di response, `cached_tokens/reasoning_tokens`,
`system_fingerprint`, 400 `context_length_exceeded`.
Sengaja diabaikan (tanpa pasangan upstream, diterima-tapi-diabaikan):
`top_p`, penalties, `seed`, `logit_bias`, `logprobs` request, `stop`,
`n`, `response_format`, `tool_choice`, `parallel_tool_calls`, `modalities`,
`prediction`, `metadata`, `store`, `verbosity`, `audio`, `web_search_options`.
Image/audio parts di-drop dari `content` array (hemat prefix, stabil cache).
