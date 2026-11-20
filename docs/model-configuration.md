# Model and Terminal Target Configuration

Shipped behavior of model configuration, endpoints, Terminal Targets, header and parameter injection, and profile scope. Wire fields are in the [Management API](management-api.md); tables are in the [data model](data-model.md); routing and failover behavior is in [load balancing](load-balancing.md) and [runtime routing](runtime-routing.md).

## Product Behavior

### Multi-Family Proxy

- Operation-registered proxy support for OpenAI Chat Completions, Responses/input-token/compact, image generations/edits, Anthropic Messages/count-token, and Gemini generate/stream/count-token operations
- Supports native streaming (SSE) and non-streaming generation responses; token-count operations and Responses compact remain non-streaming
- Preserves native request/response formats per API family
- Runtime compatibility is fixed by `api_family`
- `GET /v1/models` is local and returns the OpenAI `object`/`data` list for enabled OpenAI models whose `model_configs.direct_request_enabled` is true; query parameters do not select an alternate response shape

### Model Configuration

- Map each model to a fixed runtime `api_family`
- OpenAI models that accept text carry an `openai_accepted_format` of `responses_only`, `chat_completions_only`, or `dual_native`; strict mode equality requires every access target (model or Terminal Target) to use the identical mode, including matching `null` for image-only models
- OpenAI models independently carry an `openai_image_operations` of `generations`, `edits`, or `generations_and_edits`; an OpenAI model must declare at least one of the two dimensions, so a pure image model such as `gpt-image-2` carries no text mode at all
- For an image-only model, the Terminal Target dialog hides text capability controls and preserves the absent text mode when creating or editing, including a name-only edit. Prefilling from another Terminal Target preserves the destination model's text mode instead of importing the source's capability
- Models expose one ordered `access_targets` list whose rows point to same-family, same-mode models (Model Targets) or model-private Terminal Targets; both types share one global `position` and are type-neutral peers of the same mixed order
- Terminal Targets carry endpoint, costing, health, admission-limit, and auth metadata as model-private endpoint bindings owned by one model; an OpenAI Terminal Target's `openai_text_capability` must equal the owner model's `openai_accepted_format`, while its `openai_image_capability` only has to cover the owner's `openai_image_operations` and may serve more
- Terminal Targets own an explicit persisted `upstream_model_id` (migration `000031_terminal_target_upstream_model_identity.sql`): create-omitted writes the owner model's current `model_id` explicitly to the database, explicit null/blank/over-200-Unicode-character values reject `422`, PATCH omission preserves and the field can never be cleared, model rename never cascades, and batch copy preserves the source value byte-for-byte after the original value's create-time trim. Clients keep addressing the stable entry `model_id`; each upstream attempt sends the selected target's frozen upstream identity instead. The model detail form prefills the owner `model_id` with manual decoupling, composite create supports the field, the access-target display and copy dialog show it, and the Requests surface exposes it as an optional hidden-by-default column plus detail/CSV values that render a reasoned `—` for historical rows instead of inferring one
- Select which access targets are enabled for each model; enabled models require at least one enabled target
- CRUD operations for all configurations are available via REST API

### Default-Profile Endpoints & Terminal Targets

- **Endpoints** are profile-scoped credential objects containing a name, base URL, and API key.
- **Models** carry fixed `api_family` metadata.
- **Terminal Targets** are profile-scoped model routing, costing, and health configurations that reference endpoints in the same profile. They can also carry per-target custom HTTP headers, an optional static JSON request-body parameter overlay, and an optional routing schedule that limits which parts of the week the target may be selected (see [custom HTTP headers](#custom-http-headers-per-terminal-target), [custom request parameters](#custom-request-parameters-per-terminal-target), and [routing schedule](load-balancing.md#routing-schedule-per-terminal-target)).
- Endpoints can be reused across multiple models within the same profile; each Terminal Target keeps its own header and request-parameter configuration.
- Deleting an endpoint is blocked if any Terminal Targets in that profile still reference it.

### Custom HTTP Headers per Terminal Target

Allow users to configure custom HTTP headers on individual Terminal Targets. These headers are appended to upstream proxy requests.

- Custom headers are configured during Terminal Target creation or editing
- Headers are stored as a JSON object
- Custom headers can override ordinary forwarded headers, but they cannot override Prism-controlled authentication or provider-version headers and cannot bypass the final Header Blocklist

### Custom Request Parameters per Terminal Target

Allow operators to attach an optional static top-level JSON object (`custom_request_parameters`) to each Terminal Target. Prism applies the object as a top-level shallow overlay on the provider-native upstream request body of every actual attempt that selects that Connection, after the provider adapter completes model/path rewrite.

- Configured during Terminal Target creation or editing as “服务附加设置”: named text, number, switch, empty-value, group, and list fields preserve the existing typed values without requiring JSON authoring. The editor validates incomplete names and values before the unchanged backend validator; service-provided setting names stay exact.
- Overlay rules: non-conflicting client top-level fields are preserved; matching top-level keys are replaced wholesale (nested objects are never recursively merged); configured `null` values are sent as literal JSON null; there is no delete-member syntax
- The same Terminal Target configuration applies to all eleven provider-forwarded POST operations, including OpenAI image generations/edits; local `GET /v1/models` never applies it
- `model`, `models`, `stream`, `messages`, `input`, `contents`, `instructions`, `system`, and `systemInstruction` are protected top-level fields and are rejected at save time; the management API returns 422 with a locatable `field`/`path`/`reason`/`limit` envelope
- Constraints: root must be an object, compact encoding ≤ 64 KiB, nesting depth ≤ 16, total object members ≤ 256, integers within the ECMAScript safe-integer range, no non-finite numbers, no duplicate or blank keys
- Unconfigured, explicit `null`, and `{}` all mean “no parameters” and keep the existing request bytes, fast paths, headers, auth, pricing, audit, and response behavior unchanged
- When any planned candidate carries a configuration, Prism buffers and validates the ingress body as a JSON object, materializes a per-attempt merged body (also for Gemini path-streaming, which switches from the request-body streaming fast path to the buffered path), and re-extracts generation-parameter telemetry from each attempt's final body
- Body-dependent headers (`Content-Encoding`, `Content-MD5`, `Digest`, `Content-Digest`) are stripped from client, auth-extra, and `custom_headers` sources whenever an overlay re-encodes the body; `Content-Length` is recomputed
- Error boundaries: non-object ingress fails with 400 before provider transport; Gemini path-bound operations with a configured candidate reject non-identity `Content-Encoding` with 415; merged bodies over the 20 MiB runtime limit fail with 413; planning-snapshot compilation fails closed on invalid persisted data (cold start fails, hot refresh keeps the last-good snapshot)
- This is plaintext, non-secret configuration: the management API echoes it, and enabling request-body audit capture stores the final merged upstream body (which includes the injected parameters); validation errors and logs never echo the configured values
- Prism does not guess vendors, download provider catalogs, or verify provider slugs; whether an upstream accepts or honors a parameter is the operator's and upstream's responsibility (for example OpenRouter `provider.only` / `provider.order` / `allow_fallbacks`)

### Supported API Families

- The application exclusively supports the shipped OpenAI, Anthropic, and Gemini `api_family` values

### Configurable Header Blocklist

Database-backed header blocklist with CRUD API. Supports exact and prefix match types. System defaults for Cloudflare tunnel metadata, tracing headers, and standard proxy headers. Applied by the Go runtime on every request.

### Frozen Profile Scope

- Prism preserves the `profiles` table and all `profile_id` storage columns for historical attribution and a future unfreeze path.
- Profile-scoped management APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.
- Global management routes stay outside profile scoping. Global routes include auth, auth-setting flows, `GET/PUT /api/settings/log-retention`, destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Profile lifecycle APIs are not exposed in the current management surface.
- Runtime proxy traffic on `/v1/*` and `/v1beta/*` ignores management profile headers and always resolves against frozen Default profile id `1`; `X-Profile-Id` and profile fields remain compatibility/storage attribution only.
- Observability rows (`request_logs`, `audit_logs`) carry immutable `profile_id` attribution for historical correctness.

