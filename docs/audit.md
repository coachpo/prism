# Request Audit

For public query and management endpoints, see the [Observability API](observability-api.md). The [data model](data-model.md) describes retained tables and ownership. [Log retention](log-retention.md) owns how audit rows age out.

## Request Audit Logging

### Audit Concept

Audit logging records request-time provenance without changing routing choices or client-facing response handling. Before the ordinary backend outbox, it applies the fixed safe-diagnostic scrub bottom line (Bearer/Basic/JWT/API-key-like/URL-secret redaction from `safediag`) plus the request-time effective Header Blocklist, using the shared matcher in `internal/domain/safediag`. Canonical sorted `[{name,value}]` header entries retain per-direction scrub provenance. Runtime snapshots load audit policy from `profile_api_family_audit_settings` by profile and model API family, then retain request-time booleans in the telemetry envelope. Materialization creates one audit row for each audited upstream attempt, including failover attempts, and metadata-only requests still create audit metadata when audit is enabled. Body capture is allowed only when audit is enabled for that family; request bodies may be captured per attempted upstream request while response body capture is associated with the final attempt.

Captured bodies persist as **BYTEA byte-exact stored prefixes** (`request_body`/`response_body`, migration `000010_request_logs_audit_observability`); the telemetry envelope carries `[]byte` (base64 JSON) so bytes never round-trip through TEXT. Capture is bounded by a per-body 4 MiB cap and per-ingress budgets: request copies 12 MiB, final response 4 MiB, scrubbed header blocks 64 KiB with 1 MiB per direction (response reserves 64 KiB for the final winner). Each audit row records ingress and per-direction byte counters, typed capture/truncation statuses and the enumerated limit reasons; allocation follows immutable launch order and budget exhaustion only stops extra audit storage, never proxy traffic.

Image operations redact their audit bodies before persistence. `b64_json`, `image`, and `mask` values are replaced with `[redacted image bytes]`, and an `image_url` value is redacted only when it is an inline `data:` URL; short https URLs and uploaded file ids stay readable so an audited edit request remains reproducible. Streamed image responses are redacted event by event so the event names, partial-image indices, and the terminal `usage` object survive. A body that does not parse — a stream truncated mid-event, or a JSON document cut off by the 4 MiB cap — is replaced wholesale rather than stored partially. The capture counters (`observed`, `stored`, `truncated`) keep describing the bytes seen on the wire: redaction is a separate transformation applied on the way to persistence.

`GET /api/audit/logs/{id}/body/request` and `/body/response` return the stored bytes as `attachment`, `application/octet-stream`, `nosniff`, `private, no-store`, and `Content-Security-Policy: sandbox`, with a fixed safe `.bin` filename and exact `Content-Length`. A missing audit row returns `404`, disabled request-time audit returns `409`, and evidence below the published retention floor returns `410 audit_evidence_revoked`; a retained row with no captured body currently returns an empty body. The audit list accepts `anchor_id`: when the anchored row falls outside the first page the first response carries it exactly once as `anchor_item`, and in-page or unknown anchors emit no `anchor_item`. Successful Requests/Audit list responses carry a same-snapshot `coverage` projection (`known|legacy_unknown`, requested/effective/retained bounds, completeness, gaps and source revision) from the owning actual-coverage read model. Requests attempts/chains and CSV export accept `time_range=1h|6h|24h|7d|30d|all|custom`; `all` and the effective SQL lower bound use owner materialization, while a dirty/stale/gapped projection is explicit `legacy_unknown`. Windows reaching before the retention floor or actual retained intersection never enter true-empty.

### Audit Flow

```
Client -> supported runtime operation
  -> Operation registry and planner resolve the attempt
  -> ProxyService forwards request to upstream
  -> Runtime captures audit metadata in the telemetry envelope
  -> Durable telemetry materialization, when reached:
       -> One audit row for each audited upstream attempt
       -> Persist the already-scrubbed request/response headers and connection metadata snapshots
       -> Link weakly to materialized request-log metadata when available
       -> Store immutable profile_id attribution
       -> Store request bodies when body capture is enabled
       -> Store the response body only for the final attempt
```

### Non-Interference Guarantees

- Audit policy and body capture never change routing, target selection, or client-facing response adaptation.
- Audit materialization occurs with durable runtime telemetry rather than inline `INSERT`s in the proxy handler.
- Telemetry handoff has its own durability rules: buffered successful responses require a completed outbox handoff before response commit; passthrough SSE and non-SSE success require an accepted outbox row before first flush and a later final update. Asynchronous captured non-`2xx` and eligible synthetic failure activity can be rejected or abandoned before materialization.

### Redaction

Applied at write time before INSERT:

- Header values are scrubbed with the fixed safe-diagnostic matcher (exact sensitive names plus fragment-sensitive names) and stored as canonical sorted `[{name,value}]` entries with `request_headers_scrub_provenance`/`response_headers_scrub_provenance`.
- Body capture is bounded: 4 MiB per body enforced during the copy, a 12 MiB shared request-body budget per ingress with a separate 4 MiB final-response reservation (16 MiB aggregate), and typed `captured|truncated|omitted_ingress_budget` statuses with observed/stored byte counts.
- Text body capture can contain sensitive user data and remains an explicit request-time setting. Image-operation bodies apply the payload redaction described in [Audit Concept](#audit-concept) before persistence.
- Stored bodies are BYTEA prefixes; raw downloads (`/api/audit/logs/{log_id}/body/request|response`) return the exact stored bytes with attachment/octet-stream/nosniff/no-store/sandbox headers and byte-exact round-trip for invalid UTF-8, NUL, and mid-codepoint truncation.

### Dedicated Audit Detail Page

The request-content view is reached from request investigation at `/observe/requests/:requestId/audit`, retaining the existing audit API and route identity. It shows the request outcome, readable messages extracted from recognized stored request/response formats, natural-language tool activity, and capture limitations. Raw headers, JSON/SSE envelopes, tool arguments/results, and raw-download controls are absent from the UI. Invalid-UTF-8, binary, unsupported, missing, and truncated bodies keep explicit explanations and byte/capture evidence instead of manufactured conversation text. The backend still retains and serves its documented audit byte representation; recognized captured text may contain user-provided sensitive content. The overview-only sheet does not load bodies, and the content page preserves the original request-list state through a validated local `return_to` target.

### Upstream Response Encoding

Prism's upstream transport sets `DisableCompression: true`, removes the client `Accept-Encoding` header, and applies no connection or timeout limits. `MaxIdleConnsPerHost` is explicitly unlimited (`math.MaxInt32`) rather than left at Go's default of two idle connections per host; all other connection counts, idle lifetimes, and the client timeout are zero/unlimited after the removal of the `runtime.transport` config section. Prism neither asks Go to automatically decompress upstream responses nor conditionally decompresses them for audit capture. Audit captures the response bytes actually read from the native upstream response.


## Product Behavior

### Request Audit Logging

Full HTTP request/response recording for proxied requests, stored in the database for auditing and debugging.

#### Request-Time Audit Flags

- Audit rows store `audit_enabled_at_request` and `audit_capture_bodies_at_request` as request-time provenance
- Audit behavior does not derive runtime compatibility from catalog metadata
- Toggling audit settings affects new requests only

#### What Gets Recorded

For each audited upstream attempt that is materialized:

- **Request**: HTTP method, scrubbed upstream URL, canonical sorted scrubbed request-header entries with provenance, and a BYTEA byte-exact stored request-body prefix.
- **Response**: Scoped HTTP status and response-header snapshot with provenance; captured response bytes when enabled. For a multi-attempt request, response body capture is associated with the final attempt rather than every failed attempt.
- **Metadata**: model and API family, connection identity (connection ID, endpoint base URL, description), scoped status/duration, stream flag, timestamp, link to `request_log`, `row_kind`, attempt facts, ingress byte counters and capture-limit reasons.

#### Sensitive Data Redaction

Audit redaction applies the fixed safe-diagnostic bottom line (Bearer/Basic/JWT/API-key-like/key=value/URL-secret redaction from `safediag`) plus request-time Header Blocklist additions before the ordinary backend outbox. Sensitive header names and credential-shaped values are stored as `[REDACTED]`; legacy rows are marked `legacy_all_values_redacted`/`legacy_rescrubbed`. Captured request or response bodies are the authorized raw-body exception and may contain prompt/PII/secrets; that exception never extends to failure diagnostics, ordinary telemetry, or headers. Body capture is bounded at 4 MiB per body, with a 12 MiB shared request budget and 4 MiB response reservation per ingress, and raw downloads serve exact stored bytes with typed capture/truncation callouts.

OpenAI image operations additionally replace inline and generated image bytes before audit storage, including each SSE event. Image audit bodies therefore preserve the redacted representation, not the original media payload; raw downloads still return the exact bytes that were stored.

Failure diagnostics reuse the same redaction rules without the raw-body exception. The persisted `error_detail` and the independent `stream_error_detail` are produced by scrubbing upstream failure text through the shared `safediag` rules plus the request-time Header Blocklist, then truncating on UTF-8 code-point boundaries; each row records whether the value was redacted and whether it was truncated. Routing failures and admission failures persist the same scrubbed projection, so a diagnostic row never carries unscrubbed upstream text. Provider-supplied error codes are adopted only after they survive the same trim/scrub pass and match the stable code grammar.

#### Routing And Delivery Boundaries

Audit policy does not change model selection, Terminal Target selection, or client-facing response handling:

- A successful provider-forwarded `2xx` response requires the applicable durable telemetry handoff before Prism commits or first flushes the response. If that handoff fails before client-visible output, Prism returns a runtime observability error instead of the successful response.
- After handoff, background materialization and non-required side-effect failures are isolated from the proxied response path.
- Unsupported or wrong-method runtime registry rejections do not enter telemetry or audit handling.

#### Audit Inspection (Frontend)

- Saved conversation content opens as “请求内容” from request investigation at `/observe/requests/:requestId/audit`; this remains a request-scoped page rather than a standalone audit browser
- Every successfully loaded request detail sheet provides an entry point to the dedicated audit page; the sheet itself remains overview-only and does not fetch audit payloads
- The dedicated page first loads the request detail, then shows request-time audit state as disabled, metadata-only, or full capture and resolves audit rows with `request_log_id`

#### Body Size Limits And Raw Download

- When body capture is enabled, Prism stores captured request bodies for audited attempts and the captured response body for the final attempt only.
- Capture is bounded: per-body 4 MiB cap; per ingress request copies 12 MiB and final response 4 MiB; scrubbed header blocks 64 KiB with 1 MiB per direction (response reserves 64 KiB for the final winner). Allocation follows immutable launch order; budget exhaustion only stops extra audit storage, never proxy traffic.
- Failure diagnostics are bounded independently of body capture and apply whether or not audit is enabled: the raw upstream error sample is capped at 32 KiB per attempt and stays in memory for diagnosis only — it never enters the outbox or any table — while the persisted `error_detail` and `stream_error_detail` are capped at 4 KiB each after scrubbing. These caps are code-fixed constants rather than settings; changing one requires syncing this document, the API metadata, and the runtime tests.
- Each row records `ingress_audit_bytes_observed/stored/truncated`, per-direction header byte counters, and capture/header limit reasons (`none|body_cap|ingress_budget|both|handoff_budget|omitted_ingress_budget|omitted_handoff_budget|block_cap`); truncated rows keep a permanent prefix-only callout, never a fake empty.
- Bodies persist as BYTEA stored prefixes; raw downloads (`GET /api/audit/logs/{id}/body/request` and `/body/response`) return the exact stored prefix as `attachment` / `application/octet-stream` / `nosniff` / `private, no-store` with a safe attachment filename. The dashboard shows capture limits and readable conversation text for recognized valid-UTF-8 content; it does not expose headers, raw JSON/SSE views, or raw-body downloads. These byte-download endpoints remain backend API capabilities.
- Audit list/detail/preview responses are `private, no-store`; the audit list also carries a non-null `known|legacy_unknown` coverage projection, and an anchored row outside the first page arrives exactly once as `anchor_item`.

