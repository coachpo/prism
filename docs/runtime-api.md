# Runtime API

This reference follows the [operation registry](../backend/internal/httpapi/runtime/operations.go). See [routing and execution](runtime-routing.md) for planning and attempt ownership, and [shared HTTP behavior](api-reference.md) for health and error envelopes.

Prism's runtime proxy is an explicit allowlist, not a full vendor API clone. It supports only the operations listed in this section through frozen Default profile id `1`. Other vendor routes, including stored-object, retrieve, delete, cancel, embedding, file, batch, and admin APIs, are outside Prism's runtime contract unless they appear in this allowlist.

Runtime proxy routes ignore management `X-Profile-Id` overrides and always resolve against frozen Default profile id `1`. Profile-scoped management reads and writes are pinned to Default profile id `1`; they do not switch proxy traffic.

After global CORS handling, the runtime branch applies HTTP proxy admission, then runtime proxy-key authentication, and only then the exact operation registry. HTTP admission and auth can therefore reject a `/v1` or `/v1beta` request before registry resolution. Registry rejections themselves do not read the request body or invoke planning, terminal-target admission, provider transport, telemetry, audit, feedback, or runtime side effects.

When operator auth is enabled, runtime proxy routes require a valid active, unexpired proxy API key. Prism checks `Authorization: Bearer <key>` first, then `X-API-Key`, then `X-Goog-Api-Key`. Missing keys return `401` with `Proxy API key required`; invalid, inactive, expired, or unknown keys return `401` with `Invalid proxy API key`. When auth is disabled, supported runtime routes continue without proxy-key authentication. New runtime requests observe published auth and proxy-key changes. Rotation, disabling, and expiry invalidate the corresponding credentials for new requests while key enforcement is enabled. See the [management API](management-api.md#proxy-api-keys) for creation, rotation, and deletion.

## Supported Runtime Operations

| Operation | Canonical operation name | Supported request |
| --- | --- | --- |
| OpenAI models list | `openai.models` | `GET /v1/models` |
| OpenAI chat completions | `openai.chat_completions` | `POST /v1/chat/completions` |
| OpenAI Responses | `openai.responses` | `POST /v1/responses` |
| OpenAI Responses input tokens | `openai.responses.input_tokens` | `POST /v1/responses/input_tokens` |
| OpenAI Responses compact | `openai.responses.compact` | `POST /v1/responses/compact` |
| OpenAI image generations | `openai.images.generations` | `POST /v1/images/generations` |
| OpenAI image edits | `openai.images.edits` | `POST /v1/images/edits` |
| Anthropic Messages | `anthropic.messages` | `POST /v1/messages` |
| Anthropic token count | `anthropic.count_tokens` | `POST /v1/messages/count_tokens` |
| Gemini generate content | `gemini.generate_content` | `POST /v1beta/models/{model}:generateContent` |
| Gemini stream generate content | `gemini.stream_generate_content` | `POST /v1beta/models/{model}:streamGenerateContent` |
| Gemini token count | `gemini.count_tokens` | `POST /v1beta/models/{model}:countTokens` |

Each allowlisted row maps to one canonical operation name. Provider-forwarded runtime operations persist that name as `operation_name` in runtime telemetry. Operation names are part of the runtime contract, not aliases for broader vendor route groups. The Gemini `{model}` path binding is one non-empty path segment and cannot contain `/` or `:`. Nested Gemini model paths are not part of this runtime contract.

## Unsupported Routes and Methods

Unsupported runtime routes return a Prism JSON `404` response before Prism reads the request body, resolves a model, claims local Terminal Target admission state, contacts a provider, submits runtime side effects, or writes runtime persistence rows. The current error detail is `Runtime operation not found`.

Wrong methods on supported runtime paths return a Prism JSON `405` response before the same downstream seams run. The response includes the supported method in `Allow`, and the current error detail is `Method not allowed for runtime operation`.

Supported runtime operation request bodies are capped at `20 MiB`. Oversized requests return JSON `413` with `error: "request_body_too_large"` and `limit_bytes`. A known oversized Content-Length is rejected before planning. For path-bound Gemini requests with unknown lengths, candidate probing can precede reading enough of the body to detect the overflow.

When any planned Terminal Target candidate has custom request parameters configured, the ingress body must be a valid JSON object (otherwise `400` with `Request body must be a JSON object when custom request parameters are configured`), the per-attempt merged body is re-validated against the same `20 MiB` limit (`413 request_body_too_large` before transport), and Gemini path-bound operations reject non-identity `Content-Encoding` with `415` (`Content-Encoding is not supported when custom request parameters are configured`) before buffering. These failures never reach admission, Ban Policy attempt counting, provider transport, or audit body capture.

## Routing Failures

Runtime planning keeps two explicit model indexes: all enabled models for recursive graph resolution, and only enabled `direct_request_enabled=true` models for client-bound operation lookup. A non-entry model ID therefore returns the existing `404` before provider transport, telemetry, audit, feedback, admission, or durable side effects; a direct parent may still resolve through that node. Once a direct root is accepted, planning orders its enabled mixed access-target rows once by `(position, id)` and lets the attached strategy shape the effective peer sequence: `single` keeps only the first enabled mixed row, `fill-first` walks the authored mixed order, and `round-robin` rotates the direct mixed rows once per request. A Model Target row recursively resolves through the child model's own strategy and contributes one contiguous block; a Terminal Target row contributes its own attempt. Candidate-local misses (zero-leaf child, unavailable connection, operation incompatibility, routing window closed) skip to the next effective peer, while cycle, depth, and missing-strategy errors fail closed. If no eligible Terminal Target is available inside the current retry window, Prism returns a routing-availability error before opening an upstream request. If all otherwise eligible attempts are blocked by admission counters, Prism returns `503` with `error: "admission_exhausted"` plus route-reason metadata before upstream transport. An OpenAI text request whose requested model does not accept the ingress operation rejects immediately with `400 openai_operation_not_supported`. Terminal Target connections that do not natively support the operation are skipped so later native attempts remain eligible; if every otherwise eligible attempt is wire-incompatible, Prism returns `400 openai_request_translation_unsupported` with `translation_mode: "none"` and `unsupported_reason: "operation_translation_unsupported"` before provider transport. Availability failures with no otherwise eligible attempt retain the ordinary `503` no-eligible-target response.

Two family-neutral planning codes describe routing-schedule rejections, and this section is their authoritative definition. `terminal_target_schedule_closed` is returned when every terminal target the request evaluated was excluded solely because it sits outside its configured routing window; `terminal_target_schedule_unresolvable` is returned when they were all excluded solely because their routing timezone could not be resolved. Both are `503`. Neither fires when any other cause contributed to the failure — a mixed failure keeps the ordinary response and appends an `N of M` sentence to the detail instead, so the stable codes never overstate what happened. The closed code carries `schedule_excluded_connection_ids`, `schedule_excluded_connection_ids_truncated`, `schedule_excluded_connection_count`, `schedule_reference_now`, `schedule_earliest_next_open_at`, and `schedule_earliest_next_open_at_known`; the unresolvable code carries the matching `schedule_unresolvable_*` trio plus `schedule_reference_now`. The `_at` keys are absent whenever the matching `_known` flag is false.

Request-log detail keeps flat attribution fields for the ingress model, attempt target model, actual `terminal_target_id`, planned `selected_terminal_target_id`, endpoint, and operation translation mode. Deleted model-owned routing metadata is not exposed on public detail responses.

## OpenAI native mode equality (strict)

OpenAI text routing is native-only and mode-strict. The requested model's `openai_accepted_format` and each Terminal Target connection's `openai_text_capability` use `responses_only`, `chat_completions_only`, or `dual_native`; both must be **exactly equal** (only the diagonal of the 3×3 mode matrix is legal). A requested model that does not accept the ingress operation returns `400 openai_operation_not_supported` before target resolution. A connection whose mode differs from the requested model is skipped so load balancing can try the next authored target; when every otherwise eligible connection is mode-incompatible, Prism returns `400 openai_request_translation_unsupported` before provider transport. The response detail is `Prism cannot translate this OpenAI request shape for the selected target.`, with `translation_mode: "none"` and `unsupported_reason: "operation_translation_unsupported"`. Responses adjunct operations, `openai.responses.input_tokens` and `openai.responses.compact`, require a responses-capable target, which equality guarantees for `responses_only` and `dual_native`.

Management enforcement mirrors the runtime contract. Authoring any OpenAI relation (model to model, model to Terminal Target, including references to shared connections) whose source mode differs from the target mode is rejected with `422 Unprocessable` and issue code `target_openai_mode_mismatch` inside `routing_plan_issues`; disabled or inactive relations are not exempt. Changing a persisted model `openai_accepted_format` or connection `openai_text_capability` that would break an existing relation returns `409 Conflict`. Non-OpenAI families keep the existing api-family validation channels unchanged.

The internal `openaimodecheck.Check` scan is read-only and reports persisted `model_target`/`connection_target` violations in stable order. Startup invokes it as `openai_text_mode_check` after SQL migrations, pricing-transition validation, and observability upgrade, and before retention cutover and invariant/default seeds. A violation fails startup with `openai text mode equality check failed` and a summary. The executable has no separate `PRISM_OPENAI_MODE_PREFLIGHT` environment entrypoint.

Static diagnostics classify operation-set overlap as `full`, `partial`, or `none`; this evidence does not authorize a text mode mismatch. When planning fails, a compatible leaf excluded by static enablement/activation/strategy produces `503 openai_no_eligible_terminal_target`; no authored reachable route produces `503 openai_no_compatible_terminal_target`; a reachable incompatible terminal leaf produces `400 openai_request_translation_unsupported`. A model-only path with no leaf or a dynamically unavailable compatible route keeps the ordinary `503` family; admission uses `503 admission_exhausted`.

Prism does not convert requests, non-stream responses, or streams between Chat Completions and Responses. Native attempts use the ingress operation's upstream path and preserve `operation_translation_mode = "none"`. The `operation_translation_mode` columns and request-log fields remain readable for historical rows that recorded the retired translation values.

### Application capability matrix

The following application-spec example assumes these OpenAI text capabilities and matching access-target order:

- `gpt-5.5`: `dual_native`
- `gpt-5.4`: `dual_native`
- `deepseek-v4-flash`: `chat_completions_only`

Native request behavior (strict mode equality: each target shown is authored with the same mode as the requested model):

| Requested model | Ingress path | Target capability | Upstream path | `operation_translation_mode` | Client-visible shape |
| --- | --- | --- | --- | --- | --- |
| `gpt-5.5` | `/v1/responses` | `dual_native` | `/v1/responses` | `none` | Responses |
| `gpt-5.5` | `/v1/chat/completions` | `dual_native` | `/v1/chat/completions` | `none` | Chat Completions |
| `gpt-5.4` | `/v1/responses` | `dual_native` | `/v1/responses` | `none` | Responses |
| `gpt-5.4` | `/v1/chat/completions` | `dual_native` | `/v1/chat/completions` | `none` | Chat Completions |
| `deepseek-v4-flash` | `/v1/responses` | `chat_completions_only` | No upstream request | N/A | Rejected |
| `deepseek-v4-flash` | `/v1/chat/completions` | `chat_completions_only` | `/v1/chat/completions` | `none` | Chat Completions |

Cross-mode targets are authoring-rejected before they can be persisted, so a requested `chat_completions_only` model can never reach a `dual_native` connection and a requested `responses_only` model can never reach a `dual_native` connection; such combinations would return `400 openai_request_translation_unsupported` before provider transport with zero upstream calls if they existed in legacy data.

## OpenAI image capability (independent dimension, containment)

OpenAI image support is a second capability dimension that is independent of the text dimension. The requested model's `openai_image_operations` and each Terminal Target connection's `openai_image_capability` use `generations`, `edits`, or `generations_and_edits`; `NULL` means the row does not serve images at all. The two dimensions never answer for each other: a text capability serves no image operation, and an image capability serves no text operation.

An OpenAI model or Terminal Target must declare at least one of the two dimensions. A row that declares neither could serve no operation, so `ck_model_configs_openai_dimensions` and `ck_connections_openai_dimensions` reject it, and the management API returns `400`/`422` with the same rule. `openai_accepted_format` and `openai_text_capability` are therefore nullable for `api_family = 'openai'`: a pure image model such as `gpt-image-2` speaks no text protocol.

The frontend preserves that absence throughout Terminal Target authoring. `ConnectionDialog.tsx` hides the text capability section when the owner has no text mode; `useModelDetailDialogState.ts` and `connectionDataSupport.ts` preserve `null` in create/edit drafts and submitted OpenAI payloads, without substituting `responses_only`. Prefill keeps the destination owner's text capability. The local `classifyOpenAICoverage.ts` helper treats absent modes as empty text-operation sets: an owner with no accepted text operations has `full` text coverage because none is unserved. This display result proves neither image support nor a valid cross-mode relation; backend equality and image gates remain authoritative.

Image coverage is **containment**, not the strict equality the text dimension requires. Chat Completions and Responses are mutually exclusive wire protocols, so a text target speaking the other protocol is useless. Image generations and edits are additive capabilities on one protocol, so a target serving both can safely back a model that only accepts one. A Terminal Target must serve every image operation its owner model accepts and may serve more; a narrower target is rejected at authoring time with issue code `target_openai_image_uncovered`, and an omitted `openai_image_capability` is defaulted from the owner model's own image dimension.

The image dimension has no separate startup check. Its authoring gate requires owner coverage, its runtime gate checks the requested image operation, and its diagnostics can describe partial coverage in an inspected graph. The `openai_text_mode_check` startup guard checks text equality only.

At runtime an image request resolves against the image dimension alone: a model that does not accept the ingress image operation returns `400 openai_operation_not_supported` before target resolution, and Terminal Targets that do not serve it are skipped so later eligible attempts remain usable.

## Retired Exact OpenAI Facade Routing

Exact OpenAI facade routing and its model fields are retired. Runtime planning uses the requested model's ordinary access-target graph, native Terminal Target capability checks, and the attached Ban Policy strategy. Prism no longer performs context-window preflight filtering or returns context-window-exceeded planning errors.

## Retired Overflow Replay

Model-scoped overflow replay and its authoring fields are retired. Runtime planning now uses the ordinary operation registry, access-target graph, strict mode-equality checks, and the attached Ban Policy strategy. Public request-log and usage surfaces keep flat requested model, final target, Terminal Target, endpoint, and operation fields without nested retired routing metadata.

## OpenAI Operations

### Models

```
GET /v1/models
```

Local OpenAI-shaped `{"object":"list","data":[...]}` list of enabled `api_family="openai"` models with `direct_request_enabled=true` for frozen Default profile id `1`. Enabled non-entry Model Target nodes are omitted even though direct parents may route to them. Query parameters, including the retired `client_version`, do not select an alternate response shape. Prism does not contact configured model providers for this local operation.

### Chat Completions

```
POST /v1/chat/completions
```

Request (standard OpenAI format):

```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "temperature": 0.7,
  "stream": false
}
```

Response: Native attempts are proxied from an upstream Chat Completions-capable OpenAI endpoint. Canonical operation name: `openai.chat_completions`.

### Responses

```
POST /v1/responses
```

Request (OpenAI Responses generation format):

```json
{
  "model": "gpt-4o",
  "input": "Hello!",
  "stream": false
}
```

Response: Native attempts are proxied from an upstream Responses-capable OpenAI endpoint. Canonical operation name: `openai.responses`.

### Image Generations

```
POST /v1/images/generations
```

Request uses the upstream OpenAI-compatible image generation body, including body-bound `model`. `stream: true` opts into a server-sent event stream of `image_generation.partial_image` events terminated by `image_generation.completed`; the non-stream response is the ordinary `ImagesResponse` JSON body. Canonical operation name: `openai.images.generations`.

### Image Edits

```
POST /v1/images/edits
```

Request uses the upstream OpenAI-compatible JSON image edit body, whose `images` array references inputs by URL, data URL, or uploaded file id. `stream: true` opts into `image_edit.partial_image` events terminated by `image_edit.completed`. Canonical operation name: `openai.images.edits`.

`multipart/form-data` image edits are **not** part of this runtime contract. Only the JSON channel is registered, and the ordinary `20 MiB` request body limit applies. Because the operation binds its model from the JSON body, a multipart edit body cannot be routed and is rejected with `400 Cannot determine model for routing. Operation 'openai.images.edits' binds models from the body.` before provider transport; it is never proxied unrouted.

### Responses Input Tokens

```
POST /v1/responses/input_tokens
```

Request uses the OpenAI Responses input-token counting format, including body-bound `model` and `input`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses.input_tokens`.

### Responses Compact

```
POST /v1/responses/compact
```

Request uses the OpenAI Responses compaction format, including body-bound `model` and `input`.
Response: Proxied directly from the upstream OpenAI-compatible endpoint. Canonical operation name: `openai.responses.compact`.

## Anthropic Operations

### Messages

```
POST /v1/messages
```

Request (standard Anthropic format):

```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}
```

Response: Proxied directly from the upstream Anthropic-compatible endpoint. Canonical operation name: `anthropic.messages`.

### Token Count

```
POST /v1/messages/count_tokens
```

Request uses the upstream Anthropic-compatible token-count body, including body-bound `model`.
Response: Proxied directly from the upstream Anthropic-compatible endpoint. Canonical operation name: `anthropic.count_tokens`.

## Gemini Operations

### Generate Content

```
POST /v1beta/models/{model}:generateContent
```

Request (standard Gemini native format):

```json
{
  "contents": [
    {
      "role": "user",
      "parts": [{"text": "Hello!"}]
    }
  ]
}
```

Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.generate_content`.

### Stream Generate Content

```
POST /v1beta/models/{model}:streamGenerateContent
```

Request uses the upstream Gemini native generate-content body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.stream_generate_content`.
The `{model}` binding must be one non-empty path segment and cannot contain `/` or `:`.

### Count Tokens

```
POST /v1beta/models/{model}:countTokens
```

Request uses the upstream Gemini native token-count body with the model bound from the path.
Response: Proxied directly from the upstream Gemini-compatible endpoint. Canonical operation name: `gemini.count_tokens`.
For all Gemini runtime paths in this section, the `{model}` binding must be one non-empty path segment and cannot contain `/` or `:`.

## Streaming

Streaming stays operation-native: `openai.chat_completions`, `openai.responses`, `openai.images.generations`, `openai.images.edits`, and `anthropic.messages` use their upstream-compatible request body flags, while `gemini.stream_generate_content` uses `POST /v1beta/models/{model}:streamGenerateContent`. Streaming responses are proxied from a natively compatible upstream.
For Gemini, the `gemini.stream_generate_content` path is authoritative: `POST /v1beta/models/{model}:streamGenerateContent` is treated as streaming even when the request body omits `stream: true`. `gemini.generate_content` remains the non-stream generate-content operation.

## Token Usage Extraction

The gateway extracts token usage from upstream responses and logs canonical disjoint token components to `request_logs`. Extraction is selected by the resolved canonical operation name and its hook collection. `input_tokens` is base input only, `output_tokens` is base output only, cache-read input, cache-creation input, and reasoning output are separate fields, and `total_tokens` uses the provider total when one is supplied.

**Non-streaming responses:**

| Canonical operation name | Response format | Extraction path |
| --- | --- | --- |
| `openai.chat_completions` | `{"usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `openai.responses`, `openai.responses.compact` | `{"usage": {"input_tokens": N, "output_tokens": N, "total_tokens": N}}` plus detail objects when present | Base input and output subtract cached and reasoning detail counts; provider `total_tokens` stays authoritative |
| `openai.responses.input_tokens` | `{"input_tokens": N, "total_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null. Pricing selects the applicable input card and charges observed input/cache components only; absent output is zero for this operation. |
| `openai.images.generations`, `openai.images.edits` | `{"usage": {"input_tokens": N, "output_tokens": N, "total_tokens": N, "input_tokens_details": {...}}}` | Base input and output are taken whole with no subtraction; there is no cache or reasoning component, and `input_tokens_details` is a breakdown rather than a separate billing component, so its `text_tokens`/`image_tokens` split is not persisted |
| `anthropic.messages` | `{"usage": {"input_tokens": N, "cache_read_input_tokens": N, "cache_creation_input_tokens": N, "output_tokens": N}}` | Base input, cache-read input, cache-creation input, and base output stay separate; total is derived when upstream omits it |
| `anthropic.count_tokens` | `{"input_tokens": N}` | Top-level count as base `input_tokens` and `total_tokens`; `output_tokens` = null. Pricing selects the applicable input card and charges observed input/cache components only; absent output is zero for this operation. |
| `gemini.generate_content`, `gemini.stream_generate_content` when handled as non-stream JSON | `{"usageMetadata": {"promptTokenCount": N, "cachedContentTokenCount": N, "candidatesTokenCount": N, "thoughtsTokenCount": N, "totalTokenCount": N}}` | Base input subtracts cache-read input because `cachedContentTokenCount` is part of `promptTokenCount`; base output is `candidatesTokenCount` as reported and reasoning output is `thoughtsTokenCount` as reported, because Google defines `totalTokenCount` as prompt + thoughts + response candidates (three parallel terms); provider `totalTokenCount` stays authoritative |
| `gemini.count_tokens` | `{"totalTokens": N}` or `{"total_tokens": N}`, with optional `cachedContentTokenCount` or `cache_read_input_tokens` | Top-level count as base `input_tokens` and `total_tokens`; optional cached count is retained as cache-read input; `output_tokens` = null. Pricing selects the applicable input card and charges observed input/cache components only; absent output is zero for this operation. |

**Streaming responses:**
The gateway accumulates SSE chunks during streaming and extracts usage from operation-specific terminal events:

| Canonical operation name | Usage events | Extraction |
| --- | --- | --- |
| `openai.chat_completions` | Final usage chunk before `[DONE]` | Same canonical disjoint fields as non-streaming usage |
| `openai.responses` | `response.completed` event with a `usage` object when provided by upstream | Same canonical disjoint fields as non-streaming usage |
| `openai.images.generations` | `image_generation.completed` event with a `usage` object | Same canonical disjoint fields as non-streaming image usage; `image_generation.partial_image` events carry no usage |
| `openai.images.edits` | `image_edit.completed` event with a `usage` object | Same canonical disjoint fields as non-streaming image usage; `image_edit.partial_image` events carry no usage |
| `anthropic.messages` | `message_start` usage plus cumulative `message_delta.usage.output_tokens` | Base input, cache-read input, cache-creation input, and final base output stay separate |
| `gemini.stream_generate_content` | Stream terminal or final chunk carrying `usageMetadata` | Same canonical disjoint fields as Gemini non-stream `usageMetadata` |

Image operations are priced through the ordinary `PER_1M` token pipeline because GPT image models return a token `usage` object. The pricing template has no per-component slot for the text/image input split, so both kinds of input token are priced at the single `input_price`; an image template's `input_price` must therefore be authored as a weighted rate. Models that return no usage at all (the DALL-E family) record `MISSING_TOKEN_USAGE` and stay unpriced.

If token data cannot be extracted from the provider response, runtime usage token fields are logged as `null`. Completed streams that lack required usage keep `MISSING_TOKEN_USAGE`; interrupted or no-terminal streams with missing required tokens use `STREAM_USAGE_UNAVAILABLE` when their classified stream outcome made terminal usage unavailable. Aggregate `cached_tokens` is derived-only from cache-read plus cache-creation input tokens and is not a persisted runtime component.

A Gemini `usageMetadata` payload whose provider total falls below the canonical disjoint component sum, or that yields a negative component, is rejected rather than silently treated as absent: the telemetry envelope records `usage_source = normalization_rejected` and the backend emits a `runtime usage normalization rejected upstream payload` warning.

---
