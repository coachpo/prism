# Runtime Routing

This document follows a request through the exact operation match, frozen routing plan, target selection, and provider attempt. See the [Runtime API](runtime-api.md) for supported routes and wire behavior.

## 3. Request Flow

Prism is proxy-first. It forwards only the provider-native operations registered in the runtime operation catalog, and it is not a full OpenAI, Anthropic, or Gemini API clone.

Global CORS handling runs before the runtime branch. The runtime branch then applies HTTP proxy admission, runtime proxy-key authentication, and the operation registry in that order. The operation registry is the ingress contract inside the authenticated runtime handler. Each supported operation declares an exact HTTP method, path template, API family, model-binding source, streaming classification, canonical operation name, and `HookCollectionID`; it does not declare a provider adapter. The current canonical operation names are `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, `openai.images.edits`, `anthropic.messages`, `anthropic.count_tokens`, `gemini.generate_content`, `gemini.stream_generate_content`, and `gemini.count_tokens`. Requests that do not match that registry are rejected before body reads, planning, provider transport, telemetry, audit, feedback, or durable side effects.

`GET /v1/models` is the exception: `openai.models` branches to the local models-list handler before provider request-body handling, planning, or provider execution core, and lists only enabled OpenAI models whose `direct_request_enabled` bit is true. Every other registered proxy operation enters the shared runtime and gateway path: it resolves the exact client model ID through the direct-entry lookup, then resolves ordered access targets (including enabled non-entry Model Target nodes), applies the attached Ban Policy strategy, claims local attempt state, builds an upstream request, and hands activity to telemetry seams. A non-entry client ID therefore returns the existing 404 before provider transport or downstream side effects, while a direct parent can recursively route to that node. The provider adapter is selected during planned-upstream request construction, not registry resolution. Request, non-stream response, and stream hooks are looked up by `HookCollectionID`, allowing related operations such as token counting or compact Responses to use hook collections different from their canonical operation names. Those hooks own generation extraction and stream intent, non-stream parsing and token usage, and stream terminal classification and usage merge respectively.

OpenAI Chat Completions and Responses are operation-native and mode-strict. Planning requires the model's `openai_accepted_format` and the selected connection's `openai_text_capability` (`responses_only`, `chat_completions_only`, or `dual_native`) to be exactly equal: `dual_native`, `chat_completions_only`, and `responses_only` may connect only to the identical mode (3×3 equality matrix, diagonal only). Incompatible terminal attempts are skipped in authored order so the next target can be tried; if every otherwise eligible attempt is mode-incompatible, Prism returns the typed `400 openai_request_translation_unsupported` response before provider transport. Current native attempts record `operation_translation_mode = "none"`; the columns and stats reads remain readable for historical rows. Responses adjunct operations (`openai.responses.input_tokens`, `openai.responses.compact`) require responses-capable targets, which mode equality guarantees for `responses_only` and `dual_native`. Management write paths enforce the same equality: authoring a cross-mode relation returns `422 target_openai_mode_mismatch` (including disabled/inactive relations), and changing a persisted model mode or connection capability that would break an existing relation returns `409`. The read-only `openaimodecheck.Check` scan validates persisted relations during startup after migration, pricing-transition, and observability-upgrade work; violations prevent later startup steps and serving. Diagnostic `full|partial|none` operation-set coverage does not relax the text equality gate.

Runtime observability stores canonical disjoint token components. Base input, cache-read input, cache-creation input, base output, and reasoning output are separate dimensions, while provider totals remain authoritative when supplied. Pricing selects one complete typed card (`standard`, `tier_base`/`tier_above`, or `peak`/`offpeak`) from the frozen ingress planning clock before FX and arithmetic. Explicit `"0"` component prices mean configured free pricing instead of a missing-price condition.

Runtime pricing ownership is split by responsibility: `backend/internal/httpapi/runtime/runtime_pricing.go` owns the runtime pricing result and currency projection, `runtime_pricing_core.go` owns the shared five-component exact arithmetic, and `runtime_pricing_tier.go` owns the pure single-threshold tier selection. The tier selector uses the normalized disjoint input basis `InputTokens + CacheReadInputTokens + CacheCreationInputTokens`, applies the tier only when the basis is strictly greater than `tier_input_tokens_above`, and never changes aggregate read-model math. The additive tier shape belongs to `backend/migrations/000022_pricing_input_tier.sql`; the typed pricing-card and peak/valley window shape belongs to the fresh-only `backend/migrations/000023_pricing_template_kind_cards.sql`.

Terminal Target `openai_text_capability` remains connection-owned metadata used by strict OpenAI text mode-equality checks. Model-owned capability authoring, context-window preflight filtering, overflow-promotion routing, and sibling-operation translation have been removed; ordinary strategy selection now uses explicit Ban Policy routing families.

### 3.1 Runtime Request With Private Connection Target

The canonical durable identity is created by `newRuntimeIngressContext` in `backend/internal/httpapi/runtime/ingress.go` after operation acceptance. Before request-body handling or provider execution, the response writer is bound to that same UUID, so `X-Prism-Ingress-Request-Id` identifies the retained request/usage/audit group when telemetry is recorded. Pre-operation rejection and the local models-list response retain a server-generated branch token and do not create durable request records. A response ID does not itself guarantee that telemetry has materialized; the frontend requires an exact match and labels any broader browsing fallback.

```
Client -> POST /v1/chat/completions {model: "gpt-4o"}
  -> Operation registry resolves `openai.chat_completions` and its HookCollectionID
  -> Shared core resolves against frozen Default profile id `1` at request start
  -> Gateway assigns the accepted-operation UUID used for durable request/usage/audit grouping
  -> Request setup resolves the requested model and its ordered access targets in frozen Default profile id `1` scope
  -> Planner reaches the model's Terminal Target, applies the attached explicit Ban Policy strategy, and checks admission counters plus retry-window state
  -> Planned request construction selects the provider adapter and builds the selected upstream request
  -> Executor claims the primary attempt lease and forwards that request to the selected endpoint
  -> Upstream responds with JSON
  -> Gateway returns JSON to client, releases any non-stream lease, commits durable telemetry on the applicable success path, and feeds the outcome back into local runtime routing state
```

### 3.2 Runtime Request Through Model Access Targets

```
Client -> POST /v1/messages {model: "claude-sonnet-4-5"}
  -> Operation registry resolves `anthropic.messages` and its HookCollectionID
  -> Shared core resolves against frozen Default profile id `1`
  -> Resolver orders the model's enabled mixed access-target rows once by (position, id)
  -> The attached strategy shapes the effective peer sequence: single keeps only the first row,
     fill-first walks the mixed order, round-robin rotates the direct mixed rows
  -> Each peer row resolves by type: a Terminal row yields one attempt; a Model row recursively
     resolves through the child model's own strategy and contributes one contiguous block
  -> Candidate-local misses (zero-leaf child, unavailable connection, operation incompatibility,
     routing window closed)
     skip to the next effective peer; cycle/depth and missing-strategy errors fail closed
  -> Executor plans attempts against Terminal Targets
  -> Upstream responds; eventual request history keeps model_id as the requested model and resolved_target_model_id as the final target model for each materialized attempt
  -> Gateway returns response to client
```

Model Target rows and Terminal Target rows are type-neutral peers: there is no model-aggregate-first tier and no direct-terminal fallback tier. If at least one effective peer produces compatible attempts, planning succeeds and candidate-local misses are bypassed; otherwise the first compatibility miss in effective order is returned, or the ordinary no-eligible `503` when no peer contributes.

### 3.3 Runtime Request (Streaming)

```
Client -> POST /v1/chat/completions {model: "gpt-4o", stream: true}
  -> Operation registry resolves `openai.chat_completions`; its request hook determines stream intent
  -> Shared core resolves against frozen Default profile id `1`
  -> Gateway assigns the accepted-operation UUID used for durable request/usage/audit grouping
  -> Access-target resolution, route planning, adapter request build, and admission finish before downstream commit
  -> Executor claims a streaming lease before opening the upstream stream
  -> ProxyService opens streaming connection to the selected upstream endpoint
  -> While a later candidate remains, a `2xx` SSE response is read up to its first event (at most 32 KiB) before selection; a provider error first event reroutes to the next candidate, otherwise the buffered head is replayed unchanged
  -> SSE chunks stream back to the client after provider-adapter stream classification allows the operation
  -> Internal buffering is automatic for rewrite or hook-safety cases before downstream commit
  -> First downstream byte/event commits the stream boundary
  -> After commit: no retry, redirect, or hedge replay can start
  -> On stream finalization or cancellation: release the stream lease, finalize the accepted telemetry outbox payload when possible, and record runtime feedback
```

Stream-start error reroute (`stream_start_inspection.go`): a provider error first event is an SSE `error` event, or a JSON payload that declares `"type": "error"` or carries a non-null top-level `error` member. Prism treats it as an in-band rejection of this request alone, so the executor closes that upstream and reroutes to the next candidate like a `reroute_status_codes` status: no runtime failure feedback (no retry-window write, no retry or ban counting), and the next launch carries `attempt_trigger = reroute`. No status code gates this, and unlike a reroute status the abandoned stream is not kept as the ingress's fallback response. The abandoned attempt persists as `attempt_result = stream_error`, `stream_outcome = provider_incomplete`, `failure_stage = stream`, and `success_flag = false`. Its error code is the recognized provider code, with `stream_provider_incomplete` as the fallback. The last candidate is not inspected, so its stream, including an error first event, passes through unchanged. While a later candidate remains, the client receives response headers only after the upstream's first event.

Streaming OpenAI usage instrumentation: for `openai.chat_completions` requests whose body sets `stream: true`, the OpenAI adapter injects `stream_options.include_usage = true` into the upstream body before transport. OpenAI-compatible upstreams emit the final usage chunk only when the caller asks for it, so without the injection every streaming attempt persists NULL token components and `pricing_status = unpriced` / `unpriced_reason = MISSING_TOKEN_USAGE`. Caller intent wins: a client-supplied `stream_options` object that already declares `include_usage` is forwarded unchanged; a missing key or an explicit JSON `null` is treated as unset and receives the injected object. The injection is scoped to Chat Completions - `openai.responses` reports usage in its `response.completed` event, and Anthropic and Gemini streams carry usage natively, so none of them are modified. The injected upstream body is what audit body capture stores, and the extra `choices: []` usage chunk is forwarded to the client verbatim.

### 3.4 API Family Routing

| API family            | Canonical operation names                       | Supported Prism operation paths                    | Upstream path                                      | Auth header                                          |
| --------------------- | ----------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | ---------------------------------------------------- |
| OpenAI                | `openai.models`, `openai.chat_completions`, `openai.responses`, `openai.responses.input_tokens`, `openai.responses.compact`, `openai.images.generations`, `openai.images.edits` | `GET /v1/models`, `POST /v1/chat/completions`, `POST /v1/responses`, `POST /v1/responses/input_tokens`, `POST /v1/responses/compact`, `POST /v1/images/generations`, `POST /v1/images/edits` | Local OpenAI list for `GET /v1/models`; otherwise same path under `{base_url}` | `Authorization: Bearer {key}`                        |
| Anthropic             | `anthropic.messages`, `anthropic.count_tokens`  | `POST /v1/messages`, `POST /v1/messages/count_tokens` | Same path under `{base_url}` | `x-api-key` set to `{key}` plus `anthropic-version` set to `2023-06-01` |
| Gemini                | `gemini.generate_content`, `gemini.stream_generate_content`, `gemini.count_tokens` | `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent`, `POST /v1beta/models/{model}:countTokens` | Same path under `{base_url}` | `Authorization: Bearer {key}` by default (`auth_type` unset or `gemini`, for Gemini-compatible gateways and OAuth access tokens); `x-goog-api-key: {key}` when `auth_type` is `gemini_api_key`, which is what Google's official generativelanguage endpoint requires for API keys |

The endpoint verify probe uses the same auth profile as the runtime: a verified endpoint is verified under the exact credential scheme (`auth_type`) it will be used with.

OpenAI runtime support is limited to the registered local models list, Chat Completions, Responses generation/input-token/compact operations, and JSON image generations/edits listed above. Stored Responses object lifecycle APIs, including retrieve, list, delete, and cancel routes, are outside Prism's supported contract.

The local OpenAI models operation returns the OpenAI `object`/`data` response for every request, including requests with query parameters. It reads the frozen Default-profile runtime snapshot and never contacts a configured model provider.

Gemini requests use `/v1beta/models/{model}:...` paths only. Prism rewrites the model ID segment to the selected Terminal Target's frozen `upstream_model_id` before forwarding, independently of the requested or resolved logical model IDs.
For Gemini, `gemini.stream_generate_content` and the `:streamGenerateContent` path are authoritative for stream classification even when the request body omits `stream: true`; `gemini.generate_content` remains non-stream generate content, and `gemini.count_tokens` remains the token-count operation.

Runtime upstream requests capture an immutable bootstrap runtime snapshot at request start. The snapshot includes an HTTP client built without any connection or timeout limits: the outbound transport sets only `DisableCompression: true` and an explicit unlimited `MaxIdleConnsPerHost` (`math.MaxInt32`), and the client carries no `Timeout`. The `runtime.transport` config section (connection counts, idle lifetimes, request timeout) was removed outright, so no startup setting can re-add upstream limits; `runtime.sideEffects.attemptTimeout` remains the per-attempt budget for scheduler-owned runtime activity handoff work. The removed transport section is rejected with a readable migration error when present in an existing `config.json`.

The startup config projection is built once at process start and is read-only in process: the in-process snapshot used by CORS origin checks, auth TTL and cookie metadata, the runtime upstream HTTP client, and M2/M3 management admission limits. Changing any effective field requires a process restart.

Live startup resources include listener host and port, PostgreSQL URL and six-lane pool budgets, the limit-free runtime upstream HTTP client, runtime side-effect attempt timeout, runtime secret encryption key, auth JWT signing key, CORS, auth TTL and cookie metadata, and management admission. Compatibility-only mail, telemetry, realtime-pool, reset-code, terminal-translation, and state-transfer fields do not create live services.

Runtime compatibility and redirect checks use each model's required `api_family`. Model rows do not depend on catalog metadata for routing, validation, or display. The Models page renders each row's `api_family` metadata directly.

The model-management frontend keeps model CRUD form state in `frontend/src/pages/models/modelFormState.ts`, mixed access-target draft/order rules in `accessTargetFormState.ts`, and detail-side ownership/options/summary projection in `modelAccessTargetProjection.ts`. Connection collection patching is owned by `connectionCollectionState.ts`; `useModelDetailConnectionMutations.ts` only composes the connection submit, lifecycle, reconciliation, and mixed-target mutation owners. The management-only `ModelsDevCatalogPanel.tsx` owns the inset shell, honest read/action state, effective metadata projection, and snapshot-guarded unbind confirmation. `CatalogBindDialog.tsx`, `CatalogRefreshDialog.tsx`, and `CatalogOverrideDialog.tsx` own bind, refresh-preview/commit, and override workflows respectively; none of these facts enters runtime compatibility or routing.

The Models list data boundary keeps concurrent model/strategy bootstrap, revision-keyed reference-cache reads, and server-DTO list patching in `frontend/src/pages/models/useModelsCollection.ts`. `useModelDialogMutations.ts` owns create/edit dialog session fencing, form CRUD, and structured server-error mapping; `useModelEnablementMutations.ts` owns row/bulk enablement; `useModelDeletion.ts` owns deletion; and `useLoadbalanceStrategyDefaults.ts` owns default creation, forced strategy re-read, and create-dialog form reconciliation. `useModelsPageData.ts` only composes those owners and the separate 24-hour metrics hook, preserving the page-facing return contract.

The client model-export composition root `ModelExportPage.tsx` switches between separately mounted Pi and OpenCode feature trees, defaulting to Pi. The Pi host adapter `useModelExportSource.ts` owns the aggregate source query, batch selection and filters, per-row `PiCatalogModelView` mapping, and authoritative source reconciliation; it performs neither Pi mutations nor per-row single-model reads. Model detail uses `useModelDetailPiSource.ts` for `GET /api/models/{id}/pi` and its reconciliation. Both hosts compose `models/catalog/pi/usePiBindingController.ts`, which owns paged directory search plus bind/refresh/override/unbind mutations and invokes the caller-supplied reconciliation after success. `useModelExportRender.ts` authors Pi render requests. `useOpenCodeExportSource.ts` owns the separate persisted-metadata snapshot and selection; `useOpenCodeExportRender.ts` authors its OpenCode requests. Both render hosts compose `useExportRenderSession.ts` for cancellation, request-generation checks, ephemeral credentials/results and stale-source handling, and reuse `exportDestination.ts` for gateway-origin validation. Target switches unmount the former tree and clear its query entry; closing, switching or leaving aborts work and discards late results. `ExportResultSheet.tsx` owns exact-byte delivery and target-specific provider fragments; `OpenCodeMetadataDetails.tsx` presents the source/override/final metadata projection. The shared dialogs and evidence components live under `models/catalog/pi/`; `PiBindingCell.tsx` and `PiDevCatalogPanel.tsx` are host presentation adapters, while `ModelExportPage.tsx` and `ExternalCatalogSourcesSection.tsx` remain composition roots.

### 3.5 Management API Profile Scoping

- Prism keeps one route-class matrix:
  - Global management routes omit `X-Profile-Id`.
  - Profile-scoped management routes accept `X-Profile-Id`, but the backend ignores its value and resolves against Default profile id `1`.
  - Supported runtime operations under `/v1` and `/v1beta` ignore management overrides and always resolve against frozen Default profile id `1`.
- Global management routes include `/api/auth/*`, auth and proxy-key settings under `/api/settings/auth*`, `GET/PUT /api/settings/log-retention`, retention destructive preflights and manual jobs under `/api/maintenance/log-retention/*`, and global retention job list/detail/cancel under `/api/management/jobs*`.
- Multi-profile management is frozen. Profile-scoped management reads and writes are pinned to Default id `1`; runtime routing still loads the published Default-profile runtime snapshot.
- Scope-control errors return stable `code` values plus human-readable `detail` text.
- Supported runtime operations always resolve against frozen Default profile id `1` and ignore override headers.

The protected frontend shell derives sidebar destinations and breadcrumbs from the route metadata in `frontend/src/components/layout/app-layout/useShellNavigation.ts`, and persists only the desktop sidebar collapse preference in localStorage. Mobile drawer state remains transient browser UI state.

The `/route/endpoints` feature keeps its lifecycle owners explicit: `useEndpointReferenceSummaries.ts` owns bounded batch summary hydration and fresh/stale/error replacement, `useEndpointReferenceDetails.ts` owns lazy per-Endpoint snapshots and opaque-cursor pagination, and `useEndpointReferences.ts` owns only their shared generation fence. `useEndpointList.ts` owns Endpoint cache load, visible-ID reference reconciliation, search, and reference-derived filter/sort; `useEndpointMutations.ts` composes the resource-specific form, delete, orphan-cleanup, duplicate, and attach lifecycles without owning their independent state. `EndpointDialog.tsx` separates saved state from optional verification; `AttachToModelDialog.tsx` offers an existing model or `/route/models?action=create&endpoint_id=...` for a preselected new-model flow. The endpoint route also accepts `endpoint_id` for a request-recovery edit entry.

The model flow keeps structured service request settings in `frontend/src/pages/model-detail/RequestSettingFields.tsx` and `structuredRequestSettings.ts`, composed by `ConnectionCustomRequestParametersEditor.tsx`; the existing validator and JSON wire contract remain authoritative. `frontend/src/features/models/detail/ModelClientConnectionCard.tsx` presents the effective client address, exact client model name, key setup, and model-preselected verification. `frontend/src/features/runtime-self-test/effectiveOrigin.ts` remains the common origin owner, and `/system/proxy-keys?action=verify&model_id=...` carries only the model identity, never a secret.

The pricing feature owns tier form state and presentation in `frontend/src/features/pricing/pricingTierSchema.ts` and `PricingTierFields.tsx`; `frontend/src/features/pricing/pricingSchemas.ts` remains the single form-state/schema/validation/payload contract, while `pricingDeletion.ts` owns delete blocking and `pricingUsage.ts` owns usage/reference response projection. Request-log tier explanation stays in `frontend/src/pages/request-logs/pricingExplanation.ts` and the overview detail tab, while currency-migration blocking is rendered by the existing billing-currency dialog.

The loadbalance, pricing, and proxy-key feature data contracts keep independent state owners behind thin page coordinators.

`frontend/src/features/loadbalance/useBanPolicyStrategyCollection.ts` owns strategy collection/detail reads and revision-backed fragments, `useBanPolicyMutations.ts` owns CRUD/default mutation state, and `useStrategyImpactPager.ts` owns per-strategy cursor merge and stale/error state.

Pricing uses `usePricingTemplateCollection.ts` for the revision cache/list, `usePricingTemplateMutations.ts` for editor impact preflight and create/update/delete CAS workflows, `usePricingTemplateDetailReads.ts` for usage/history reads, and `usePricingImportProtocol.ts` for preview/hash-commit; `usePricingFeatureData.ts` only composes them. `PricingTemplateImportDialog.tsx` reads a selected price file and keeps read/preview failures recoverable; `PricingImportPreview.tsx` presents the explicit commit step. File selection replaces raw JSON authoring in the UI without changing import schemas or hash validation. Price forms use reporting-currency readiness from `ReportingCurrencyContext`; optional blank prices serialize to null and explicit zero remains distinct.

Proxy-key ledger queries/capacity live in `useProxyKeyLedger.ts`, and visible-key usage in `useProxyKeyUsage.ts`; `useProxyKeyCreateMutation.ts`, `useProxyKeyEditMutation.ts`, `useProxyKeyRotateMutation.ts`, and `useProxyKeyDeleteMutation.ts` own the four credential mutation lifecycles, while `useProxyKeyMutations.ts` only composes their page-facing values. `proxyKeyMutationReconciliation.ts` is the explicit cache-patch/invalidation owner; the individual mutation hooks own safe inline error state so failed operations retain their dialog and draft. `useProxyKeySecretSession.ts` remains the only post-handoff owner of a one-time raw secret. The standing access-verification dialog open state remains page presentation state in `ProxyKeysFeaturePage.tsx`.

`frontend/src/pages/proxy-api-keys/ProxyKeyVerifyAccessDialog.tsx` composes model/key selection with the shared `frontend/src/features/runtime-self-test/RuntimeSelfTestPanel.tsx`; the generated-secret dialog reuses that panel without stacking a second self-test dialog. `useRuntimeSelfTest.ts` owns direct-send, cancellation, and independent record-retry state. `selfTestRunner.ts` sends the existing operation-specific request, consumes the response without retaining diagnostic bodies, then reads the exact ingress chain. Only an authoritative finalized summary with a matching detail record supplies execution, final target, and trusted cost; an arbitrary attempt row or HTTP 200 alone cannot establish completion. Received-response, interrupted-receive, cancelled-wait, and finalized outcomes remain separate. Both chain and detail reads forward cancellation. The panel renders localized outcomes and missing-data recovery without raw responses or headers; known micro-unit fees are converted to currency units, and missing fees are never zero-filled.

The request-log frontend keeps attempts and ingress chains in separate read owners: `frontend/src/pages/request-logs/useRequestLogAttempts.ts` owns attempt-list query/replace/generation/filter state, while `useRequestLogIngressChains.ts` owns chain-page replacement, signed cursor navigation, nested row append fencing, and chain-specific state. `requestLogChainProjection.ts` owns the chain-envelope to attempt-row projection and stable-ID dedupe; `useRequestLogsPageData.ts` only combines the active view for `RequestLogsPage.tsx`. `RequestLogsTable.tsx` owns the virtualized attempt table and `ColumnToggleMenu.tsx` owns column visibility presentation.

The dedicated request-log audit page keeps request detail, audit-list cursor pages, and selected audit detail in `useRequestLogAuditRequest.ts`, `useRequestLogAuditList.ts`, and `useRequestLogAuditDetail.ts`; `useDedicatedRequestLogAudit.ts` only composes their public lane states and retry commands. `useRequestLogProxyApiKeyOptions.ts` owns the filter query key/parameters and selected-option prepend, while `FiltersBarPrimaryFilters.tsx` retains filter markup order. `requestLogMetricPresentation.ts` owns the shared cost, token, TTFT, and token-rate value formatting consumed by both `columns.tsx` and the detail overview; table-only status, pricing, and stream semantics remain in `columns.tsx`. `RequestLogAuditPage.tsx` renders the saved-content page and validates its request-list `return_to` navigation. `detail/RequestLogPayloadBlock.tsx` renders only readable conversation and tool-activity notices from the internal protocol parsers. `detail/RequestFailureRecovery.tsx` is shared by the sheet and content page, backed by `requestFailurePresentation.ts` and exact current-model lookup in `useRequestRecoveryModel.ts`; labels live in `frontend/src/i18n/messages/requestRecovery.ts`.

Routing-health events use `useRoutingHealthQueryContext.ts` for the signed window context lifecycle, `useRoutingHealthEventPage.ts` for URL-backed event filters/cursor/detail selection and page reads, and `LoadbalanceEventsPresentation.tsx` plus `LoadbalanceEventRow.tsx` for the event card and row projection. Global Current State uses `useRoutingHealthCurrentStateRead.ts` for read/pagination, `useRoutingHealthCurrentStateReset.ts` for reset commands and post-reset calibration, and `GlobalCurrentStatePresentation.tsx` plus `GlobalCurrentStateRow.tsx` for filters, table, and confirmation dialog. `RoutingHealthTab.tsx` only composes these owners.

The Settings shell uses canonical public URLs with `scope=global|instance` and a section allowlist (`billing-currency`, `timezone`, `audit-privacy`, `header-blocklist`, `client-rules`, `authentication`, `retention`, `manual-cleanup`, `retention-jobs`); the legacy `tab` query value is dropped during canonicalization. `frontend/src/pages/settings/settingsNavigation.ts` owns those scope and section facts. The visible `显示与记录` group (`scope=global`) keeps billing and reporting currency, timezone, audit & privacy, and config-rule flows; the visible `访问与数据` group (`scope=instance`) owns authentication and operator account, automatic retention policy with actual coverage, manual cleanup, and the retention job center. Audit resource state is split across API-family audit settings, Header Blocklist, and User-Agent Client Rules owners; retention state is split across policy, manual-cleanup, and job lifecycles, with `retentionProtocol.ts` owning their shared protocol invariants. `costing/costingForm.ts`, `currencyMigrationProtocol.ts`, `manualCleanup.ts`, `sections/authentication/authenticationPassword.ts`, and `frontend/src/lib/timezone.ts` own their respective form, migration, cleanup, password, and timezone projections. Normal log retention applies across all profiles; list and detail APIs are pinned to Default id `1`. `retentionPresentation.ts` maps dataset and warning values into user-facing descriptions, while the job and currency components present outcomes and limitations rather than generation/epoch or internal checkpoint fields. UTC cutoff, irreversible deletion, estimated coverage, delayed reclamation, and the effect on active queries remain visible; auth, retention, and currency-migration protocols are unchanged.

### 3.6 Custom Header Injection

When a connection has `custom_headers` configured, they are injected into the upstream request after all other headers:

```
build_upstream_headers():
  1. Start with the caller's headers, withholding only hop-by-hop headers and host, transport-controlled headers (accept-encoding, content-length), the caller's own credential headers (authorization, x-api-key, x-goog-api-key), and names the provider auth profile controls
  2. Apply blocklist sanitization to client-supplied headers
  3. Add api-family auth headers
  4. Add api-family extra headers (e.g., anthropic-version)
  5. Apply connection custom_headers (from `connections.custom_headers` JSON), skipping proxy-controlled auth/version header names
     -> Same-name ordinary headers from earlier steps are overwritten
  6. Apply final blocklist pass (with api-family auth/version headers protected)
     -> Blocked headers cannot be reintroduced by custom headers
  7. Return final header dict
```

Upstream response headers are filtered by a fixed allowlist before relay: `content-type`/`content-length`/`content-encoding`/`content-disposition`, `cache-control`, `date`, `etag`, `last-modified`, `vary`, `retry-after`, `request-id`, and any header starting with `x-ratelimit-`, `anthropic-ratelimit-`, or `openai-`. `set-cookie`, `server`, vendor-private `x-*` headers and upstream `access-control-*` decisions are never relayed: Prism owns this response.

Outbound client headers are governed by a blocklist, and the Header Blocklist is part of it. The caller's headers reach the upstream as sent, except for four categories that are wrong to copy rather than merely revealing:

| Category | Headers | Why |
| --- | --- | --- |
| Hop-by-hop | `connection`, `keep-alive`, `te`, `trailers`, `transfer-encoding`, `upgrade`, `proxy-authenticate`, `proxy-authorization`, `host` | They describe this connection, not the next one (RFC 7230 §6.1) |
| Transport-controlled | `accept-encoding`, `content-length` | Prism recomputes them; the runtime transport sets `DisableCompression`, so a forwarded `gzip` would return a body the streaming parsers cannot read, breaking usage, token, and output-rate extraction |
| Caller credentials | `authorization`, `x-api-key`, `x-goog-api-key` | Prism authenticates with the connection's own key; forwarding these leaks a client credential to a third party and can collide with the header Prism sets. Checked as a full set, because the auth profile's controlled names only cover the active api family |
| Provider-controlled | The active auth profile's `AuthHeader` and `ExtraHeaders` | Prism sets them downstream in the same pipeline |

A blocklist rather than an allowlist because the goal is that the upstream cannot tell a gateway is there at all. A request stripped down to a handful of protocol headers is itself an anomaly — no real client sends an empty User-Agent and nothing else — so reproducing what the caller sent is what keeps Prism from announcing itself. Which of the remaining headers are undesirable is a deployment question, not a protocol one, and it is answered by the Header Blocklist at runtime rather than by this list at compile time.

`cookie` is withheld by a seeded system rule rather than by the code above, because it is the one caller header whose forwarding is a question about the deployment rather than the protocol. On a shared instance several callers authenticate through one upstream key, and a forwarded `Cookie` would carry one caller's session into requests the others also make. A deployment where the upstream genuinely needs it can disable the rule: system rules cannot be deleted or repatterned, but `enabled` is mutable, so the safe default keeps its escape hatch.

`user-agent` is forwarded, and is worth calling out because it was previously withheld. Withholding it was a negative trade for the gateway-invisibility goal: an empty User-Agent is a stronger signal that a proxy is present than any real client string is, and it made acceptance depend on Prism rather than on the caller. An upstream that only accepts particular User-Agents is still stating a fact about that endpoint, so `connection.custom_headers` remains the way to pin one value for that endpoint; it overrides the forwarded one, and `request_logs.user_agent_overridden` records that the upstream value differed from the caller's. Only when neither the caller nor `custom_headers` supplies one does Prism send an empty User-Agent, to keep net/http from substituting its `Go-http-client` default and naming the gateway's implementation outright.

Withholding a header does not erase it. The audit trail records the headers the caller actually sent unioned with the headers Prism actually forwarded, so an operator can answer both "did my client leak something" and "what did Prism send" from the same record. The Header Blocklist applies twice over: it stops a matching header from being forwarded, and it is fed to the audit scrubber as extra sensitive-name rules so the value is redacted in diagnostics and audit as well. It can only make either stricter — it never weakens the fixed `safediag` bottom line.

Custom headers are a power-user feature and the supported way to pin a header for one upstream regardless of what the caller sent. They can override ordinary forwarded headers, but they cannot override Prism-controlled authentication or provider-version headers and cannot re-add headers blocked by the Header Blocklist. This is enforced by skipping proxy-controlled custom header names and applying the blocklist last in the header construction pipeline.

### 3.7 Custom Request Parameter Overlay

Terminal Targets can carry an optional static top-level JSON object (`connections.custom_request_parameters`). When any planned candidate for a request has a non-empty configuration, Prism must buffer the ingress body, verify it is a JSON object, and materialize a per-attempt upstream body before provider transport:

```
build_planned_terminal_attempts():
  1. For each candidate attempt, build the provider-native upstream request (adapter model/path rewrite)
  2. If the Connection has no custom_request_parameters: use the existing body unchanged
  3. Otherwise overlay O onto the native body B:
       R = deep_copy(B)
       for k in sorted(top_level_keys(O)): R[k] = deep_copy(O[k])
       re-encode R and enforce the 20 MiB effective-body limit
  4. Re-extract the attempt's generation-parameter snapshot from its own final body
  5. Mark the plan as requiring a replayable body (streaming request-body fast path disabled)
```

Overlay rules: non-conflicting client top-level fields are preserved verbatim; matching top-level keys are replaced wholesale (nested objects are never recursively merged); configured `null` is sent as literal JSON null; there is no delete-member syntax. `model`, `models`, `stream`, `messages`, `input`, `contents`, `instructions`, `system`, and `systemInstruction` are protected and can never appear in the configuration.

The overlay runs after adapter body construction, so a Connection that configures `stream_options` replaces the value Prism injected for streaming Chat Completions. Setting `"stream_options": null` restores the pre-injection wire shape for upstreams that reject the field; the overlay has no delete-member syntax, so the key itself is always present once configured.

Body-dependent headers (`Content-Encoding`, `Content-MD5`, `Digest`, `Content-Digest`) are stripped from client headers, provider auth extras, and Connection `custom_headers` whenever an overlay re-encodes the body; `Content-Length` is recomputed from the merged body. Non-identity `Content-Encoding` cannot be re-encoded: Gemini path-bound operations with a configured candidate reject it with `415`, while body-bound OpenAI/Anthropic operations keep the existing `400` malformed-body path.

Buffering decisions:

- If any candidate has non-empty parameters, `requiresReplayableRequestBody` becomes true and `canStreamIncomingRequestBody` returns false; Gemini `streamGenerateContent` then uses the two-phase planning boundary (probe with `rawBody == nil` only decides the replayable requirement, never overlays or 400s; the second full plan reads the body and materializes the merged body).
- With no configured parameters anywhere, the existing request-body streaming fast path is preserved unchanged.
- Each attempt owns an immutable merged body; failover, retry, or hedge candidates never share mutable maps, slices, or buffers.

Fail-closed boundaries: non-object ingress fails with `400` before admission/transport; planning-snapshot compilation fails on invalid persisted data (cold start fails, hot refresh keeps the last-good snapshot); validation errors and logs never echo configuration values. Audit body capture stores each attempt's final merged body; request-log generation parameters are extracted per attempt from its final body.

## 4. Routing Strategies and Runtime Health Signals

### 4.1 Routing policy contract

- Models attach one Default-profile explicit loadbalance strategy.
- Strategies carry the routing family field `legacy_strategy_type` (`single`, `fill-first`, or `round-robin`).
- Strategies also carry explicit Ban Policy fields for failure status codes, retry delay, backoff, jitter, retry-window limits, `cycle_retry_attempt_limit`, `ban_cumulative_retry_attempt_threshold`, ban mode, and ban duration.
- Retry-cycle exhaustion is inclusive at `cycle_retry_attempts >= cycle_retry_attempt_limit`.
- Ban creation is inclusive at `cumulative_retry_attempts >= ban_cumulative_retry_attempt_threshold`; the runtime never derives this threshold from the cycle limit.
- `ban_mode` accepts `off`, `temporary`, and `until_reset`. The `until_reset` mode keeps a terminal target's current connection row banned until the current-state reset endpoint clears it.
- The loadbalance strategies page exposes a `Create Defaults` action that explicitly creates `Default single routing`, `Default fill-first routing`, and `Default round-robin routing` for Default profile id `1`.
- Outbound provider requests carry no connection or timeout limits; `runtime.sideEffects.attemptTimeout` applies to scheduler-owned runtime activity handoff work, not to per-strategy timeout fields.

### 4.2 Runtime execution pipeline

1. The operation registry resolves the exact runtime operation and hook collection before the request body is consumed.
2. Request setup resolves the frozen Default profile id `1` model by exact direct-entry lookup (`DirectModelsByID`), then uses the all-enabled `ModelsByID` graph index for ordered access targets, recursive Model Targets, the attached strategy, and one immutable effective strategy snapshot for the request.
3. Planner and runtime-state helpers use the production `LocalRuntimeStateStore` to build the current candidate set from admission counters, leases, round-robin cursors, and Ban Policy retry-window state. A connection carrying a routing schedule is additionally required to be inside one of its windows at the planning instant; the window check runs after Ban filtering, so a target excluded by its schedule is one already known to be otherwise usable.
4. The shared execution core claims per-attempt local leases before any client-visible bytes are committed; outbound provider calls use the limit-free runtime HTTP client, while scheduler-owned runtime activity handoff work uses `runtime.sideEffects.attemptTimeout`.
5. Operation request, response, and stream hooks interpret provider-native payload details by `HookCollectionID`, not necessarily operation name. Token-count and compact operations use their dedicated collections; passive outcomes feed back into process-local connection state while durable `loadbalance_events` retain transition history and model-policy snapshots, including `cycle_retry_attempt_limit` and `ban_cumulative_retry_attempt_threshold` when Ban Policy evaluation produced the event.

Runtime request bodies are capped at `20 MiB`; oversized supported-operation requests return JSON `413` with `error: "request_body_too_large"` and `limit_bytes` as documented in the [request size contract](runtime-api.md#22-unsupported-routes-and-methods). If all eligible candidates are unavailable inside the current retry window, the gateway returns `503` with routing-availability detail. If all otherwise available candidates are blocked by admission limits, runtime returns `503` with `error: "admission_exhausted"` plus route-reason metadata before upstream transport. If every evaluated candidate was excluded solely by its routing schedule, runtime returns `503` with `terminal_target_schedule_closed` or `terminal_target_schedule_unresolvable` (see §14.2.2A). Exact facade routing, context-window preflight filtering, regex matching, capability-metadata expansion, hidden weight or tier semantics, and response-body model rewriting are retired. Request logs and usage events keep the existing requested-model and resolved-target fields.

### 4.3 Runtime State Residency

Production creates a fresh `LocalRuntimeStateStore` for every process. It owns connection-level Ban Policy state, admission counters, in-flight lease counts, connection round-robin cursors, and model-target round-robin cursors. A normal restart resets this hot state safely.

The SQL tables `routing_connection_runtime_state`, `routing_connection_runtime_leases`, and `loadbalance_round_robin_state` remain compatibility schema, not the runtime hot path. Prism does not rely on startup reconciliation or persistent SQL leases for active routing. Durable `loadbalance_events` remain the historical record of routing transitions and incidents.

## 5. Unified Model Access

### 5.1 Concept

Models resolve through ordered access targets. Public target authoring points only to other same-profile, same-`api_family` models. Terminal Targets are the product-facing model-private endpoint bindings. They remain in `model_access_targets` as internal `connection` ownership and terminal routing edges backed by `connections` rows. Model targets can chain until a Terminal Target is reached, and the runtime records requested model, final target model, selected terminal target, and endpoint for observability. The requested model enters planning by exact `model_id`, not by regex, capability metadata, facade policy, or context-window preflight matching.

### 5.2 Rules

- Access targets must stay in the same profile and same `api_family`.
- Compatibility connection targets are terminal and are presented as Terminal Targets in product-facing routing surfaces.
- Model targets can chain, but cycles and self-targets are rejected.
- Endpoints are reusable. Terminal Targets are created and managed from model detail through model-scoped connection routes while retaining `connections` and `connection_id` compatibility names.
- Every access target carries explicit ordering metadata.
- Access-target `position` orders all rows of both types in one global mixed sequence and is not priority, tier, or weight; Model Target and Terminal Target rows are type-neutral peers and no target type holds a hidden priority.
- `single`, `fill-first`, and `round-robin` run once over the enabled mixed rows. A Model Target row is an atomic parent peer whose child attempts stay one contiguous block; child models keep their own strategy and round-robin cursor.
- Model IDs are unique within a profile.
- The gateway may normalize provider request payloads before forwarding. Every selected Terminal Target carries an explicit persisted `upstream_model_id`, and every upstream attempt rewrites the provider model identity to that target's frozen value instead of the final logical target model ID. Runtime snapshots load only active owner-backed connections; a missing or blank identity on such a row makes the replacement snapshot invalid so hot refresh retains its last-good generation, while orphan connections are excluded. The entry `model_id` the client addressed stays the logical identity for attribution, capability checks, routing, and pricing; only the actual upstream body/path uses the frozen upstream ID. Prism does not rewrite response-body model identity on the client-facing way back out.

Model contracts require `api_family`; OpenAI operations also apply the separate text and image capability gates described in [OpenAI text mode equality](runtime-api.md#22b-openai-native-mode-equality-strict) and [OpenAI image capability](runtime-api.md#22b-img-openai-image-capability-independent-dimension-containment).

### 5.3 Resolution

```
resolve_access(profile_id, model_id):
  config = lookup(profile_id, model_id)
  authored_peers = ordered_enabled_targets(config)   // mixed rows, sorted by (position, id)
  effective_peers = order_for_strategy(strategy, authored_peers)

  first_compatibility_error = nil
  for peer in effective_peers:
    candidate = resolve_peer(peer)                  // Terminal row -> direct attempt
                                                     // Model row -> recursive child resolution
    if candidate is a compatibility miss:
      remember first miss in effective order
      continue
    if candidate has no eligible terminal attempts:      // includes Ban filtering
      continue
    if candidate's connection has a routing schedule and is outside it:
      continue                                             // evaluated after Ban filtering
    append candidate attempts as one contiguous block

  if resolved has terminal attempts and connections:
    return resolved
  if first_compatibility_error exists:
    return first_compatibility_error
  return no_eligible_target
```

The three strategies act on the same enabled mixed rows: `single` keeps only the first enabled row, `fill-first` walks the authored mixed order, and `round-robin` rotates the direct mixed rows once per request (each row occupies one cursor slot; a child model's expanded attempts do not enlarge the parent modulus, and the child claims its own cursor keyed by its own source model, strategy, and target-set hash). An enabled but currently unavailable or incompatible row still consumes its round-robin slot; eligibility is judged after rotation. Reordering, adding, removing, or toggling rows changes the target-set hash, and a new hash starts a fresh cursor while a reappearing identical hash may continue its existing cursor. Cursors are process-local.

Because eligibility is judged after rotation, a routing schedule shifts which row wins the first attempt rather than removing a cursor slot. A row that is outside its window still consumes its slot, so the first in-window row following a run of `k` consecutive out-of-window rows receives `(k+1)/N` of the first-attempt share for as long as that run lasts. This is a deliberate trade: the alternative filters before rotation, which would change the target-set hash whenever a window opens or closes and restart the cursor from zero each time. The skew matters most under the intended usage of switching upstreams by time of day, where most rows go out of window together — five rows with three of them scheduled for the night leave the remaining two splitting first attempts four to one throughout the day. Availability is unaffected: every in-window row is still attempted in order on failure.

### 5.4 Default profile and active runtime separation

Profile-scoped management APIs are frozen to Default id `1`. They accept `X-Profile-Id` for frontend compatibility, but the backend ignores the header value. Runtime proxy traffic ignores that management header and resolves through the frozen Default profile id `1` runtime snapshot.

Connection management keeps its existing ownership seams under `backend/internal/httpapi/management/connections/`: `connection_read_routes.go`, `routes.go`, and the owner mutation route files own HTTP boundaries; `connection_model_store.go`, `connection_endpoint_store.go`, `terminal_target_store.go`, `connection_access_target_store.go`, `connection_reference_store.go`, and `routing_window_store.go` own profile-scoped PostgreSQL records; `terminal_target_projection.go` and `connection_db_arguments.go` own row projection and database values. The HTTP-neutral `writer.go`, `access_targets.go`, `routing_schedule.go`, and `endpointdomain` remain canonical shared seams.

## 6. Request-Derived Metrics

Prism has no manual Terminal Target probe routes or probe-backed health fields. Retained request history exposes three explicit metric scopes with an authoritative ordered catalog owned by `backend/internal/domain/stats/scope.go`:

- `ingress` — `requests`, `errors`, `ttft`, `output_rate`, `tokens`, `cache_read_share`, `cost` (default `requests`)
- `final_execution` — `requests`, `errors`, `final_attempt_latency`, `tokens`, `cache_read_share`, `cost` (default `requests`)
- `route_attempt` — `attempts`, `errors`, `attempt_latency` (default `attempts`)

An omitted `metric` resolves to the scope default before validation; an explicit incompatible metric/group/filter returns typed `422`, never a no-op, alias, or cross-scope substitution. Ingress reads one `usage_request_events` row per finalized ingress and uses requested-model identity. Final execution uses the resolved leaf and actual winning Terminal Target, with latency joined by `final_attempt_number`. Route attempt reads upstream `request_logs`, classifies attempt outcomes, renders `attempts` as a count, and makes no cost claim. Ingress and final-execution cost are non-additive projections of the same `priced` plus `trusted` served-final fact. Latency P50/P95 for `ttft`, `final_attempt_latency`, and `attempt_latency` uses the shared percentile fields while preserving missing, zero, partial-sample, and read-failure states. Top-N series/Errors conserve the full denominator through a real Other bucket, labels resolve through current or retained catalogs with explicit fallbacks, and `truncated` means a remainder actually exists. Every envelope identifies its caliber, datasets, coverage, and observation/latency/cost sample and missing counts; the Models and Observe surfaces never infer health from absent samples.

### 6.1 URL Path Joining

Endpoint `base_url` values may include an upstream path prefix such as `/v1` or `/v1beta`. On create and update, Prism strips trailing slashes, requires a scheme and host, and rejects query strings or fragments. Runtime joins the normalized endpoint path with the allowlisted operation path for the selected request.

Prism does not document or apply version-segment de-duplication. Operators should configure endpoint paths to match the operation path shape expected by that upstream.
