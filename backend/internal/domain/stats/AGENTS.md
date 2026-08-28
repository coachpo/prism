# BACKEND DOMAIN STATS KNOWLEDGE BASE

## OVERVIEW

`backend/internal/domain/stats/` owns PostgreSQL-backed read models for dashboards, usage analytics, spending, request logs, recent activity, routing health, and retained filter options. Every metric response declares one of three grains: `ingress` (one `usage_request_events` row, requested-model identity), `final_execution` (a finalized usage row with resolved-target identity and final-attempt latency evidence), or `route_attempt` (one `request_logs` upstream row, attempt-result semantics, and no cost claim).

## STRUCTURE

```text
stats/
├── errors.go                     # Stats error contract
├── query_contract.go             # PostgreSQL query executor contract
├── query_values.go               # PostgreSQL scan/scalar projection conversions
├── request_log_scope.go          # Row-scoped request-log status/duration SQL
├── report_currency.go            # Report-currency projection read
├── read_model_math.go            # Shared time-preset, bucketing, percentile, and rate math
├── stats_catalog.go              # Current endpoint/connection catalog and Terminal Target label resolution
├── user_agent_rules.go           # Compiled User-Agent Client Rule loading and caller display classification
├── classifier.go                 # Canonical outcome and pricing-status classifier reused by every read model
├── aggregates.go                 # Stats summary aggregation
├── throughput.go                 # Request and dashboard throughput metrics
├── model_metrics.go              # Batch model metrics
├── endpoint_model_statistics.go  # Endpoint model statistics
├── spending.go                   # Spending report aggregation
├── usage_event_records.go        # Usage-event loading, records, pricing, and endpoint projections
├── types.go                      # JSON-facing read-model types
├── dashboard_health.go           # Dashboard freshness/coverage helper types
├── dashboard_snapshot_builder.go # Overview dashboard aggregate snapshot
├── dashboard_aggregate_store.go  # Per-profile dashboard aggregate snapshot cache
├── dashboard_recent_activity.go  # Bounded request-history activity feed
├── observe_models.go             # Dashboard-now read model
├── observe_usage_summary.go      # Observe usage summary and cost sparkline read model
├── observe_query.go              # Actual-coverage bounds resolution
├── observe_query_context.go     # Opaque query-context signing and verification
├── observe_series.go             # Usage series, interval resolution, Top N + Other breakdowns
├── observe_errors.go             # Usage errors summary/timeline/ranking with canonical deep-link filters
├── observe_activity.go           # Finalized ingress activity feed (never rebuilt from attempt rows)
├── observe_usage_summary_segments.go # Window-scoped cost-segment CTE fragment for the summary statement
├── query_coverage.go             # Non-null Requests/Audit coverage union
├── request_logs.go               # Attempt-view list projections and scoped filters
├── request_logs_chain.go         # Retained ingress-chain view
├── request_logs_chain_cursor.go  # Ingress-chain cursor signing
├── request_logs_chain_cohort.go  # Ingress-chain cohort predicates
├── request_logs_chain_summary.go # Finalized ingress summaries
├── request_logs_chain_coverage.go# Ingress-chain coverage projections
├── request_logs_chain_rows.go    # Retained ingress rows
├── request_logs_export.go        # Server-side full filtered CSV export (RR snapshot, bounds, digest)
├── request_logs_detail.go        # Exact request detail: scoped statuses, failure projection, pricing layers
├── cost_segments.go              # Canonical cost-segment catalogue (e.N / l.AAA / l.__unknown__)
├── cost_segment_cursor.go        # Signed cost-segment cursor payload and signing-key derivation
├── cost_segment_symbols.go       # Bounded offset page of observed symbols per cost segment
├── snapshot.go                   # Usage snapshot read model and snapshot-event projection
├── terminal_targets.go           # Bounded Terminal Target drill-down statistics
├── proxy_api_key_options.go      # Bounded proxy-key filter-option union
├── retention_source.go           # Retention source and actual-coverage owner projection
└── *_test.go                     # Classifier, snapshot, cursor, and coverage coverage
```

## WHERE TO LOOK

- Dashboard aggregate snapshot and routing health map: `dashboard_snapshot_builder.go`
- Dashboard aggregate snapshot cache and snapshot revision: `dashboard_aggregate_store.go`
- Recent activity feed and watermarks: `dashboard_recent_activity.go`
- Request-log attempt list/detail, chain view, CSV export, and cost segments: `request_logs.go`, `request_logs_chain.go`, `request_logs_export.go`, `request_logs_detail.go`, `cost_segments.go`, `types.go`
- Ingress-chain cursor signing: `request_logs_chain_cursor.go`
- Ingress-chain cohort predicates: `request_logs_chain_cohort.go`
- Finalized ingress summaries: `request_logs_chain_summary.go`
- Ingress-chain coverage projections: `request_logs_chain_coverage.go`
- Retained ingress rows: `request_logs_chain_rows.go`
- Usage snapshot, spending, endpoint/model/proxy-key aggregates: `snapshot.go`, `spending.go`, `endpoint_model_statistics.go`, `model_metrics.go`
- Report-currency projection: `report_currency.go`; `observe_models.go`, `snapshot.go`, and `spending.go` consume the same read contract
- Observe usage summary and cost segments: `observe_usage_summary.go`, `observe_usage_summary_segments.go`
- Canonical cost-segment generation and validation: `cost_segments.go`, `cost_segment_symbols.go`, `classifier.go`; the SQL generator and `CostSegmentKeyFor` must classify epoch 0, non-canonical currency codes, and NULL codes identically.
- Observe query-context bounds/signing: `observe_query.go`, `observe_query_context.go`
- Stats summary and throughput metrics: `aggregates.go`, `throughput.go`
- Shared usage-event loading/scanning: `usage_event_records.go`
- Usage snapshot latency trends expose hourly/daily p50 and p95 `response_time_ms` buckets through `latency_trends` beside request and token trends.
- HTTP management consumers: `../../httpapi/management/stats/`

## CONVENTIONS

- Keep this package HTTP-neutral. Selected-profile parsing, query params, and response writing stay in `httpapi/management/stats`.
- Use retained history as the source of truth: `request_logs`, `usage_request_events`, and endpoint label snapshots.
- Preserve server field names in JSON types; frontend contracts mirror these names.
- Keep request-log filtering aligned with current product semantics: `ingress_model_id` selects the requested entry model, `attempt_target_model_id` selects the retained attempt target, signed `final_target_model_id` selects the finalized owner, and `client_rule_id` matches caller User-Agent Client Rules. `row_kind` preserves the planning/admission/upstream/legacy boundary (route-attempt deep links pin `upstream`), and `pricing_status` is the four-state pricing filter (the retired `priced` boolean alias is rejected as an unknown query key).
- Keep row scoping strict in every read model: `upstream_status_code`/`gateway_status_code`/`legacy_status_code` and `attempt_duration_ms`/`legacy_duration_ms` are selected by `row_kind`; never COALESCE across scopes in public DTOs.
- Keep the Observe retention source authoritative: `retention_source.go` projects `log_retention_policy_resources` per dataset (configured UTC-day cutoff, published floor, revocation epoch, purge state). Observe query contexts, ordinary Requests reads, Audit coverage, Events, manual purge final publish, and the Settings projection all consume this source; never re-derive a floor from policy days or `MIN(created_at)`, and fail closed with the owning 503 while `running|recovery_required`.
- Keep actual coverage owner-authored: the owner materialization cut is `{kind, committed/raw cut, optional manifest/build identity}`; `RefreshActualCoverageProjection` is the only bounded aggregate refresh, and `RecordActualCoverageAppend` is the same-transaction append handoff. Consumers must preserve `coverage_revision`, `coverage_hash`, `source_revision`, generation/fence, freshness, and explicit gaps instead of synthesizing intervals or zero counts.
- Keep query contexts opaque and server-validated: fragments never re-parse presets; a manual-purge final publish revokes older tokens via the retention epoch (`410 dataset_snapshot_revoked`).
- Keep the chain view server-owned: whole-ingress outer pages with signed chain cursors, bounded retained-row inner pages, and finalized-summary facts from `usage_request_events` only.
- Keep CSV export server-side from a single `READ ONLY REPEATABLE READ` snapshot with typed rejection before any file bytes.
- Keep pricing and usage-source math in backend read models. Frontend tables render supplied values; they do not recalculate totals.
- Retained request-log filters for `cost_segment_key`, `pricing_card_role`, and `pricing_selection_state` must validate at the HTTP boundary and reuse the canonical domain predicates; `cost_segment_key` alone must not switch the ingress source away from retained request logs.
- Observe cost segments may carry a selected-card-role breakdown (including peak/offpeak); derive it from trusted usage-event evidence in the same aggregate statement and preserve NULL for an untrusted or absent cost.
- Dashboard overview and recent activity are separate read models; do not fold recent activity into aggregate snapshots.
- Keep `scope.go` authoritative for public scope, group, filter, caliber, sample/missing, and dataset-coverage semantics. Retired ambiguous `model`/`model_id` query keys reject with typed 422; use the scope-specific ingress/final/attempt model names.
- Canonical money is always `pricing_status=priced AND pricing_evidence_trust=trusted`. Ingress and final-target groupings are non-additive projections of the same served-final fact; route-attempt metrics expose no cost. A trusted zero is a non-null zero with a positive cost sample count, while no trusted sample is null.
- Final-execution latency is associated by `(profile_id, ingress_request_id, final_attempt_number)` to raw `request_logs.attempt_duration_ms`. Keep usage/request coverage separate and expose a missing latency sample when the request row was retained for less time.
- Project non-positive retained endpoint, connection, Terminal Target, and proxy-key IDs as unattributed/null without rewriting retained history.

## ANTI-PATTERNS

- Do not add HTTP handlers or route parsing here.
- Do not duplicate stats aggregation in frontend code.
- Do not use mutable endpoint labels when retained `endpoint_label_snapshot` is required for historical reporting.
