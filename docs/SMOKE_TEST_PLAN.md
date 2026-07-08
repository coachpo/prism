# Smoke Test Plan: Prism (Comprehensive)

## 1. Scope and Goals

This smoke test plan validates all documented workflows and core function paths across:

- Backend API contract
- Proxy behavior (selector-driven target selection, load balancing, failover, streaming)
- Health detection
- Statistics and token extraction
- Audit logging and redaction
- Header blocklist and sanitization
- Disaster recovery backup guidance
- Batch data deletion semantics
- Frontend management flows
- Token costing and spending reports

The objective is a fast but thorough confidence pass that catches regressions before release.

---

## 2. Source Documents Covered

This plan is synthesized from:

- `docs/API_SPEC.md`
- `docs/ARCHITECTURE.md`
- `docs/DATA_MODEL.md`
- `docs/PRD.md`
- deployment notes in `README.md` plus the backend/frontend service setup already checked into this monorepo

---

## 3. Test Strategy

### 3.1 Priority Tiers

- `P0` release gate: must pass before merge/release.
- `P1` extended smoke: should pass in nightly/manual extended run.

### 3.2 Execution Lanes

- API smoke lane (backend only, deterministic mock upstreams).
- UI smoke lane (backend + frontend, browser automation/manual).
- Destructive lane (import and delete tests in isolated DB).

### 3.3 Data Isolation

- Use dedicated PostgreSQL database/schema for smoke runs (isolated from dev/prod data).
- Reset DB between destructive scenarios.
- Never run destructive tests on production-like DB.

---

## 4. Environment Prerequisites

- Go 1.26.4 toolchain, Node `24+`, pnpm `10.30.1`, Docker, and Docker Compose.
- When using the checked-in launcher, backend available at `http://localhost:8000` from the checked-in `config.json` and frontend at `http://localhost:5173`.
- The root launcher binds local PostgreSQL on host port `15432` for local orchestration.
- Upstream behavior controlled by test doubles or known test endpoints.
- At least one active model with enabled access targets for each runtime `api_family` under test.

Suggested startup:

```bash
# backend only
./start.sh headless

# full stack
./start.sh full

```

---

## 5. Baseline Fixture Setup

Prepare seed state through API (not manual DB edits):

2. The seeded Default profile exists with id `1`; profile-management routes are removed.
3. Profile-scoped Endpoints (credentials):
   - in Default profile id `1`: OpenAI, Anthropic, and Gemini endpoints as needed by the case set
4. Profile-scoped models:
   - in Default profile id `1`: OpenAI, Anthropic, and Gemini-family models as needed by the case set, with 2+ reachable private connection targets for failover cases
5. Unified access targets:
   - same-`api_family` model-target chains plus private connection targets using ordered `access_targets` (`target_type`, `target_model_id`, `connection_ref`, `position`, `is_enabled`) in Default profile id `1`. `position` is ordering only, not priority, tier, or weight
6. Private connection diversity in Default profile id `1`:
   - active + inactive
   - different model target positions
   - one connection with `custom_headers`
   - one connection assigned a `pricing_template_id`
7. Audit toggles initially disabled, then enabled per-case.
8. Duplicate `model_id` and endpoint `name` validation is within Default profile id `1`; multi-profile duplicate coverage is frozen.

---

## 6. API Surface Coverage Matrix

| Endpoint | Coverage IDs |
|---|---|
| `GET /health` | A04 |
| `GET /api/models` | B04, E12, M03, M12 |
| `POST /api/models/by-endpoints` | B04A, M03 |
| `GET /api/models/{id}` | B18, M03 |
| `POST /api/models` | B04-B10, M12 |
| `PUT /api/models/{id}` | B08-B10, M03 |
| `DELETE /api/models/{id}` | B11, M03 |
| `GET /api/endpoints` | B12, M03 |
| `GET /api/endpoints/connections` | B12B, M03 |
| `POST /api/endpoints` | B13, M03 |
| `POST /api/endpoints/{endpoint_id}/duplicate` | B13B, M03 |
| `PUT /api/endpoints/{endpoint_id}` | B14, M03 |
| `PATCH /api/endpoints/{endpoint_id}/position` | B14A, M03 |
| `DELETE /api/endpoints/{endpoint_id}` | B15, M03 |
| `GET /api/connections` | B18, M03 |
| `GET /api/connections/{id}` | B18, M03 |
| `GET /api/connections/{id}/references` | B21B, M03 |
| `POST /api/models/{model_config_id}/connections` | B16-B17, L01-L02, M03 |
| `PATCH /api/models/{model_config_id}/connections/{connection_id}` | B19-B20, L03, L24, M03 |
| `DELETE /api/models/{model_config_id}/connections/{connection_id}` | B21-B21A, M03 |
| `GET /api/models/{model_config_id}/targets` | B18A, M03 |
| `POST /api/models/{model_config_id}/targets` | B18A, M03 |
| `PUT/PATCH /api/models/{model_config_id}/targets/{target_id}` | B20A, M03 |
| `PATCH /api/models/{model_config_id}/targets/{target_id}/position` | B20A, M03 |
| `DELETE /api/models/{model_config_id}/targets/{target_id}` | B20A, M03 |
| `GET /v1/models` | C15, M11-M13 |
| `POST /v1/chat/completions` | C01, C03, C04, C06-C14, E08, E10, L08-L10, M11-M13, M21 |
| `POST /v1/responses` | C01, C03, C04, C06-C14, E08, E10, L08-L10, M11-M13, M21 |
| `POST /v1/responses/input_tokens` | C16, E08, L08-L10, M11-M13 |
| `POST /v1/responses/compact` | C17, E08, L08-L10, M11-M13 |
| `POST /v1/messages` | C02, C04, E08, E10, L08-L10, M11-M13, M21 |
| `POST /v1/messages/count_tokens` | C20, E08, L08-L10, M11-M13 |
| `POST /v1beta/models/{model}:generateContent` | C03, E08, L08-L10, M11-M13 |
| `POST /v1beta/models/{model}:streamGenerateContent` | C21, E08, L08-L10, M11-M13 |
| `POST /v1beta/models/{model}:countTokens` | C22, E08, L08-L10, M11-M13 |
| `GET /api/stats/requests` | E01-E04, M14 |
| `GET /api/stats/endpoints/{endpoint_id}/models` | E16, M14 |
| `GET /api/stats/summary` | E05-E06, M14 |
| `GET /api/stats/dashboard` | E14, M14 |
| `POST /api/stats/models/metrics` | E13, M14 |
| `GET /api/stats/connection-success-rates` | E07 |
| `GET /api/stats/throughput` | E15, M14 |
| `GET /api/stats/spending` | L11-L13, L19-L20, M19 |
| `GET /api/settings/audit` | L31, M19 |
| `PUT /api/settings/audit` | L32, M19 |
| `GET /api/settings/log-retention` | G01, M14 |
| `PUT /api/settings/log-retention` | G01-G02, M14 |
| `POST /api/maintenance/log-retention/jobs` | G03-G15, G20, M14-M15 |
| `GET /api/audit/logs` | F10, F12, M15 |
| `GET /api/audit/logs/{id}` | F11, M15 |
| `GET /api/management/jobs` | F13, G03-G15, G20, M15 |
| `GET /api/management/jobs/{job_id}` | F13, G03-G15, G20, M15 |
| `POST /api/management/jobs/{job_id}/cancel` | F13, G03-G15, G20, M15 |
| `GET /api/loadbalance/current-state` | G18, M14 |
| `POST /api/loadbalance/current-state/{connection_id}/reset` | G19, M14 |
| `GET /api/loadbalance/events` | G16, M14 |
| `GET /api/loadbalance/events/{id}` | G17, M14 |
| `GET /api/config/header-blocklist-rules` | K01, M20 |
| `GET /api/config/header-blocklist-rules/{id}` | K05-K06, M20 |
| `POST /api/config/header-blocklist-rules` | K02-K04, K12-K15, M20 |
| `PATCH /api/config/header-blocklist-rules/{id}` | K07-K09, M20 |
| `DELETE /api/config/header-blocklist-rules/{id}` | K10-K11, M20 |
| `GET /api/config/user-agent-client-rules` | K37, M20 |
| `GET /api/config/user-agent-client-rules/{id}` | K38, M20 |
| `POST /api/config/user-agent-client-rules` | K39, M20 |
| `PATCH /api/config/user-agent-client-rules/{id}` | K40-K41, M20 |
| `DELETE /api/config/user-agent-client-rules/{id}` | K42-K43, M20 |
| `GET /api/settings/costing` | L04, M19 |
| `GET /api/settings/timezone` | L29, M19 |
| `GET /api/pricing-templates/{id}/connections` | L26, L28 |
| `DELETE /api/pricing-templates/{id}` | L26-L27 |
| `PUT /api/pricing-templates/{id}` | L02, L25 |
| `POST /api/pricing-templates` | L01, L25 |
| `GET /api/pricing-templates` | L01, L25 |
| `PUT /api/settings/costing` | L05-L07, M19 |
| `PUT /api/settings/timezone` | L30, M19 |
| `GET /api/auth/status` | N01 |
| `GET /api/auth/public-bootstrap` | N02 |
| `POST /api/auth/login` | N03 |
| `POST /api/auth/logout` | N04 |
| `POST /api/auth/refresh` | N05 |
| `GET /api/auth/session` | N06 |

---

## 7. Detailed Test Cases

## A. Startup and Deployment

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| A01 | P0 | Start backend in `headless` mode | Backend process starts, API reachable |
| A02 | P0 | Start in `full` mode | Backend + frontend reachable |
| A03 | P0 | First boot with empty DB | DB created and baseline profile seeded |
| A04 | P0 | `GET /health` | `200`, JSON contains `status=ok` and a non-empty `version` string |
| A05 | P1 | Backend-served API documentation surface | Not exposed by the backend |
| A06 | P1 | CORS preflight | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; explicit backend base URLs remain available for standalone frontend workflows |
| A07 | P1 | Root Docker Compose bundle | `docker compose up --build` exposes the public Prism port, `GET /health` succeeds through Nginx, SPA fallback serves frontend assets, and `/api`, `/v1`, and `/v1beta` proxy paths reach the private backend upstream |
| A08 | P1 | Single-image `BUILD_FRONTEND=false` fallback | Root Dockerfile build with `BUILD_FRONTEND=false` serves the fallback page while `/health`, `/api/*`, `/v1`, and `/v1beta` proxy paths still reach the private backend upstream |

## B. Configuration CRUD and Validation

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| B04 | P0 | Create model | `201`, model stored |
| B05 | P0 | Create duplicate `model_id` in same effective profile | `409` |
| B06 | P0 | Create valid model with ordered access targets | `201` |
| B07 | P0 | Model missing/invalid access target metadata | `400` |
| B08 | P0 | Cross-api-family model target | `400` |
| B09 | P0 | Access target cycle | `400` |
| B10 | P0 | Enabled model with no enabled access targets | `400` |
| B11 | P0 | Delete model referenced by another model target | `409` with referrer detail |
| B12 | P0 | List profile-scoped endpoints | `200`, returns array scoped to effective profile |
| B12A | P0 | List profile-scoped endpoints preserves persisted order | Response order follows `position ASC, id ASC` |
| B12B | P0 | List endpoint connections dropdown | `200`, returns connection dropdown items scoped to the effective profile |
| B13 | P0 | Create profile-scoped endpoint | `201`, endpoint stored in effective profile |
| B13A | P0 | Create profile-scoped endpoint appends position | New endpoint gets the next contiguous `position` |
| B13B | P0 | Duplicate profile-scoped endpoint | `201`, new endpoint created with "Copy of" name |
| B14 | P0 | Update profile-scoped endpoint | `200`, changes persist in effective profile |
| B14A | P0 | Move profile-scoped endpoint | `200`, returns reordered list; no-op stays stable; out-of-range `to_index` returns `422` |
| B15 | P0 | Delete profile-scoped endpoint in use | `409` conflict |
| B15A | P0 | Delete profile-scoped endpoint compacts later positions | Remaining endpoints are renumbered to contiguous `0..N-1` |
| B16 | P0 | Create Terminal Target | `201`, compatibility connection inherits owner model `api_family` |
| B16A | P0 | Reject public connection target assignment | `400`, no arbitrary connection ID is attached to a model |
| B17 | P0 | Reject conflicting Terminal Target `api_family` | `400` |
| B18 | P1 | List Terminal Targets | `200`, returns profile-scoped compatibility connections with endpoint and pricing metadata |
| B18A | P0 | List model access targets | `200`, returns ordered model and connection targets |
| B19 | P0 | Update Terminal Target through owner-scoped route with `custom_headers=null/{}` | Headers removed |
| B20 | P1 | Update Terminal Target through owner-scoped route omitting `custom_headers` | Existing headers retained |
| B20A | P0 | Reorder model access target | `200`, returns reordered targets; no-op stays stable; wrong model/profile combo returns `404`; out-of-range `to_index` returns `422` |
| B20B | P0 | Compatibility connection payload containing access-target ordering fields | `422` validation error |
| B21 | P1 | Delete Terminal Target through owner-scoped route | `200`, compatibility connection and owner target row removed together |
| B21A | P0 | Delete final enabled Terminal Target from enabled model | `400` until another enabled target exists or the model is disabled |
| B21B | P0 | Read Terminal Target references | `200`, returns the compatibility connection owner model target row |

## C. Runtime Routing, Unified Access Targets, Headers, and Ban Policy

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| C01 | P0 | OpenAI-compatible proxy call | Upstream response proxied as-is |
| C02 | P0 | Anthropic non-stream proxy call | Upstream response proxied as-is |
| C03 | P1 | Gemini route compatibility | Correct routing and auth behavior |
| C04 | P0 | Unified access-target model request | Routed through ordered model and Terminal Target compatibility edges; requested model and final target model are logged separately |
| C05 | P0 | Unknown/disabled model | `404` |
| C06 | P0 | Fill-first routing order | Lowest-position eligible target wins when Ban Policy state is otherwise equal |
| C07 | P0 | Ban Policy retry-window path | First transient failure increments retry counters without blocking; threshold hit opens a retry window with backoff and jitter; expired windows allow the Terminal Target to be selected again in normal order |
| C08 | P0 | Failover on configured Ban Policy status codes | Next Terminal Target attempted for the default set `403/422/429/500/502/503/504/529`, and status-code behavior follows the attached strategy |
| C09 | P0 | Failover on connection error/timeout | Next Terminal Target attempted; failure kind classified (`connect_error` / `timeout`) for Ban Policy state |
| C10 | P0 | Non-failover client error while Ban Policy state exists | Request returns upstream error; existing retry-window state is not force-cleared |
| C11 | P0 | All failover attempts fail | `502` with last error detail |
| C12 | P0 | No active Terminal Target compatibility edges | `503` |
| C13 | P1 | Header merge order with custom override | Custom headers win over ordinary forwarded headers but cannot override proxy-controlled auth/version headers |
| C14 | P1 | Terminal Target `custom_headers` override | Effective headers follow override |
| C15 | P0 | OpenAI local models list | `GET /v1/models` returns an OpenAI-shaped list of enabled OpenAI models for frozen Default profile id `1` without contacting upstream |
| C16 | P0 | OpenAI Responses input-token operation | `POST /v1/responses/input_tokens` is allowlisted, routes only to responses-capable targets, and persists token-count usage |
| C17 | P0 | OpenAI Responses compact operation | `POST /v1/responses/compact` is allowlisted, routes only to responses-capable targets, and extracts Responses usage |
| C20 | P0 | Anthropic token-count operation | `POST /v1/messages/count_tokens` is allowlisted and persists top-level `input_tokens` as token-count usage |
| C21 | P0 | Gemini stream generate operation | `POST /v1beta/models/{model}:streamGenerateContent` is streaming by route even without `stream: true` in the body |
| C22 | P0 | Gemini count tokens operation | `POST /v1beta/models/{model}:countTokens` is allowlisted and persists top-level token-count usage |
## D. Runtime URL Failsafe

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| D01 | P1 | Runtime endpoint path joining | Endpoint path prefixes are preserved, operation paths are appended, and no version-segment de-duplication is applied |

## E. Statistics and Token Extraction

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| E01 | P0 | Successful request logging | `request_logs` row exists with required fields |
| E02 | P0 | Failover attempt logging | Both failed and successful attempts logged |
| E03 | P0 | Request log filters (`ingress_request_id`, `model_id`, `client_rule_id`, `resolved_target_model_id`, `status_family`, `endpoint_id`, `from_time`) | Correct subsets returned; `client_rule_id` matches caller user agent only |
| E04 | P0 | Pagination (`limit`, `offset`, `total`) | Consistent counts and windows |
| E05 | P0 | Summary without `from_time` | Uses all historical data |
| E06 | P1 | Summary grouping (`model/api_family/endpoint`) | Groups and aggregates correct with endpoint labels from stored snapshots |
| E07 | P1 | Connection success-rate API | Values match request logs |
| E08 | P0 | Non-stream token extraction | Token fields match provider format rules |
| E09 | P1 | Unsupported/malformed usage fallback | Token fields null |
| E10 | P0 | Stream token extraction | Token fields populated |
| E11 | P1 | Streaming without usage fields | Token fields null |
| E12 | P0 | Model health fields in `/api/models` | Success rate and request totals are grouped by requested `request_logs.model_id` |
| E13 | P0 | Model metrics batch API | Returns metrics for multiple models |
| E14 | P0 | Dashboard aggregate stats API | `GET /api/stats/dashboard` returns the canonical stats-only overview snapshot with `snapshot_revision`, diagnostic `source_watermark`, `metric_snapshot`, `api_family_rows`, top spending models, strategy-family counts, and backend-computed `routing_health_map`; recent activity is absent from this payload |
| E14A | P0 | Dashboard recent activity API | `GET /api/stats/dashboard/recent-activity?limit=N` returns `{ generated_at, activity_watermark, items }`, defaults to 12 items, caps at 50, and orders by newest request history first |
| E15 | P1 | Throughput API | Returns aggregate RPM metrics plus time buckets for the selected scope |
| E16 | P0 | Endpoint model statistics API | Returns per-model counts, success rates, TTFT percentiles, token totals, and cost for the selected endpoint scope |

## F. Audit Logging

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| F01 | P0 | Audit disabled for request | No audit row created |
| F02 | P0 | Audit enabled + body capture enabled | Request/response metadata and bodies recorded |
| F03 | P0 | Body capture disabled | Bodies stored as null |
| F04 | P0 | Streaming audited request with body capture | Raw captured SSE response body stored when bytes are captured; metadata-only or empty captures keep `response_body` null |
| F05 | P0 | Failover with audit enabled | One audit row per upstream attempt |
| F06 | P0 | Redaction exact headers | Values redacted before storage |
| F07 | P1 | Redaction by name pattern | Values redacted |
| F08 | P1 | Non-sensitive headers | Preserved |
| F09 | P0 | Audit body capture storage | Captured request/response body strings are stored when body capture is enabled; no documented truncation marker is required |
| F10 | P0 | Audit list API | Requires bounded `from`/`to` window, caps windows at 7 days, returns `request_body_preview` max 200 chars, and orders by `(created_at DESC, id DESC)` |
| F11 | P0 | Audit detail API | Full row returned; unknown id is `404` |
| F12 | P0 | Audit filters/pagination | Correct subsets, keyset `next_cursor`, `has_more`, and no audit `offset` or `total` response contract |
| F13 | P0 | Audit delete job validation | `202` creates an async job with `job_id`, `state`, `status_url`, and `Location`; invalid body, scope, reason, or idempotency returns `400` |
| F14 | P1 | Audit non-interference on write failure | Proxy response unaffected |
| F15 | P0 | Orphan audit row visibility | Audit rows with null `request_log_id` remain visible in audit APIs and keep request-time provenance |

## G. Global Log Retention and Weak Link Semantics

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| G01 | P0 | Get global log-retention settings | `200`, returns day policies for `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`; no profile scope |
| G02 | P0 | Update global log-retention settings | `200`, persists nullable day policies for all retention tables; invalid day values return `400` |
| G03 | P0 | Create retention job with missing table | `400` |
| G04 | P0 | Request-log retention job with stored policy | `202`, returns `{ job_id, state, status_url, scope }` and a `Location` header; job scope is global |
| G05 | P0 | Request-log retention preserves audit provenance | Audit rows may outlive request logs and retain weak `request_log_id`, `request_log_created_at`, and `ingress_request_id` metadata |
| G06 | P0 | Request-log retention with explicit cutoff | `202`, computes partition cleanup against the supplied cutoff and exposes job progress through management job APIs |
| G07 | P0 | Retention job rejects invalid table or scope | `400` |
| G08 | P0 | Retention job rejects conflicting cutoff and delete-all modes | `400` |
| G09 | P0 | Request-log delete-all retention mode | Retention flow removes and recreates or reboots partitions for `request_logs`; it does not use a parent-root table delete |
| G10 | P0 | Audit-log retention with explicit cutoff | `202`, returns `{ job_id, state, status_url, scope }` and a `Location` header; cleanup is global, not profile-scoped |
| G11 | P0 | Audit-log delete-all retention mode | Retention flow removes and recreates or reboots `audit_logs` partitions while preserving independent request-log data |
| G12 | P0 | Audit/request weak linkage after uneven retention | Audit list/detail rows remain visible even when request-log detail linkage is missing |
| G13 | P0 | Loadbalance-event retention with explicit cutoff | `202`, creates a global retention job for `loadbalance_events` |
| G14 | P0 | Loadbalance-event delete-all retention mode | Retention flow removes and recreates or reboots `loadbalance_events` partitions |
| G15 | P0 | Boundary-partition cleanup | Whole expired daily child partitions are dropped; only the cutoff-overlapping child receives bounded cleanup plus vacuum |
| G16 | P0 | List loadbalance events | Returns events for a model |
| G17 | P0 | Get loadbalance event detail | Returns full event metadata |
| G18 | P0 | List current loadbalance state for a model | Returns derived per-connection current-state rows in effective profile scope |
| G19 | P0 | Reset current loadbalance state for a connection | `200`, returns `{ connection_id, cleared }`; idempotent when no row exists |
| G20 | P0 | Statistics retention for usage events | `202`, creates a global retention job for `usage_request_events` with job metadata and status URL |

## H. Disaster Recovery Backup Guidance

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| H01 | P0 | PostgreSQL backup command documented | Operator docs point to `pg_dump` for PostgreSQL-backed state |
| H02 | P0 | Startup config backup documented | Operator docs include copying the selected plaintext `config.json` |
| H03 | P1 | Dashboard omits removed backup workflow | Settings UI no longer offers a configuration transfer section |

## I. Frontend Workflow Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| I01 | P0 | Sidebar navigation | All routes load |
| I02 | P0 | Dashboard + Models success rate badges | Correct color thresholds and `N/A` |
| I03 | P0 | Model detail connection success badge + tooltip | Correct counts, rates, request-derived status detail |
| I04 | P0 | Private connection actions | Create/edit/delete toast or banner reflects result |
| I05 | P0 | Statistics cards and request table | Data renders and updates |
| I06 | P0 | Statistics "All" time range consistency | Summary totals align with table totals |
| I07 | P0 | Statistics api_family filter | Only OpenAI/Anthropic/Gemini options |
| I08 | P0 | Audit list/filter/detail UI | Works end-to-end; stream notice shown; request-time provenance distinguishes disabled, metadata-only, and full capture |
| I09 | P0 | Settings audit toggles | Persist and reflect backend; active-request provenance stays frozen after toggles |
| I10 | P0 | Settings data management preset buttons | Correct API calls and toasts |
| I11 | P1 | Connection custom header editor | Add/remove/persist roundtrip |
| I12 | P1 | Frontend error details | Backend `detail` surfaced to user |
| I12A | P0 | Access-target drag reorder in Model Detail | Drop updates target order immediately, persists after refresh, and rollback toast appears on API failure |
| I12B | P0 | Access-target reorder disabled during active filter | Drag handles disable and helper text explains how to re-enable ordering |
| I12C | P0 | Private connection dialog ordering UX | Dialog exposes no numeric priority field and explains that owner model access-target order controls routing |
| I13 | P0 | Settings data management custom days flow | Custom day input validates, calls API correctly |
| I14 | P0 | Settings data management delete-all flow | Confirmation dialog shows "ALL", calls `delete_all=true` API |
| I15 | P0 | Settings data management in-flight disable | All delete buttons disabled during active deletion |
| I16 | P0 | Model detail connection pricing template selector | Template assignment saves and reloads correctly |
| I17 | P0 | Settings costing and currency card | Report currency + symbol load/save |
| I18 | P0 | Settings FX mapping editor | Add/remove mapping enforces unique `(model_id, endpoint_id)` |
| I19 | P0 | Statistics spending tab filters and pagination | Controls update data correctly |
| I20 | P0 | Statistics operations request log costing columns | Breakdown columns render without UI regressions |
| I21 | P0 | Operations special-token row filter behavior | Filter only changes request-log rows |
| I22 | P0 | Null-vs-zero rendering in request log metrics | Null values render `N/A`, zero renders as `0` |
| I23 | P0 | Spending "Special Tokens Captured" card correctness | Card shows cached total and detail |
| I24 | P0 | Responsive token visibility below `xl` | Compact `Usage` column shows summary |
| I25 | P0 | No-regression check for existing costing indicators | Existing spend columns still render correctly |
| I26 | P0 | Dashboard polling recent activity | New request appears in Recent Activity after the next REST poll or manual refresh |
| I27 | P0 | Request logs manual refresh and exact-request mode | Refresh reloads the current server slice; `request_id` mode fetches only the targeted request |
| I28 | P0 | Loadbalance events tab REST refresh | Refresh or page revisit loads the latest failover and Ban Policy events for the model |
| I29 | P0 | Dedicated request audit page lookup | Opening `/observe/requests/:requestId/audit` triggers linked-audit lookup, skips fetches when audit was disabled for that request, and reports empty, missing, list-error, or detail-error states without fetching audit payloads in the overview drawer |
| I30 | P0 | Dashboard REST reconciliation | Dashboard refetches ground truth on poll/manual refresh and ignores stale snapshots |
| I31 | P1 | Dashboard REST payload split | `GET /api/stats/dashboard` refreshes summary, api_family, spending, throughput, and routing data; `GET /api/stats/dashboard/recent-activity` refreshes recent activity without forcing a snapshot rebuild |
| I32 | P1 | Recent activity insertion animations | New dashboard REST-polled rows animate on insert, with reduced-motion fallback showing static highlight only |
| I33 | P1 | Request-log scroll preservation | Virtualized browsing preserves scroll position while filters, page changes, or detail selection update the view |
| I34 | P1 | Request-log retained-filter state | Changing one retained filter preserves unrelated URL-backed filter state, resets pagination, and refreshes the server-backed slice without client-side refinement gating |
| I35 | P0 | Frontend locale switch (public + protected) | Switching between `en` and `zh-CN` updates shell and page copy, persists across refresh, and updates `document.documentElement.lang` |
| I36 | P0 | Locale-aware management formatting | Settings, statistics, request logs, and proxy-key metadata render numbers/timestamps in the selected locale |
| I37 | P0 | Analytics REST polling path | `/observe?tab=analytics` polls `GET /api/stats/usage-snapshot?preset=...`, treats each accepted snapshot as a full replacement, and uses endpoint model stats REST for endpoint drilldown |

## J. Non-Functional Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| J01 | P1 | 10+ concurrent proxy requests | No crashes; logs complete |
| J02 | P1 | Added proxy latency quick check | Within acceptable envelope |
| J03 | P1 | No-auth local operation | Expected unrestricted local usage |
| J04 | P1 | API reference sanity | Core routes present in the markdown API reference |
| J05 | P1 | PostgreSQL hygiene | DB schema and migration state are valid for smoke environment |

## Locale Smoke Addendum

Run these checks in both `en` and `zh-CN` after the frontend is up:

1. Public auth route check
   - Visit `/auth/login`
   - Switch the language control
   - Confirm shell/auth copy changes immediately and survives a hard refresh

2. Protected route check
   - Visit one protected route such as `/observe` or `/system/settings`
   - Confirm the selected locale persists after a refresh
   - Confirm `document.documentElement.lang` matches the selected locale

3. Formatting check
   - Verify at least one timestamp, one number, and one currency value on a protected route change formatting between `en` and `zh-CN`

4. Management route spot-check
   - Verify `/system/settings`, `/models`, `/route/endpoints`, `/route/ban-policies`, `/control/proxy-keys`, and `/route/pricing` for obvious mixed-language regressions

## K. Header Blocklist

### K.1 CRUD API

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| K01 | P0 | List rules returns seeded system defaults | `200`, includes system headers |
| K02 | P0 | Create user rule (exact match) | `201`, rule stored with `is_system=false` |
| K03 | P0 | Create user rule (prefix match ending with `-`) | `201`, rule stored |
| K04 | P0 | Create duplicate rule | `409` |
| K05 | P0 | Get single rule by ID | `200`, returns full rule object |
| K06 | P0 | Get non-existent rule ID | `404` |
| K07 | P0 | Update user rule | `200`, changes persist |
| K08 | P0 | Update system rule `enabled` only | `200`, change persists |
| K09 | P0 | Update system rule immutable fields | `400` |
| K10 | P0 | Delete user rule | `200`, returns `{ "deleted": true }` |
| K11 | P0 | Delete system rule | `404` because the delete route only targets profile-owned user rules |

### K.2 Validation

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| K12 | P0 | Create prefix rule without trailing `-` | `422` validation error |
| K13 | P0 | Create rule with invalid header token chars | `422` validation error |
| K14 | P0 | Pattern normalized to lowercase | Mixed-case input stored as lowercase |
| K15 | P0 | Pattern whitespace trimmed | Leading/trailing whitespace removed |
| K16 | P1 | Invalid `match_type` value | `422` validation error |

### K.3 Proxy Runtime Integration

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| K17 | P0 | Proxy request with `cf-ray` header | Header blocked from upstream |
| K18 | P0 | Proxy request with `x-forwarded-for` | Header blocked |
| K19 | P0 | Proxy request with tracing header | Header blocked |
| K20 | P0 | Proxy request with allowed header | Passes through to upstream |
| K21 | P0 | `custom_headers` cannot re-add blocked header names | Blocked header still absent |
| K22 | P0 | API-family auth headers remain correct | Auth headers present and correct |
| K23 | P0 | Runtime provider request applies blocklist rules | Blocked headers excluded |
| K24 | P1 | Disable all rules | Metadata headers flow through |

### K.4 Frontend UI (Settings Page)

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| K30 | P0 | Header blocklist card loads in Settings | Card visible |
| K31 | P0 | System rules collapsible section | Expands to show system rules |
| K32 | P0 | User rules collapsible section | Expands to show user rules |
| K33 | P0 | Toggle system rule enabled state via UI | Switch updates, state persists |
| K34 | P0 | Add user rule via dialog | Rule appears in user rules table |
| K35 | P0 | Edit user rule via dialog | Changes reflected |
| K36 | P0 | Delete user rule via dialog | Rule removed from table |

### K.6 User-Agent Client Rules

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| K37 | P0 | List user-agent client rules | `200`, includes seeded system rules plus effective-profile rules |
| K38 | P0 | Get single user-agent client rule by ID | `200` for in-scope rule; `404` for missing or out-of-scope rule |
| K39 | P0 | Create user-agent client rule | `201`, regex pattern persists on a profile-scoped rule |
| K40 | P0 | Update profile-scoped user-agent client rule | `200`, mutable fields persist |
| K41 | P0 | Update system user-agent client rule immutable fields | `400`; only `enabled` may change on system rules |
| K42 | P0 | Delete profile-scoped user-agent client rule | `200`, returns `{ "deleted": true }` |
| K43 | P0 | Delete system user-agent client rule | `404`, because the delete route only targets profile-owned user rules |
| K44 | P0 | System rule edit/delete buttons disabled | Icons disabled for system rules |
| K45 | P1 | Add user-agent client rule validation: invalid regex | Backend validation error is surfaced without saving the rule |
| K46 | P1 | Add user-agent client rule validation: empty name or pattern | Submit is blocked by validation toast; the dialog does not save an empty rule |

## L. Token Costing and Spending Reports

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| L01 | P0 | Create pricing template | `201`, template persisted with expected version and explicit token prices |
| L02 | P0 | Update pricing template pricing fields | `200`, template `version` increments |
| L03 | P0 | Update Terminal Target pricing template assignment | `200`, `pricing_template_id` updates |
| L04 | P0 | GET `/api/settings/costing` | Returns defaults |
| L05 | P0 | PUT `/api/settings/costing` with FX mappings | `200`, settings persist |
| L06 | P0 | PUT `/api/settings/costing` rejects `fx_rate <= 0` | `400` |
| L07 | P0 | PUT `/api/settings/costing` rejects duplicate (model, endpoint) | `400` |
| L08 | P0 | Proxy successful request with priced Terminal Target | `request_log` has cost fields populated |
| L09 | P0 | Proxy failed request | `billable_flag=false`, all `cost_micros=0` |
| L10 | P0 | Proxy successful request with unpriced Terminal Target | `priced_flag=false`, `unpriced_reason` set |
| L11 | P0 | GET `/api/stats/spending` summary | Returns correct totals |
| L12 | P0 | GET `/api/stats/spending` `group_by=model` | Returns grouped rows |
| L13 | P0 | GET `/api/stats/spending` excludes failed requests | Failed requests not in totals |
| L18 | P1 | FX conversion with custom rate | Correct converted cost |
| L19 | P1 | Model rename updates FX mapping keys | FX mappings remain valid |
| L20 | P1 | Spending report pagination | `limit`/`offset` respected |
| L21 | P1 | Spending report `top_n` | Returns correct top spenders |
| L22 | P1 | Older request logs without pricing data | Unpriced rows aggregate under `UNKNOWN` bucket when reason is null/empty |
| L23 | P1 | Special token pricing uses explicit values | Templates use explicit token prices only |

| L28 | P1 | Pricing template usage endpoint | `/connections` compatibility response matches current Terminal Target assignments |
| L27 | P0 | Delete pricing template not in use | `200`/success response and template removed from list |
| L26 | P0 | Delete pricing template in-use conflict | `409` conflict returns compatibility connection dependencies and UI shows Terminal Targets |
| L25 | P0 | Pricing templates CRUD UI in Settings | List/create/edit actions persist and render correctly |
| L24 | P0 | Clear Terminal Target pricing template assignment | `200`, `pricing_template_id=null` accepted |
| L29 | P0 | GET `/api/settings/timezone` | Returns current preference |
| L30 | P0 | PUT `/api/settings/timezone` | `200`, preference persists |
| L31 | P0 | GET `/api/settings/audit` | Returns stable `openai`, `anthropic`, and `gemini` rows for Default profile id `1` |
| L32 | P0 | PUT `/api/settings/audit` | Full-family replacement persists Default-profile audit and body-capture policy |

## M. Frozen Default Profile Scope

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| M01 | P0 | Default profile management routes are removed | management profile CRUD requests are not mounted |
| M03 | P0 | Management API profile resolution (`X-Profile-Id` absent vs present) | Profile-scoped `/api/*` succeeds without the header; any header value is ignored and operations use Default profile id `1` |
| M11 | P0 | Runtime request with `X-Profile-Id` override header | Runtime ignores override and uses frozen Default profile id `1` context only |
| M12 | P0 | Duplicate `model_id` in Default profile id `1` | Duplicate create returns `409`; there is no profile switch path to bypass uniqueness |
| M13 | P0 | Access target missing from Default profile id `1` | Target resolution fails (`404`) |
| M14 | P0 | Request-log attribution and stats scope | Every row has immutable `profile_id`; stats/list/delete operate on Default profile id `1` |
| M15 | P0 | Audit attribution and scope | Every row has immutable `profile_id`; list/detail/delete operate on Default profile id `1` |
| M19 | P0 | Costing/settings scope | Updating currency/FX persists for Default profile id `1` |
| M20 | P0 | Header blocklist scope merge | Runtime uses global system rules + Default profile user rules; management CRUD/list views stay on Default profile id `1` |
| M21 | P1 | Failover Ban Policy scope | Retry-window and ban state are isolated to Default profile id `1` rows |
| N01 | P0 | Auth status check | Returns correct auth/authn state |
| N02 | P0 | Public bootstrap | Initializes session for login |
| N03 | P0 | Login with valid credentials | `200`, session cookies set |
| N04 | P0 | Logout | `200`, cookies cleared |
| N05 | P0 | Session refresh | `200`, new session issued |
| N06 | P0 | Get session | Returns current session info |

## 8. Recommended Execution Order

1. A (startup/health).
2. M01-M03 (removed profile routes and frozen Default profile scope).
3. B (CRUD and validation).
4. M11-M13 (runtime profile isolation checks).
5. C and D (proxy behavior and runtime URL joining).
6. E and F plus M14-M15 (stats/audit with attribution scope).
7. K.1-K.3 plus M20 (header blocklist, including profile scope).
8. L plus M19 (token costing and spending reports with profile isolation).
9. G, H, K.4, and M16-M18 in isolated destructive lane.
10. I and K.5 (frontend full-stack smoke, including Default profile behavior).
12. J and M21 (non-functional quick pass + failover memory scope).

---

## 9. Acceptance Criteria

- All `P0` tests pass.
- No proxy contract regressions in routing/failover/logging/audit.
- Any `P1` failure is triaged with reproducible payloads and logs.

---

## 10. Test Reporting Template

Use this minimal template for each run:

```text
Run ID:
Date:
Commit:
Environment:

P0 Pass/Fail:
P1 Pass/Fail:

Failures:
- [ID] Summary
  - Observed:
  - Expected:
  - Repro:
  - Evidence (API response / DB row / UI screenshot):

Notes:
```

---

## 11. Notes and Assumptions

- Time cutoff tests use server UTC (`cutoff` semantics).
- `delete_all=true` mode deletes all records without a time cutoff.
- Destructive data deletion tests must run against an isolated smoke DB.
- Streaming token extraction tests should include both usage-present and usage-missing streams.
- Failover tests must verify per-attempt logging in both `request_logs` and `audit_logs` (when enabled).
- Header blocklist rules are resolved from DB per request (no in-memory cache).
- Header blocklist matching is case-insensitive.
- System blocklist rules are seeded on first boot.
- Prefix rules must end with `-` (e.g. `cf-`, `x-cf-`).
- Profile-scoped management APIs are pinned to Default profile id `1`; global management routes remain outside profile scoping; proxy runtime uses frozen Default profile id `1` scope.
