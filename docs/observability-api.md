# Observability API

This reference covers statistics, Requests, Audit, and durable retention jobs. It follows the [shared HTTP contract](api-reference.md). See [observability and retention](observability-and-retention.md) for attribution, persistence, and capture semantics.

## 4. Statistics API

Stats APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

### 4.0 Dashboard Stats

```
GET /api/stats/dashboard
```

This mounted aggregate API returns one backend-computed, stats-only aggregate snapshot for the effective profile, including overview metrics, API-family rows, top-spending models, and a `routing_health_map` response field. It does not include recent request rows, request-log IDs, or request-log cursor data. Recent activity is served by `GET /api/stats/dashboard/recent-activity`. The current Observe UI uses the separate query-context fragments in §3.8 and does not consume this aggregate or render `routing_health_map`.

Query parameters: none. Legacy `window` query values are ignored. The endpoint always returns the canonical aggregate snapshot and does not expose the old top-level `window`, `covers`, `freshness`, or `metrics` shape. Snapshot freshness is ordered by lexicographic `snapshot_revision`; `source_watermark` is diagnostic only.

Response `200`:

```json
{
  "generated_at": "2026-04-19T12:00:00Z",
  "snapshot_revision": "01JZ8Y3K2N4P6R8T0V1W2X3Y4Z",
  "source_watermark": {
    "latest_usage_event_created_at": "2026-04-19T11:59:58Z",
    "latest_usage_event_id": 345
  },
  "coverage_24h": {
    "from": "2026-04-18T12:00:00Z",
    "to": "2026-04-19T12:00:00Z"
  },
  "coverage_30d": {
    "from": "2026-03-20T12:00:00Z",
    "to": "2026-04-19T12:00:00Z"
  },
  "health": {
    "lag_seconds": 0,
    "stale": false,
    "stale_after_seconds": 120
  },
  "metric_snapshot": {
    "active_models": 12,
    "average_rpm": 0.7,
    "average_rpm_request_total": 42,
    "avg_latency": 523,
    "error_rate": 2.38,
    "p95_latency": 900,
    "priced_request_count": 40,
    "stream_share": 18.5,
    "success_rate": 97.62,
    "total_cost": 1250000,
    "total_models": 14,
    "total_requests": 42,
    "unpriced_request_count": 2
  },

  "api_family_rows": [
    { "key": "openai", "total_requests": 42, "success_rate": 97.62 }
  ],
  "top_spending_models": [],
  "routing_health_map": {
    "nodes": [],
    "links": [],
    "endpointCount": 0,
    "modelCount": 0,
    "activeConnectionTotal": 0,
    "activeTerminalTargetTotal": 0,
    "trafficRequestTotal24h": 0
  }
}
```

`routing_health_map` is assembled by the backend from Default-profile model, access-target, endpoint, connection, and final-attributed usage-event data.

In `metric_snapshot`, `avg_latency`, `error_rate`, `p95_latency`, and `success_rate` are `null` when the window has zero traffic; the counts (`total_requests` etc.) stay numeric. Recent-activity items carry nullable `status_code` / `response_time_ms` (null for rows without a resolved duration), and `response_time_ms` is the end-to-end duration including stream finalization.

### 4.0A Dashboard Recent Activity

```
GET /api/stats/dashboard/recent-activity?limit=N
```

This mounted endpoint is a separate request-history-backed activity feed; the current Observe page uses its own activity fragment. It is not embedded in the dashboard snapshot. The default limit is `12`; the maximum limit is `50`. Rows are ordered by `(created_at DESC, request_log_id DESC)`.

Response `200`:

```json
{
  "generated_at": "2026-04-19T12:00:00Z",
  "activity_watermark": {
    "latest_request_log_created_at": "2026-04-19T11:59:59Z",
    "latest_request_log_id": 101
  },
  "items": [
    {
      "request_log_id": 101,
      "created_at": "2026-04-19T11:59:59Z",
      "ingress_model_id": "gpt-4o",
      "ingress_model_label": "GPT-4o",
      "attempt_target_model_id": "gpt-4o-mini",
      "attempt_target_model_label": "GPT-4o mini",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "status_code": 200,
      "response_time_ms": 403,
      "ttft_ms": 120,
      "completion_duration_ms": 403,
      "is_stream": true,
      "stream_outcome": "completed",
      "total_tokens": 1234,
      "total_cost_user_currency_micros": 1250000,
      "pricing_status": "priced",
      "unpriced_reason": null,
      "report_currency_symbol": "$"
    }
  ]
}
```

Recent activity links into request-log investigation by `request_log_id`. It does not define snapshot freshness, and activity publication does not force a dashboard snapshot rebuild.

### 4.1 Usage Snapshot

```
GET /api/stats/usage-snapshot
```

This is the retained REST analytics snapshot contract for API callers and debugging. The current `/observe?tab=trend` UI uses the signed query-context summary/series fragments described above rather than this snapshot polling path.

Query parameters:

| Parameter | Type | Default | Description |
|---|---|---|---|
| `preset` | string | `1h` | Snapshot range preset. Supported values: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |

The snapshot is backed by `backend/internal/httpapi/management/stats/service.go` together with the aggregation types and query helpers in `backend/internal/domain/stats/snapshot.go` and `backend/internal/domain/stats/types.go`.

The snapshot is aggregated from persisted usage-event rows. Endpoint aggregates read the stored `usage_request_events.endpoint_label_snapshot` value and expose it as public `endpoint_label`, so historical labels survive later endpoint renames or deletion. `/api/stats/dashboard` remains a separate aggregate with a backend-computed `routing_health_map`; the current Observe UI uses query-context fragments. Exact request investigation remains on `/observe/requests`, while dashboard and other pages continue to use the shared stats routes below.

Response `200` includes `latency_trends` alongside `request_trends`, `token_usage_trends`, `token_type_breakdown`, and `cost_overview`. `latency_trends.hourly[]` and `latency_trends.daily[]` use the same series key/label shape as request trends; each point exposes `bucket_start`, `p50_ms`, and `p95_ms`. Empty latency buckets keep the bucket and return `null` percentile values.

`overview` token fields use the canonical disjoint caliber described in [Request Statistics](observability-and-retention.md#7-request-statistics): `input_tokens` excludes cache-read and cache-creation input, `output_tokens` excludes reasoning output, and `cached_tokens` is the derived cache-read plus cache-creation aggregate. `token_component_basis` names that caliber (`"disjoint"`), and `uncategorized_tokens` reports `total_tokens` minus the sum of the components, clamped at zero. A positive `uncategorized_tokens` means upstreams supplied provider totals that the components cannot reconstruct - typically usage payloads carrying only a total - so the components and the total are expected not to add up in that case.

`GET /api/stats/requests/operations` is not part of the current management API.

### 4.1A Endpoint Model Statistics

```
GET /api/stats/endpoints/{endpoint_id}/models
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `preset` | string | `1h` | Time preset: `1h`, `6h`, `24h`, `7d`, `30d`, `all` |
| `from_time` | datetime | — | Optional explicit start time |
| `to_time` | datetime | — | Optional explicit end time |

Response `200` is an envelope `{items, scope, caliber, coverage, samples}` rather than a bare array. Each item supplies P50/P95 latency, priced/unpriced counts, trusted cost (including trusted zero), token totals and output-rate samples for the requested supported scope; missing samples remain null with explicit sample/missing counts.

### 4.2 List Request Logs / Ingress Chains

```
GET /api/stats/requests?view=attempts
GET /api/stats/requests?view=ingress_chains
```

The Requests page explicitly sends `view=ingress_chains` as its default. The API's omitted-view form remains the flat attempts read used by existing direct callers.

Attempt-view query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `ingress_request_id` | string | — | Exact incoming-request grouping ID shared by per-attempt rows |
| `ingress_model_id` | string | — | Filter by requested entry model ID |
| `attempt_target_model_id` | string | — | Filter by the resolved leaf model used by the attempt |
| `api_family` | repeated string | — | Attempt API-family filter; repeated and comma-separated values are equivalent |
| `row_kind` | repeated string | — | Retained row kind (`planning`, `admission`, `upstream`, `legacy_unknown`); route-attempt Observe links pin `upstream` |
| `status_family` | string | — | Filter by scoped status family (`2xx`, `4xx`, or `5xx`) |
| `status_code` | integer | — | Exact scoped status-code filter |
| `error_text` | string | — | Case-insensitive substring match against `error_detail`/`error_code`/`stream_error_detail`/`stream_error_kind` |
| `pricing_status` | string | — | Four-state pricing filter: `priced`, `unpriced`, `ineligible`, `unknown` (the old `priced` boolean alias is rejected with `422 unknown_query_key`) |
| `unpriced_reason` | string | — | Exact unpriced reason filter: `PRICING_DISABLED`, `MISSING_TOKEN_USAGE`, `STREAM_USAGE_UNAVAILABLE`, or `MISSING_PRICE_DATA` (only valid with `pricing_status=unpriced`) |
| `time_range` | string | `24h` | Server-resolved owner window: `1h`, `6h`, `24h`, `7d`, `30d`, `all`, or `custom`; `all` uses actual owner earliest coverage |
| `from_time` / `to_time` | datetime | — | ISO 8601 half-open time range `[from_time,to_time)` |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `terminal_target_id` | integer | — | Filter by actual Terminal Target ID |
| `client_rule_id` | integer | — | Filter by caller client, matched against `caller_user_agent` only through enabled User-Agent Client Rules |
| `limit` | integer | 50 | Result limit; must be positive |
| `offset` | integer | 0 | Pagination offset (attempt view) |
| `sort_by` | string | `created_at` | One of `created_at`, `display_status`, `ttft_ms`, `total_tokens`, or `total_cost_user_currency_micros` (attempt view); other values return `422 sort_unsupported` |
| `sort_order` | string | `desc` | `asc` or `desc`; any other value is rejected with `422 sort_unsupported` |

Chain-view query parameters (`view=ingress_chains`):

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `chain_limit` | integer | 20 | Ingress count per outer page (max 50) |
| `chain_row_limit` | integer | 50 | Retained-row inner page per ingress (max 200) |
| `chain_cursor` | string | — | Signed opaque outer cursor binding the frozen window and complete cohort; never splits an ingress across pages |
| `row_cursor` | string | — | Signed opaque inner cursor binding one exact ingress, frozen window, filters and row page size |
| `anchor_request_log_id` | string | — | Exact BIGINT row anchor within one exact ingress selector; returned as a separate `anchor_item` in the same batch |
| `time_range` | string | `24h` | Same server-resolved owner window as attempts; `custom` requires `from_time` and `to_time` |
| `sort_by` | string | `created_at` | `created_at`, `elapsed_ms`, or `total_cost_user_currency_micros` |
| `sort_order` | string | `desc` | Metric direction within a comparable group; missing values remain last in either direction |
| Cohort filters | — | — | Tokenless `confirmed_failover`/`ingress_final_result`; finalized pricing/cost-segment/model selectors; retained-row target/status/client filters |

Ranked chains use the same retained cohort as totals and chain CSV. `elapsed_ms` is the floored nonnegative millisecond difference between the retained finalized event's ingress start and completion, never the sum of attempt durations. Cost is the latest retained finalized event's known `priced`/`trusted` report-currency cost, once per ingress. Comparable cost groups are the canonical `cost_segment_key` plus currency code; group ordering is an identity order, not a comparison of amounts across currencies or epochs. Each chain carries `ranking: {metric,value,group,state}`, where missing-finalized, missing-elapsed, untrusted-cost and unknown-currency states remain explicit.

The envelope's `ranking` contains `metric`, `rankable_ingress_count`, `unrankable_ingress_count` and `unrankable_reasons` (`missing_finalized`, `missing_elapsed`, `untrusted_cost`, `unknown_currency`) for the **entire filtered cohort**, including on an empty outer page. It is not a count of rows currently rendered. Cursor identity includes the metric, nullable value, comparable group and deterministic time/ingress tie-breakers alongside the existing window/filter/retention identity. Chain CSV selects that same cohort and follows its group/metric order before retained-row order, without adding synthetic parent costs or making per-attempt charges additive.

The `/observe/requests` route uses these signed chain cursors and derives its default 24-hour window in the page state. The request attempts, ingress-chain list, and CSV export send the selected `time_range` to the server; the server resolves it against the Requests actual-coverage owner rather than trusting a browser-generated timestamp. CSV export is server-side over the selected bounded result set at `GET /api/stats/requests/export`; responses include the digest, exact content length and coverage callout rather than treating an incomplete retained window as true-empty.

Attempt-view response `200`:

```json
{
  "filter_options": {
    "endpoints": [{ "endpoint_id": 12, "endpoint_label": "Primary OpenAI" }],
    "ingress_models": [{ "ingress_model_id": "gpt-4o", "model_label": "GPT-4o" }],
    "clients": [{ "client_rule_id": 7, "client_label": "Codex" }],
    "attempt_target_models": [{ "attempt_target_model_id": "gpt-4o", "model_label": "GPT-4o" }]
  },
  "coverage": {
    "requested_from_time": "...",
    "requested_to_time": "...",
    "effective_from_time": "...",
    "effective_to_time": "...",
    "retention_from_time": "...",
    "complete": true,
    "gaps": [],
    "state": "known|legacy_unknown",
    "source_revision": "request_logs-v1:..."
  },
  "items": [
    {
      "request_log_id": "1327",
      "row_kind": "upstream",
      "ingress_request_id": "018f...",
      "attempt_number": 1,
      "attempt_trigger": "initial",
      "attempt_result": "http_error",
      "is_winner": true,
      "attempt_duration_ms": 842,
      "legacy_duration_ms": null,
      "upstream_status_code": 429,
      "gateway_status_code": null,
      "legacy_status_code": null,
      "error_source": "upstream",
      "error_code": "rate_limit_exceeded",
      "failure_stage": "upstream_response",
      "failure_detail_preview": "{\"error\":{\"message\":\"Rate limit...",
      "failure_detail_source": "error_detail",
      "failure_detail_preview_truncated": true,
      "failure_detail_redacted": false,
      "stream_outcome": "not_streaming",
      "stream_error_kind": null,
      "ingress_model_id": "gpt-4o",
      "model_label": "GPT-4o",
      "attempt_target_model_id": "gpt-4o",
      "attempt_target_model_label": "GPT-4o",
      "upstream_model_id": "provider/model-a",
      "api_family": "openai",
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "terminal_target_id": 1,
      "terminal_target_label": "Primary OpenAI Connection",
      "terminal_target_configured": true,
      "ttft_ms": 320,
      "completion_duration_ms": null,
      "is_stream": false,
      "output_tokens": null,
      "total_tokens": null,
      "total_cost_user_currency_micros": null,
      "pricing_status": "ineligible",
      "pricing_evidence_trust": "trusted",
      "unpriced_reason": null,
      "reasoning_effort": "low",
      "report_currency_symbol": "$",
      "caller_client_display": "Codex",
      "upstream_client_display": "OpenAI SDK",
      "user_agent_overridden": false,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 150,
  "limit": 50,
  "offset": 0
}
```

`terminal_target_label` is resolved from the current connection catalog: `null` means the connection row no longer exists; a label with `terminal_target_configured=false` means the connection exists but is inactive; a catalog read failure always returns `5xx` and never downgrades to `configured=false`. `total` is capped at 10000 with `total_is_exact=false` above the cap (`has_more` is then also true), so the UI can render "10,000+".

Chain-view response `200`:

```json
{
  "query_context": null,
  "source_ingress_total": 3,
  "retained_ingress_total": 3,
  "retained_upstream_attempt_total": 4,
  "retained_request_log_row_total": 4,
  "legacy_unknown_row_total": 0,
  "page_ingress_count": 2,
  "items": [
    {
      "ingress_request_id": "018f...",
      "started_at": "2026-08-08T19:13:00Z",
      "completed_at": "2026-08-08T19:13:12Z",
      "elapsed_ms": 12400,
      "elapsed_evidence_state": "authoritative",
      "finalized_evidence_state": "authoritative",
      "finalized_summary": {
        "request_log_id": "1329",
        "final_status_code": 200,
        "final_result": "completed",
        "final_error_code": null,
        "requested_model": {"id": "agent", "label": "Agent"},
        "resolved_model": {"id": "gpt-5.6", "label": "GPT-5.6"},
        "final_upstream_model_id": "provider/gpt-5.6-2026-08",
        "terminal_target": {"id": 23, "label": "OpenCode Go / GPT-5.6", "configured": true, "owner_model_id": "gpt-5.6"},
        "endpoint": {"id": 7, "label": "OpenCode Go"},
        "ttft_ms": 320,
        "output_rate_tps": 48.2,
        "total_tokens": 1840,
        "total_cost_user_currency_micros": "23100",
        "report_currency_code": "USD",
        "report_currency_symbol": "$",
        "reporting_currency_epoch": 3,
        "currency_attribution": "identified",
        "cost_segment_key": "e.3",
        "final_pricing_status": "priced",
        "final_unpriced_reason": null,
        "final_pricing_resolution_kind": null,
        "missing_price_components": null,
        "final_pricing_evidence_trust": "trusted",
        "pricing_template_id_used": 41,
        "pricing_template_name_snapshot": "GPT-5.6 Standard",
        "pricing_template_revision_id_used": "901",
        "pricing_config_version_used": 4,
        "pricing_version_effective_at": "2026-08-01T12:00:00Z",
        "pricing_snapshot_unit": "PER_1M",
        "pricing_snapshot_input": "2.5",
        "pricing_snapshot_output": "10",
        "pricing_snapshot_cache_read_input": null,
        "pricing_snapshot_cache_creation_input": "0",
        "pricing_snapshot_reasoning": "3",
        "attempt_count": 3,
        "final_attempt_number": 3,
        "final_attempt_trigger": "initial",
        "final_target_entry_trigger": "initial"
      },
      "expected_attempt_count": 3,
      "expected_request_log_row_count": 3,
      "retained_upstream_attempt_count": 3,
      "retained_request_log_row_count": 3,
      "legacy_unknown_row_count": 0,
      "chain_complete": true,
      "same_target_retry_occurred": false,
      "hedge_occurred": false,
      "failover_occurred": true,
      "routing_evidence_complete": true,
      "retained_rows_loaded_count": 3,
      "retained_rows_page_complete": true,
      "next_row_cursor": null,
      "retained_rows": [
        {
          "request_log_id": "1327",
          "row_kind": "upstream",
          "attempt_number": 1,
          "attempt_trigger": "initial",
          "attempt_result": "http_error",
          "is_winner": false,
          "upstream_model_id": "provider/gpt-5.6-a",
          "upstream_status_code": 429,
          "gateway_status_code": null,
          "legacy_status_code": null,
          "attempt_duration_ms": 842,
          "pricing_status": "ineligible",
          "pricing_evidence_trust": "trusted",
          "created_at": "2026-08-08T19:13:00Z",
          "is_current": false
        }
      ]
    }
  ],
  "has_more_chains": true,
  "next_chain_cursor": "signed-chain-cursor-2"
}
```

Chain semantics:

- Outer page never splits an ingress identity; `retained_rows` are ordered `created_at ASC, id ASC` and the immutable `attempt_number` carries upstream launch order.
- `started_at`/`completed_at`/`elapsed_ms` come only from finalized usage evidence (`ingress_started_at`/`ingress_completed_at`); without finalized evidence all three are null with `elapsed_evidence_state=unavailable`.
- `retained_upstream_attempt_count` counts `row_kind=upstream` only; `retained_request_log_row_count` counts all retained row kinds; legacy rows are counted separately in `legacy_unknown_row_count`.
- `chain_complete` expresses retention/evidence reconciliation (expected vs retained), not the current API row page.
- Whole-chain counts, retained-row pages, and full-cohort totals read retained rows inside the requested window widened by one day on each side. A chain whose retained rows straddle that boundary contributes only the rows inside it. The bound exists because `request_logs` and `usage_request_events` are range-partitioned on `created_at` and only prune when the window constrains the scanned relation directly; one day is far beyond the span of a single client request and its retries.
- The outer page walks `usage_request_events` in `(profile_id, created_at)` order and merges the chains that have retained rows but no usage event, resolved over the same page span. Both sources carry the same `(created_at, ingress_request_id)` key, so an ingress is never split across pages and never appears twice.
- Items are keyed by the finalized event; the full-cohort totals and the CSV export are keyed by retained rows. The two agree on every chain that finalized inside the window. A chain still in flight at the window's upper edge — rows retained before `to_time`, finalization landing after it — is counted by the totals and exported, and appears as an item on the window that contains its finalization. This is the same incompleteness the coverage projection already reports for the live edge, not a cohort-selector difference.
- Outer chain cursors carry the ordering version. A well-formed, correctly signed cursor minted under a retired ordering returns `410 chain_cursor_version_retired`; the client reloads the first page. Row cursors are unaffected: retained rows keep their `(created_at, id)` order.
- `finalized_summary` fields come only from the finalized `usage_request_events` row; attempt rows never carry `final_*` facts.
- Each attempt/list/chain/exact-detail row exposes its stored `upstream_model_id`. `finalized_summary.final_upstream_model_id` comes only from the winner usage event. A missing historical or diagnostic value stays `null`; the server and UI never substitute a current Terminal Target value or a logical model ID.
- `finalized_summary.currency_attribution` comes from persisted usage-event provenance; `cost_segment_key` remains independently canonicalized as epoch first, then legacy code, then unknown.
- `attempt_budget_exhausted` etc. gateway terminal codes appear in `final_error_code`.
- All Requests list/detail/chain/export responses send `Cache-Control: private, no-store` and preserve auth/profile-sensitive `Vary`.

The attempts route is the slim browse contract and keeps one retained row per attempt, while the chain envelope keeps one selected ingress with its complete retained-row counts/pages. Both envelopes require `filter_options.endpoints`, `filter_options.ingress_models`, `filter_options.clients`, and `filter_options.attempt_target_models`; empty option sets are `[]`. Rows include requested and attempt-target labels, `stream_outcome`, `stream_error_kind`, and a failure preview. `client_rule_id` matches caller User-Agent only and never matches `upstream_user_agent`. Requested, attempt-target, and signed final-owner identities remain distinct, and BIGINT row IDs are positive decimal strings rather than JavaScript numbers. The attempts UI offers page sizes 100, 300, and 500, with 100 as its default; this retained-history route is an investigation surface, not a dashboard aggregate.

Ordinary Requests triage (`ingress_final_result`, `confirmed_failover`) needs no query token. Observe analysis links use `view=attempts`, an opaque signed `query_context`, and explicit repeated/comma-equivalent selectors. Same-key values are OR, different keys are AND, and `__null__` matches a normalized unattributed value without forcing finalized usage evidence for attempt-only selectors. A server-generated bounded `final_exclude=<facet>,<visible>...` expresses the exact complement behind ingress/final Errors Other; its facet is closed, values are parameterized, and it is rejected without the signed context. Exact `ingress_request_id` without an explicit window resolves against owner-retained `all` bounds for attempts, chains and CSV. Every cohort mutation clears cursors; signed outer and row cursors also bind the normalized cohort/window, profile, ordering, page size and retention generation.

Exact single-request investigation now lives on `GET /api/stats/requests/{request_id}` (v2 detail) instead of the paginated list-query surface.

### 4.2A Full Filtered CSV Export

```
GET /api/stats/requests/export
```

Server-side full filtered CSV export ([Requests requirements](requests.md#6-data-and-api-requirements)). It reuses the non-pagination attempt/chain filter projection, including requested/attempt/final identities, API family, row kind, status/error, pricing, signed `query_context`, and final selectors. `view=attempts` exports the flat attempt cohort; `view=ingress_chains` exports all retained rows of the server-selected chain cohort, including finalized pricing/cost-segment and row-filter semantics. Signed final selectors retain their frozen request-domain bounds, while ordinary triage remains tokenless. The export:

The CSV places `upstream_model_id` immediately after
`attempt_target_model_id`; the cell is the immutable attempt snapshot or empty
for rows without evidence. It is subject to the same formula-injection
protection as every other exported text cell.

- Reads rows and counts in one `READ ONLY REPEATABLE READ` snapshot (with the exact-ingress range exemption); concurrent inserts cannot change the exported row count.
- Rejects more than 100,000 rows or a requested range wider than 31 days (unless exact ingress is supplied) with a typed error before any file bytes; exactly 31 days is reachable even though ordinary custom browsing remains capped at 30.
- Neutralizes formula injection by prefixing cells that start with `=`, `+`, `-`, or `@` with a single quote.
- Spools to a 0600 temp file (128 MiB cap), computes a SHA-256 digest, then streams with `Content-Type: text/csv`, `Cache-Control: private, no-store`, and `Digest: sha-256=...`. Any spool/digest failure returns a typed rejection, never a partial success file.
- Rejects every attempts/chain/row pagination key before opening the export snapshot.

### 4.3 Get Request Log Detail

```
GET /api/stats/requests/{request_id}
```

Response `200` (v2 exact detail; old un-scoped `status_code`/`response_time_ms` and `priced_flag`/`billable_flag` are not part of this contract):

```json
{
  "summary": {
    "request_log_id": "1327",
    "created_at": "2025-01-15T10:30:00Z",
    "model_id": "gpt-4o",
    "model_label": "GPT-4o",
    "resolved_target_model_id": "gpt-4o",
    "resolved_target_model_label": "GPT-4o",
    "upstream_model_id": "provider/model-a",
    "api_family": "openai",
    "row_kind": "upstream",
    "upstream_status_code": 429,
    "gateway_status_code": null,
    "legacy_status_code": null,
    "attempt_duration_ms": 842,
    "legacy_duration_ms": null,
    "ttft_ms": null,
    "completion_duration_ms": null,
    "is_stream": false,
    "stream_outcome": "not_streaming",
    "stream_error_kind": null,
    "attempt_number": 1,
    "attempt_trigger": "initial",
    "attempt_result": "http_error",
    "is_winner": true
  },
  "request": {
    "operation_name": "openai.chat_completions",
    "upstream_operation_name": "openai.chat_completions",
    "operation_translation_mode": "none",
    "request_path": "/v1/chat/completions",
    "upstream_request_path": "/v1/chat/completions",
    "ingress_request_id": "ingress_req_42",
    "provider_correlation_id": "req_upstream_abc123",
    "proxy_api_key_id": null,
    "proxy_api_key_name_snapshot": null,
    "caller_user_agent": "codex/1.0",
    "upstream_user_agent": "OpenAI/Python 1.0",
    "caller_client_display": "Codex",
    "upstream_client_display": "OpenAI SDK",
    "user_agent_overridden": false,
    "request_generation_params": {"temperature": 0.7},
    "request_generation_params_status": "captured",
    "metadata_redacted_fields": ["authorization"],
    "metadata_truncated_fields": [],
    "url_scrub_provenance": "runtime_scrubbed"
  },
  "routing": {
    "profile_id": 1,
    "endpoint_label": "Primary OpenAI",
    "endpoint_id": 12,
    "terminal_target_id": 1,
    "selected_terminal_target_id": 1,
    "endpoint_base_url": "https://api.openai.com",
    "endpoint_description": "Primary OpenAI",
    "audit_enabled_at_request": false,
    "audit_capture_bodies_at_request": false
  },
  "usage": {
    "input_tokens": 15,
    "output_tokens": null,
    "total_tokens": null,
    "success_flag": false,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0,
    "reasoning_tokens": 0
  },
  "failure": {
    "category": "upstream_http",
    "source": "upstream",
    "stage": "upstream_response",
    "code": "rate_limit_exceeded",
    "detail": "{\"error\":{\"message\":\"Rate limit reached...\"}}",
    "detail_redacted": false,
    "detail_truncated": false,
    "detail_source": "error_detail",
    "evidence_state": "authoritative",
    "upstream_request_started": true,
    "response_headers_received": true,
    "first_body_or_stream_event_seen": true,
    "stream_outcome": "not_streaming",
    "stream_error_kind": null,
    "stream_error_detail": null
  },
  "terminal_target": {
    "kind": "terminal_target",
    "terminal_target_id": "23",
    "owner_model_config_id": "42",
    "name": "OpenCode Go / GPT-5.6",
    "name_source": "current",
    "deleted": false,
    "configured": true
  },
  "endpoint": {
    "kind": "endpoint",
    "id": "7",
    "name": "OpenCode Go",
    "name_source": "current",
    "deleted": false,
    "configured": true
  },
  "routing_provenance": {
    "initial_terminal_target": {"kind": "terminal_target", "terminal_target_id": "19", "owner_model_config_id": "42", "name": "Initial Target #19", "name_source": "snapshot", "deleted": null, "configured": null},
    "differs_from_actual": true
  },
  "pricing": {
    "pricing_status": "ineligible",
    "unpriced_reason": null,
    "pricing_resolution_kind": null,
    "missing_price_components": null,
    "pricing_evidence_trust": "trusted",
    "total_cost_user_currency_micros": null,
    "total_cost_original_micros": null,
    "currency_code_original": "USD",
    "fx_rate_used": "1",
    "fx_rate_source": "DEFAULT_1_TO_1",
    "report_currency_code": "USD",
    "report_currency_symbol": "$",
    "reporting_currency_epoch": 3,
    "currency_attribution": "identified",
    "cost_segment_key": "e.3",
    "pricing_template_id_used": 41,
    "pricing_template_name_snapshot": "GPT-5.6 Standard",
    "pricing_template_revision_id_used": "901",
    "pricing_config_version_used": 4,
    "pricing_version_effective_at": "2026-08-01T12:00:00Z",
    "pricing_snapshot_unit": "PER_1M",
    "pricing_snapshot_input": "33.333333",
    "pricing_snapshot_output": "17.857143",
    "pricing_snapshot_cache_read_input": "0",
    "pricing_snapshot_cache_creation_input": "0",
    "pricing_snapshot_reasoning": "0",
    "evidence_state": "authoritative"
  },
  "legacy_pricing_evidence": null,
  "current_pricing_template": {
    "template_id": 41,
    "deleted": false,
    "current_revision_id": "944",
    "current_version": 6,
    "current_effective_at": "2026-08-08T09:00:00Z",
    "matches_request_revision": false
  }
}
```

Failure projection semantics:

- `category` is one of `planning`, `admission`, `upstream_http`, `transport`, `provider_stream`, `client_disconnect`, or `unknown`, derived from typed row facts. Normal success returns `failure=null`.
- The projection picks detail from `error_detail` by default and from `stream_error_detail` only when error detail is absent (with `detail_source="stream_error_detail"` and the stream redacted/truncated flags); it never writes stream detail back into `error_detail`.
- Legacy failed rows without detail keep known source/stage values and return `detail=null, evidence_state="unavailable"`.
- `legacy_pricing_evidence` is non-null only when `pricing_evidence_trust=legacy_untrusted` (shape `{raw_total_cost_original_micros, raw_total_cost_report_micros, raw_component_cost_micros, raw_price_snapshots, original_currency_code, report_currency_code, warning_code:"historical_unverified"}`); canonical cost stays null for those rows.
- `current_pricing_template` is an optional read-only comparison; deleted/out-of-scope templates return `deleted=true` with the availability state, never a current-configuration substitute.

Request-log detail uses the same canonical disjoint token components as runtime persistence: base input, base output, cache-read input, cache-creation input, reasoning output, and provider or derived total. Pricing snapshots store the five strings from the selected card only. Detail also exposes independent template kind, selection state, card role, generic tier evidence, and peak/valley decision-time evidence. Explicit `"0"` prices are configured free pricing and produce zero component cost without marking the row unpriced. Public request-log detail exposes `terminal_target_id` as the actual attempt exit and `selected_terminal_target_id` as the planning-primary identity; it does not expose a second `routing.connection_id` alias.

The retained request-log list and ingress-chain views accept independent `pricing_card_role` and `pricing_selection_state` row filters. `cost_segment_key` is validated as `e.<positive epoch>`, `l.<AAA>`, or `l.__unknown__`, reuses the canonical classifier for filtering, and does not by itself replace the retained-row ingress source.

Request-log detail keeps ingress and upstream attribution separate. `request.operation_name` and `request.request_path` are ingress-led. `request.upstream_operation_name`, `request.operation_translation_mode`, and `request.upstream_request_path` describe the provider-facing operation selected for the attempt. Current OpenAI attempts use `operation_translation_mode = "none"` and keep ingress and upstream operation shapes equal. Historical rows retain and expose any translation mode recorded before sibling conversion was removed.

Ordinary target-graph attempts preserve the top-level requested and resolved model fields without rewriting client-visible response-body identity. Request-log detail exposes resolved model and selected Terminal Target attribution.

Response `404`: returned when the request ID is missing or out of scope for the effective profile.

Stream telemetry values are stable strings. `stream_outcome` is one of `not_streaming`, `completed`, `provider_incomplete`, `client_disconnected`, `upstream_read_error`, `upstream_ended_without_terminal`, or `unknown`. `stream_error_kind` is nullable and, when present, is one of `client_write_failed`, `request_context_canceled`, `upstream_read_failed`, or `missing_terminal_event`. `stream_error_detail` appears only on exact request-log detail responses (via the failure projection); it is sanitized diagnostic text, not provider content, headers, or secrets.

The request-log sheet consumes this grouped detail contract as overview-only data. Linked audit payload resolution is isolated to the dedicated `/observe/requests/{request_id}/audit` page, which uses `request_log_id` plus a UTC window derived from `summary.created_at`. The derived frontend window is `created_at` minus 12 hours through `created_at` plus 12 hours, serialized explicitly as canonical audit `from` and `to` query parameters.

The request-history, spending, throughput, usage-snapshot, model-metrics, connection-success-rate, and dashboard aggregate APIs in this section remain product-facing PostgreSQL-backed surfaces. Prism no longer starts metrics or tracing exporters and does not surface a backend-local `/metrics` compatibility route.

### 4.4 Get Aggregated Statistics

```
GET /api/stats/summary
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `scope` | string | `ingress` | `ingress`, `final_execution`, or `route_attempt` |
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |
| `group_by` | string | `none` | Scope-specific identity: `ingress_model`, `final_target_model`, `attempt_target_model`, `api_family`, `proxy_api_key`, `endpoint`, `terminal_target`, `attempt_trigger`, or `attempt_result` as allowed by the selected scope |
| `ingress_model_id` | string | — | Filter ingress scope by requested model |
| `final_target_model_id` | string | — | Filter final-execution scope by resolved leaf model |
| `attempt_target_model_id` | string | — | Filter route-attempt scope by the attempt's resolved leaf model |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `terminal_target_id` | integer | — | Filter by actual Terminal Target ID |

Unknown keys, ambiguous legacy model keys, and scope-incompatible group or filter values return typed `422` errors.

Response `200`:

```json
{
  "caliber": {
    "scope": "ingress",
    "grain": "ingress_request",
    "identity_basis": "ingress_model_id",
    "outcome_basis": "final_result",
    "latency_basis": "ingress_end_to_end",
    "cost_basis": "served_final_trusted_cost",
    "datasets": ["usage_request_events"]
  },
  "total_requests": 1500,
  "success_count": 1450,
  "error_count": 50,
  "success_rate": 96.67,
  "avg_response_time_ms": 850,
  "p95_response_time_ms": 2100,
  "total_input_tokens": 50000,
  "total_output_tokens": 120000,
  "total_tokens": 170000,
  "groups": [
    {
      "key": "gpt-4o",
      "total_requests": 800,
      "success_count": 790,
      "error_count": 10,
      "avg_response_time_ms": 750,
      "total_tokens": 90000
    }
  ],
  "coverage": { "usage_request_events": { "complete": true } },
  "samples": {
    "observation_count": 1500,
    "latency_sample_count": 1500,
    "latency_missing_count": 0,
    "cost_sample_count": 1400,
    "cost_missing_count": 50
  }
}
```

Ingress and final-execution summaries read finalized usage events; final-execution latency also uses retained request logs. Route-attempt summaries read only `row_kind=upstream` request-log rows and use `attempt_result` plus raw `attempt_duration_ms`.

### 4.5 Model Metrics (Batch)

```
POST /api/stats/models/metrics
```

Request:

```json
{
  "model_ids": ["gpt-4o", "claude-3-5-sonnet"],
  "summary_window_hours": 24,
  "spending_preset": "last_30_days"
}
```

Response `200` returns each requested model once with `ingress`, `final_execution`, and `route_attempt` metric blocks. Each block contains `request_count`, nullable `success_rate`, nullable `p95_latency_ms`, nullable `known_cost_micros`, `caliber`, and sample/missing counts. Route-attempt cost is always null with `cost_basis=none`. Response-level quality coverage carries usage and request datasets; spending coverage carries usage evidence. Ingress and final-execution cost are non-additive groupings of the same served-final trusted fact.

### 4.6 Get Connection Success Rates

```
GET /api/stats/connection-success-rates
```

Returns route-attempt completion data for actual Terminal Targets, computed only from upstream `request_logs` rows. Each item carries route-attempt caliber, request-log coverage, and sample counts; it does not expose served-final cost.

Query parameters:

| Parameter | Type | Default | Description |
|---|---|---|---|
| `from_time` | datetime | — | Start of time range. If omitted, returns all historical data. |
| `to_time` | datetime | now | End of time range |

Response `200`:

```json
[
  {
    "connection_id": 1,
    "total_requests": 150,
    "success_count": 148,
    "error_count": 2,
    "success_rate": 98.67
  },
  {
    "connection_id": 2,
    "total_requests": 0,
    "success_count": 0,
    "error_count": 0,
    "success_rate": null
  }
]
```

Fields:

- `connection_id` (int): The connection ID
- `total_requests` (int): Total requests routed through this connection
- `success_count` (int): Requests with 2xx status codes
- `error_count` (int): Requests with non-2xx status codes
- `success_rate` (float | null): Success percentage (0-100), `null` if no requests

### 4.7 Get Throughput Report

```
GET /api/stats/throughput
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `scope` | string | `ingress` | `ingress`, `final_execution`, or `route_attempt` |
| `preset` | string | `24h` | Actual-coverage-resolved time preset |
| `from_time` | datetime | — | Start of time range |
| `to_time` | datetime | now | End of time range |
| `ingress_model_id` | string | — | Requested-model filter for ingress scope |
| `final_target_model_id` | string | — | Resolved-model filter for final-execution scope |
| `attempt_target_model_id` | string | — | Resolved-model filter for route-attempt scope |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `terminal_target_id` | integer | — | Filter by actual Terminal Target ID |

Response `200`: Throughput summary plus time buckets (`average_rpm`, `peak_rpm`, `current_rpm`, `total_requests`, `time_window_seconds`, `buckets[]`) and the selected caliber, dataset coverage, and sample counts.

### 4.8 Global Log Retention Settings

```
GET /api/settings/log-retention
PUT /api/settings/log-retention
POST /api/maintenance/log-retention/preflights
POST /api/maintenance/log-retention/jobs
POST /api/maintenance/log-retention/jobs/{id}/cancel
POST /api/settings/log-retention/owner-drift-archive
```

These routes are global and do not use `X-Profile-Id`. They store the instance-wide retention policy and expose the destructive preflight → sealed manual job flow. Request-log, audit-log, statistics, and load-balance list/detail APIs are pinned to Default profile id `1`, but retention settings do not.

The policy uses a monotonic `revision` for CAS. `GET` returns the tagged current state; `PUT` is a full four-field replacement requiring `operation_id` and `expected_revision`. Destructive changes (enabling `null -> N` or shortening `M -> N, N < M`) require a fresh server preflight token plus keyword `DELETE` confirmation; extensions and `N -> null` are plain CAS. Legacy values above `36500` enter a `repair_required` tagged union and are never silently clamped or defaulted.

`GET` response (v2):

```json
{
  "state": "ready",
  "scope": "instance",
  "revision": "4",
  "updated_at": "2026-08-09T12:00:00Z",
  "server_now": "2026-08-09T12:00:00Z",
  "policies": {
    "request_logs_retention_days": 30,
    "audit_logs_retention_days": 7,
    "statistics_retention_days": 90,
    "loadbalance_events_retention_days": 30
  },
  "recommendations": [],
  "policy_generation": {},
  "configured_logical_cutoffs": {},
  "published_retention_floors": {},
  "retention_source_revision": {},
  "actual_coverage": {},
  "protection": {},
  "owner_drift_inventory": null
}
```

`actual_coverage` is the per-dataset owner projection consumed verbatim from `log_retention_policy_resources` via the shared `LoadRetentionSourceProjection` (Observe-owned source); it is never recomputed from policy days, `MIN/MAX(created_at)`, or a second Settings coverage route. Each projection binds the owner materialization cut: `source_revision`, `coverage_revision`, `coverage_hash`, `materialization_cut`, `retention_generation`, separate `fence_generation`, freshness, purge state, and explicit gaps. Policy/floor transitions refresh the affected owner projection in the same transaction; append-only runtime writers advance it in the same telemetry transaction; repair/fence transitions mark it dirty and require owner refresh. `protection` carries the Observe token TTL/grace projection for request/usage/event datasets and the embedded audit fence projection for `audit_logs`. Fresh installs seed all four retention fields `NULL`; existing values migrate unchanged.

Preflight (`POST /api/maintenance/log-retention/preflights`) is a read-only, single-use, body-only token flow with a discriminated union (`policy_change` full draft or `manual_cleanup` with `keep_days|cutoff|delete_all` selection). The response binds the exact affected-domain owner snapshots, bounded count/bytes/partition impact, resolved UTC cutoff, and a 5-minute token. Manual job creation (`POST /api/maintenance/log-retention/jobs`) accepts only `operation_id` + `preflight_token` + `confirmation.keyword`, durably seals the intent as a queued v2 job (`202` / `replayed=true` on exact replay), and never revokes coverage at acceptance. Execution freezes `purge_to_time` at the fence for delete-all, publishes `running|recovery_required` fail-closed states, and final publish advances the domain revocation epoch and floor (old Observe tokens then return `410 dataset_snapshot_revoked`). Manual queued jobs are cancellable; running manual purges return `409 purge_not_cancellable`; running automatic jobs accept `cancel_requested`.

`GET /api/management/jobs` (with `scope=global&type=log_retention`) lists/paginates durable v2 jobs with per-dataset `resource_key`, origin, state, cutoff/purge_to_time, policy revision/generation, progress checkpoints, and partition evidence. The job type is `log_retention`, uses `profile_id = 0`, and applies across the instance.

Retention drops whole daily child partitions whose upper bound is `<= cutoff/purge_to_time`, never current/future partitions; only the single child partition that overlaps the cutoff receives a bounded row delete, followed by `VACUUM (ANALYZE, PROCESS_TOAST TRUE)` on that boundary child. Audit rows keep weak request metadata; request-log retention never clears `request_log_id`, `request_log_created_at`, or `ingress_request_id`.

### 4.8A Global Log Retention Data Model

- `log_retention_settings`: singleton `global` row with the four policy fields, monotonic `revision`, and `1..36500` `NOT VALID` CHECKs (finalizer-validated).
- `log_retention_policy_resources`: one row per dataset carrying `policy_generation`, separate semantic `fence_generation`, `settings_revision`, `configured_logical_cutoff` (UTC day-aligned, never `now - N*24h`), `published_retention_floor`, `retention_revocation_epoch`, `purge_state` (`idle|running|recovery_required|published|rolled_back`), `physical_reclaim_state`, and `desired_work_identity`. This row is the single retention source consumed by Observe query contexts, ordinary Requests reads, Audit coverage, manual purge final publish, and the Settings projection; consumers never re-derive a floor from policy days or `MIN(created_at)`.
- `log_retention_preflights`: single-use sealed destructive previews (token hash at rest, operation/attempt identity, exact affected-domain owner snapshots, expiry/consumed state).
- `management_jobs` v2 extension: `contract_version`, `operation_id`/`request_hash`, `origin`, `resource_key`, `purge_to_time`, `purge_state`, `stage`, `terminal_disposition`, legacy provenance fields, partition/boundary accounting, and the v2 state machine CHECKs plus one-manual-reservation partial unique index.
- `settings_mutation_operations`: durable operation/request identity for response-loss replay.
- `settings_migration_evidence` + `settings_owner_drift_inventory`: per-field current-head lineage for the three duplicated legacy `user_settings` retention columns; ordinary policy commits terminalize changed heads and append successors; the explicit finalizer gates on current `converged|archived` heads only.
- `settings_schema_transition`: singleton finalizer phase/lease state; `prism_migration_history` records the UXM-008 marker.
- `proxy_key_readiness_state` and immutable `auth_config_versions`: Proxy-owned counted key classification and Auth staged/effective pointer handoff. The readiness row is evaluated at one server-clock instant and includes the 30-second safe-active horizon.
- `pricing_migration_inventories`, chunked currency drafts, and migration ledgers: bounded Pricing-owner evidence for atomic reporting-currency epoch cutover and the unused-FX archive-only path. No Settings route authors FX mappings or fabricates pending prices.

Scheduled request/usage/loadbalance protection uses the Observe 24h token TTL + 24h grace; audit retention uses its own fence projection and never inherits a fixed 48h claim.

### 4.9 Get Spending Reports

```
GET /api/stats/spending
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `scope` | string | `ingress` | `ingress` or `final_execution`; `route_attempt` returns no cost claim |
| `preset` | string | — | Time preset: `today`, `24h`, `last_7_days`, `7d`, `last_30_days`, `30d`, `custom`, `all` |
| `from_time` | datetime | — | Start of time range (ISO 8601) |
| `to_time` | datetime | — | End of time range (ISO 8601) |
| `api_family` | string | — | Filter by runtime compatibility family (fixed enum) |
| `ingress_model_id` | string | — | Requested-model filter for ingress scope |
| `final_target_model_id` | string | — | Resolved-model filter for final-execution scope |
| `endpoint_id` | integer | — | Filter by endpoint ID |
| `terminal_target_id` | integer | — | Filter by actual Terminal Target ID |
| `group_by` | string | `none` | Scope-specific grouping such as `ingress_model`, `final_target_model`, `endpoint`, or `terminal_target` |
| `limit` | integer | 50 | Result limit; must be positive |
| `offset` | integer | 0 | Pagination offset |
| `top_n` | integer | 5 | Number of top spenders to return; must be positive |

Response `200`:

```json
{
  "summary": {
    "known_cost_micros": 1250000,
    "cost_sample_count": 1400,
    "cost_missing_count": 50,
    "successful_request_count": 1500,
    "priced_request_count": 1450,
    "unpriced_request_count": 50,
    "total_input_tokens": 50000,
    "total_output_tokens": 120000,
    "total_cache_read_input_tokens": 10000,
    "total_cache_creation_input_tokens": 1500,
    "total_reasoning_tokens": 2000,
    "total_tokens": 182000,
    "avg_cost_per_successful_request_micros": 833
  },
  "groups": [
    {
      "key": "gpt-4o",
      "total_cost_micros": 850000,
      "total_requests": 800,
      "priced_requests": 790,
      "unpriced_requests": 10,
      "total_tokens": 90000
    }
  ],
  "groups_total": 12,
  "top_spending_models": [
    {
      "model_id": "gpt-4o",
      "model_label": "GPT 4o",
      "total_cost_micros": 850000
    }
  ],
  "top_spending_endpoints": [
    {
      "endpoint_id": 12,
      "endpoint_label": "Primary OpenAI",
      "total_cost_micros": 740000
    }
  ],
  "unpriced_breakdown": {
    "PRICING_DISABLED": 30,
    "STREAM_USAGE_UNAVAILABLE": 12,
    "MISSING_TOKEN_USAGE": 8
  },
  "report_currency_code": "USD",
  "report_currency_symbol": "$"
}
```

`top_spending_models` rows carry both the stable `model_id` and the resolved `model_label` used for operator-facing displays. `model_label` reflects the current canonical model configuration label when one exists and otherwise falls back to `model_id`.

Spending summaries aggregate canonical disjoint token components independently. `total_input_tokens` is base input only, `total_output_tokens` is base output only, `total_cache_read_input_tokens`, `total_cache_creation_input_tokens`, and `total_reasoning_tokens` are separate split totals, and `total_tokens` uses provider totals when available.

Unpriced reasons distinguish pricing configuration gaps from observed usage gaps. `MISSING_PRICE_DATA` means the pricing template or pricing snapshot is absent, unusable, or invalid, or required FX data was missing or invalid. Explicit `"0"` prices mean configured free pricing and do not trigger `MISSING_PRICE_DATA`. `MISSING_TOKEN_USAGE` means a completed stream or non-stream response lacked required upstream token usage. `STREAM_USAGE_UNAVAILABLE` means a classified stream outcome made terminal usage unavailable and required tokens were absent. Prism doesn't estimate tokens or cost for usage gaps.

---

## 5. Audit API

Audit APIs are pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored.

### 5.0 API-Family Audit Settings

```
GET /api/settings/audit
PUT /api/settings/audit
```

`/api/settings/audit` is pinned to Default profile id `1`; `X-Profile-Id` is accepted for compatibility and ignored. `GET` returns exactly three rows in stable order: `openai`, `anthropic`, `gemini`. Missing persisted rows default to `audit_enabled=false` and `audit_capture_bodies=false`.

Response `200`:

```json
{
  "profile_id": 1,
  "settings": [
    { "api_family": "openai", "audit_enabled": true, "audit_capture_bodies": false },
    { "api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false },
    { "api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false }
  ]
}
```

`PUT` is a full replacement. The request must contain exactly one row for each supported family. Unknown families, duplicates, missing families, and `audit_enabled=false` with `audit_capture_bodies=true` reject before persistence.

Request:

```json
{
  "settings": [
    { "api_family": "openai", "audit_enabled": true, "audit_capture_bodies": true },
    { "api_family": "anthropic", "audit_enabled": false, "audit_capture_bodies": false },
    { "api_family": "gemini", "audit_enabled": false, "audit_capture_bodies": false }
  ]
}
```

Body capture is valid only when audit is enabled for that API family. Runtime loads the selected policy by profile and model `api_family` into request planning snapshots, then persists the request-time booleans in existing `audit_enabled_at_request` and `audit_capture_bodies_at_request` fields.

### 5.1 List Audit Logs

```
GET /api/audit/logs
```

Query parameters:

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| `request_log_id` | decimal string | none | Filter audit rows linked to one request log |
| `model_id` | string | none | Filter by model ID |
| `status_code` | integer | none | Filter by response status code |
| `endpoint_id` | integer | none | Filter by endpoint ID |
| `connection_id` | integer | none | Filter by connection ID |
| `from` | datetime | required | Inclusive start of bounded time range (RFC 3339) |
| `to` | datetime | required | Exclusive end of bounded time range (RFC 3339) |
| `limit` | integer | 50 | Positive result count; values above `200` are silently capped to `200` |
| `cursor` | string | none | Opaque keyset cursor returned as `next_cursor` |
| `sort` | string | `desc` | Only `desc` is supported |
| `anchor_id` | integer | none | Anchor an audit row outside the first page; the first response carries it exactly once as `anchor_item` (in-page or unknown anchors emit no `anchor_item`) |

The list API returns one row per upstream attempt. If a proxy request fails over across connections, each attempt has its own audit row. The `from` and `to` window is required and may not exceed 7 days, including when `request_log_id` is supplied. The backend has no fallback, default audit window, or legacy time-window aliases for request-log lookups. When `request_log_id` identifies an existing request whose request-time audit flag is disabled, the list returns `409` with `Audit capture unavailable for this request`. Unsupported query keys return `400` with `audit_filter_unsupported`.

Response `200`:

```json
{
  "items": [
    {
      "id": 1,
      "profile_id": 1,
      "request_log_id": "42",
      "request_log_created_at": "2025-01-15T10:30:00Z",
      "ingress_request_id": "ingress_req_42",
      "request_log_missing": false,
      "model_id": "gpt-4o",
      "endpoint_id": 12,
      "connection_id": 1,
      "endpoint_base_url": "https://api.openai.com",
      "endpoint_description": "Primary production key",
      "request_method": "POST",
      "request_url": "https://api.openai.com/v1/chat/completions",
      "request_headers": "[{\"name\":\"authorization\",\"value\":\"[REDACTED]\"},{\"name\":\"content-type\",\"value\":\"application/json\"}]",
      "request_body_preview": "{\"model\":\"gpt-4o\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello!\"}]}",
      "request_body_stored": true,
      "row_kind": "upstream",
      "attempt_number": 1,
      "upstream_status_code": 200,
      "gateway_status_code": null,
      "legacy_status_code": null,
      "response_body_stored": true,
      "is_stream": false,
      "attempt_duration_ms": 1234,
      "legacy_duration_ms": null,
      "audit_enabled_at_request": true,
      "audit_capture_bodies_at_request": true,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "next_cursor": "eyJ2IjoxLCJsYXN0X2NyZWF0ZWRfYXQiOiIyMDI1LTAxLTE1VDEwOjMwOjAwWiIsImxhc3RfaWQiOjF9.signature",
  "has_more": true,
  "window": {
    "from": "2025-01-15T00:00:00Z",
    "to": "2025-01-16T00:00:00Z"
  },
  "limit": 50,
  "sort": "desc",
  "anchor_item": null,
  "coverage": {
    "requested_from_time": "...",
    "requested_to_time": "...",
    "effective_from_time": "...",
    "effective_to_time": "...",
    "retention_from_time": "...",
    "complete": true,
    "gaps": [],
    "state": "known|legacy_unknown",
    "source_revision": "audit_logs-v1:..."
  }
}
```

Every successful audit list response carries a non-null `coverage` projection. `request_log_id` is a decimal JSON string so PostgreSQL `BIGINT` values remain exact in browsers; the audit row `id` remains numeric. Invalid-UTF-8 bodies return a null preview with `request_body_preview_unavailable_reason="invalid_utf8"`.
The list API returns `request_body_preview` instead of the full body. It keeps at most the first 4096 Unicode code points and does not append an ellipsis or another truncation marker. Use the detail API for full content.
If body capture was off at request time, `request_body_preview` is `null` even though the audit metadata still exists. `response_body_stored` means captured response bytes were stored, independent of `is_stream`; rows with `response_body_stored=false` have no stored response body. Audit rows preserve `request_log_id`, `request_log_created_at`, and `ingress_request_id` after request-log retention. `request_log_missing=true` means both request-log link fields are present but the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves. If either link field is null, `request_log_missing` is false.
Rows are ordered by `(created_at DESC, id DESC)`. Pagination is keyset-based: when `has_more=true`, pass the returned `next_cursor` with the same window, sort, and filters. The audit list response does not include `total` or `offset`.

### 5.2 Get Audit Log Detail

```
GET /api/audit/logs/{id}
```

Response `200` returns the audit identity, weak request links, model/connection/endpoint snapshots, request method and URL, request-time audit flags, and the scoped `row_kind`, `attempt_number`, `attempt_duration_ms`, `legacy_duration_ms`, `upstream_status_code`, `gateway_status_code`, and `legacy_status_code` fields.

The body wire fields are `request_body_base64` and `response_body_base64`. They encode the exact stored BYTEA prefixes; they are not JSON text bodies. Each direction also carries `*_body_stored`, `*_body_encoding` (`utf8|binary|unknown`), `*_body_capture_status`, `*_body_capture_provenance`, `*_body_capture_end_state`, `*_body_truncated`, `*_body_bytes_observed`, and `*_body_bytes_stored`. No text decoding is required to download the stored bytes. `request_headers` and `response_headers` are nullable JSON strings containing the canonical scrubbed header-entry arrays; URL truncation flags remain explicit.

Every audited attempt may retain its request body; only the final attempt retains a response body. `is_stream` does not prevent response capture. Body capture disabled at request time leaves both base64 fields `null`; binary or invalid-UTF-8 prefixes remain representable without loss. The browser decodes eligible UTF-8 for its readable-conversation projection; binary and unsupported content are explained without a raw-data UI. The raw-download routes below remain backend API capabilities with their existing exact-byte contract.

A missing row returns `404`. Disabled request-time audit returns `409` with `Audit capture unavailable for this request`; evidence below the published retention floor returns `410 audit_evidence_revoked`.

### 5.2A Raw Body Downloads

```
GET /api/audit/logs/{log_id}/body/request
GET /api/audit/logs/{log_id}/body/response
```

Returns the exact stored BYTEA prefix byte-for-byte with `Content-Length`, `Content-Disposition: attachment; filename=<safe-name>`, `Content-Type: application/octet-stream`, `X-Content-Type-Options: nosniff`, `Cache-Control: private, no-store`, `Content-Security-Policy: sandbox`, and the `X-Prism-Body-Truncated`/`X-Prism-Body-Bytes-Observed`/`X-Prism-Body-Bytes-Stored`/`X-Prism-Body-Capture-End-State` headers. Invalid UTF-8, NUL, and mid-codepoint truncation round-trip byte-for-byte. The current raw-body loader does not provide original content-type evidence. Missing rows return `404`; a retained row with no stored capture returns zero bytes, disabled request-time audit returns `409`, and revoked evidence returns `410 audit_evidence_revoked`.

### 5.3 Audit Log Retention

Audit log list and detail APIs remain pinned to Default profile id `1`. Normal audit-log cleanup is global: the stored `audit_logs_retention_days` value from `/api/settings/log-retention` plans automatic v2 jobs; one-time cleanup uses a manual-cleanup preflight plus sealed job under `/api/maintenance/log-retention/*`.

The retired audit cleanup endpoints are not part of the current API. Retention jobs return `202` with a job object, not a boolean acknowledgement.

Audit rows retain weak request metadata in `request_log_id`, `request_log_created_at`, and `ingress_request_id`; retention does not clear those fields. `request_log_missing=true` requires both request-log link fields and reports that the `(profile_id, request_log_id, request_log_created_at)` tuple no longer resolves, so a request detail link can be unavailable after request-log retention expires before audit-log retention.

### 5.3A Management Job Status and Cancel

```
GET /api/management/jobs
GET /api/management/jobs/{job_id}
POST /api/management/jobs/{job_id}/cancel
```

`GET /api/management/jobs?scope=global&type=log_retention` is the canonical durable job-center list for instance-wide v2 retention work. It resolves only `profile_id = 0` log-retention jobs and returns bounded keyset pages; the Settings UI does not infer job truth from browser state or a `status_url`. The general management-job routes may still resolve profile-owned audit-delete history, but they do not broaden the global retention list:

```json
{
  "items": [
    {
      "id": "job_0123456789abcdef01234567",
      "type": "log_retention",
      "contract_version": 2,
      "job_scope": "global",
      "origin": "automatic",
      "dataset": "request_logs",
      "state": "queued",
      "requested_at": "2026-04-19T12:00:00Z",
      "started_at": null,
      "finished_at": null,
      "cutoff": "2025-01-01T00:00:00Z",
      "purge_to_time": null,
      "progress": {
        "stage": "waiting_for_protection",
        "visibility_state": "scheduled_cutoff_active",
        "purge_state": "not_started",
        "boundary_rows_deleted": "0",
        "boundary_batches_completed": "0"
      },
      "attempt_count": 0,
      "last_heartbeat_at": null,
      "error": null
    }
  ],
  "has_more": false,
  "next_cursor": null
}
```

`GET /api/management/jobs/{job_id}` and `POST /api/management/jobs/{job_id}/cancel` accept the same global scope/type witness for v2 retention jobs. Queued manual jobs can be cancelled; running manual purges return `409 purge_not_cancellable`; automatic jobs accept `cancel_requested`. A worker-side preflight mismatch terminalizes the job with `preflight_stale_before_execution` before acquiring the purge fence, so a new server preflight is required. Unknown or out-of-scope jobs return `404`.

### 5.4 Redaction Rules

Header values are scrubbed at write time with the fixed safe-diagnostic matcher (`safediag`: exact sensitive names plus fragment-sensitive names, Bearer/Basic/JWT/API-key-like/key=value/URL-secret redaction) and stored as canonical sorted `[{name,value}]` entries with per-direction `request_headers_scrub_provenance`/`response_headers_scrub_provenance`. Legacy rows that predate the v2 scrubber are rewritten at upgrade time with `[REDACTED-LEGACY]` values (`legacy_all_values_redacted` or `legacy_rescrubbed` provenance); no raw legacy header value is served after the v2 upgrade.

Captured text request and response bodies can contain user-provided secrets or PII; image-operation bodies apply the payload redaction in [Request Audit Logging](observability-and-retention.md#81-concept). Body capture is request-time provenance via `audit_capture_bodies_at_request`; when disabled, stored bodies and their public `*_body_base64` projections are `null`.

### 5.5 Body Size Limits

When body capture is enabled, Prism can store the captured request body for each audited upstream attempt and the captured response body for the final attempt only. Audit list previews are limited to the first 4096 Unicode code points without an appended truncation marker; detail bodies are not truncated by this preview helper.

Body capture is bounded: the per-body cap is 4 MiB enforced during the copy (observed counts all bytes, stored retains the first 4 MiB prefix), request bodies share a 12 MiB per-ingress budget with a separate 4 MiB final-response reservation (16 MiB aggregate), and capture statuses are typed (`captured|truncated|omitted_ingress_budget` with observed/stored counts). Stored bodies are BYTEA prefixes; exact bytes are served by the raw download routes in §5.2A.

---
