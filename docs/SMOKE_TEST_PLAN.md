# Smoke Test Plan: Prism (Comprehensive)

## 1. Scope and Goals

This smoke test plan validates all documented workflows and core function paths across:

- Backend API contract
- Proxy behavior (selector-driven target selection, load balancing, failover, streaming)
- Health detection
- Statistics and token extraction
- Audit logging and redaction
- Header blocklist and sanitization
- Configuration export/import
- Batch data deletion semantics
- Frontend management flows
- Token costing and spending reports
- CLIProxyAPI sidecar registration, inventory sync, and direct auth-file mutation

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

- Go 1.26.2 toolchain, Node `24+`, pnpm `10.30.1`, Docker, and Docker Compose.
- When using the checked-in launcher, backend available at `http://localhost:18000` from the checked-in `config.json` and frontend at `http://localhost:5173`.
- `backend/docker-compose.yml` binds PostgreSQL on host port `15432` for local orchestration.
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

1. Vendors exist: OpenAI, Anthropic, Gemini, plus any extra publisher metadata rows needed for cross-vendor catalog cases.
2. Profiles exist: A, B, C; start with A as active runtime profile.
3. Profile-scoped Endpoints (credentials):
   - in profile A: one OpenAI endpoint
   - in profile B: one Anthropic endpoint
   - in profile C: one Gemini endpoint
4. Profile-scoped models:
   - in profile A: one OpenAI-family model with 2+ reachable standalone connection targets
   - in profile B: one Anthropic-family model
   - in profile C: one Gemini-family model
5. Unified access targets:
   - same-`api_family` model-target chains plus standalone connection targets using ordered `access_targets` (`target_type`, `target_model_id`, `connection_ref`, `position`, `is_enabled`) in the same profile, even when vendor metadata differs
6. Standalone connection diversity per profile:
   - active + inactive
   - different model target positions
   - one connection with `custom_headers`
   - one connection assigned a `pricing_template_id`
7. Audit toggles initially disabled, then enabled per-case.
8. At least one duplicated `model_id` and endpoint `name` across A/B to validate scoped uniqueness.
9. Optional sidecar fixture or safe live sidecar endpoint for `/api/sidecars/*` coverage; management password must never be recorded in run notes.

---

## 6. API Surface Coverage Matrix

| Endpoint | Coverage IDs |
|---|---|
| `GET /health` | A04 |
| `GET /api/profiles` | M01, M03, M08, M09 |
| `GET /api/profiles/bootstrap` | M01A |
| `GET /api/profiles/active` | M02, M11 |
| `POST /api/profiles` | M04, M10 |
| `PATCH /api/profiles/{id}` | M05 |
| `POST /api/profiles/{id}/activate` | M06-M07 |
| `DELETE /api/profiles/{id}` | M08-M09 |
| `GET /api/vendors` | B01 |
| `GET /api/vendors/{id}` | B03 |
| `PATCH /api/vendors/{id}` | B02 |
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
| `POST /api/connections` | B16-B17, L01-L02, M03 |
| `PUT /api/connections/{id}` | B19-B20, M03 |
| `GET /api/connections/{id}/references` | B21B, M03 |
| `PUT /api/connections/{id}/pricing-template` | L03, L24, M03 |
| `DELETE /api/connections/{id}` | B21-B21A, M03 |
| `GET/POST/PATCH/DELETE /api/models/{model_config_id}/targets` | B18A, B20A, M03 |
| `POST /api/connections/{id}/health-check` | D01-D06 |
| `POST /v1/chat/completions` | C01, C03, C04, C06-C14, E08, E10, L08-L10, M11-M13, M21 |
| `POST /v1/responses` | C01, C03, C04, C06-C14, E08, E10, L08-L10, M11-M13, M21 |
| `POST /v1/messages` | C02, C04, E08, E10, L08-L10, M11-M13, M21 |
| `GET /api/stats/requests` | E01-E04, M14 |
| `GET /api/stats/endpoints/{endpoint_id}/models` | E16, M14 |
| `GET /api/stats/summary` | E05-E06, M14 |
| `GET /api/stats/dashboard` | E14, M14 |
| `POST /api/stats/models/metrics` | E13, M14 |
| `GET /api/stats/connection-success-rates` | E07 |
| `GET /api/stats/throughput` | E15, M14 |
| `GET /api/stats/spending` | L11-L13, L19-L20, M19 |
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
| `GET /api/config/profile/export` | H01-H04, L14, M16 |
| `POST /api/config/profile/export/with-secrets` | H01A |
| `POST /api/config/profile/import/preview` | H05-H07, L15-L16, M17-M18 |
| `POST /api/config/profile/import` | H05-H07, L15-L16, M17-M18 |
| `GET /api/config/vendors/export` | H10 |
| `POST /api/config/vendors/import/preview` | H11 |
| `POST /api/config/vendors/import` | H12 |
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
| `GET /api/sidecars` | O01, O11 |
| `POST /api/sidecars` | O02 |
| `GET /api/sidecars/{sidecar_id}` | O03 |
| `PATCH /api/sidecars/{sidecar_id}` | O04 |
| `DELETE /api/sidecars/{sidecar_id}` | O05 |
| `POST /api/sidecars/{sidecar_id}/test-connection` | O06 |
| `POST /api/sidecars/{sidecar_id}/sync` | O07 |
| `GET /api/sidecars/{sidecar_id}/auth-files` | O08 |
| `GET /api/sidecars/{sidecar_id}/providers` | O09 |
| `GET /api/sidecars/{sidecar_id}/provider-snapshots` | O09 |
| `GET /api/sidecars/{sidecar_id}/sync-status` | O10 |
| `PATCH /api/sidecars/{sidecar_id}/auth-files/{auth_id}/status` | O13 |
| `PATCH /api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields` | O14 |
| `WS /api/realtime/ws` | I26, I30, I31, I37 |

---

## 7. Detailed Test Cases

## A. Startup and Deployment

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| A01 | P0 | Start backend in `headless` mode | Backend process starts, API reachable |
| A02 | P0 | Start in `full` mode | Backend + frontend reachable |
| A03 | P0 | First boot with empty DB | DB created, vendors seeded |
| A04 | P0 | `GET /health` | `200`, JSON contains `status=ok` and a non-empty `version` string |
| A05 | P1 | Backend-served API documentation surface | Not exposed by the backend |
| A06 | P1 | CORS preflight | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; explicit backend base URLs remain available for standalone frontend workflows |

## B. Configuration CRUD and Validation

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| B01 | P0 | List vendors | Includes `audit_enabled`, `audit_capture_bodies` |
| B02 | P0 | Patch vendor audit fields | Fields persist; omitted field unchanged |
| B03 | P1 | Get/patch unknown vendor | `404` |
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
| B16 | P0 | Create standalone connection | `201`, connection stored with explicit `api_family` |
| B16A | P0 | Attach connection target to a model | New access target appends after existing model targets |
| B17 | P0 | Attach wrong-family connection target | `400` |
| B18 | P1 | List standalone connections | `200`, returns profile-scoped connections with endpoint and pricing metadata |
| B18A | P0 | List model access targets | `200`, returns ordered model and connection targets |
| B19 | P0 | Update connection with `custom_headers=null/{}` | Headers removed |
| B20 | P1 | Update connection omitting `custom_headers` | Existing headers retained |
| B20A | P0 | Reorder model access target | `200`, returns reordered targets; no-op stays stable; wrong model/profile combo returns `404`; out-of-range `to_index` returns `422` |
| B20B | P0 | Connection payload containing access-target ordering fields | `422` validation error |
| B21 | P1 | Delete unreferenced connection | `200`, connection removed |
| B21A | P0 | Delete referenced connection | `409` until referencing model targets are removed |
| B21B | P0 | Read connection references | `200`, returns model target rows that reference the connection |

## C. Runtime Routing, Unified Access Targets, Headers, and Ban Policy

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| C01 | P0 | OpenAI-compatible proxy call | Upstream response proxied as-is |
| C02 | P0 | Anthropic non-stream proxy call | Upstream response proxied as-is |
| C03 | P1 | Gemini route compatibility | Correct routing and auth behavior |
| C04 | P0 | Unified access-target model request | Routed through ordered model and connection targets; requested model and final target model are logged separately |
| C05 | P0 | Unknown/disabled model | `404` |
| C06 | P0 | Fill-first routing order | Lowest-position eligible target wins when Ban Policy state is otherwise equal |
| C07 | P0 | Ban Policy retry-window path | First transient failure increments retry counters without blocking; threshold hit opens a retry window with backoff and jitter; expired windows allow the connection to be selected again in normal order |
| C08 | P0 | Failover on `403/429/500/502/503/529` | Next connection attempted; `403` follows the configured Ban Policy status-code rules |
| C09 | P0 | Failover on connection error/timeout | Next connection attempted; failure kind classified (`connect_error` / `timeout`) for Ban Policy state |
| C10 | P0 | Non-failover client error while Ban Policy state exists | Request returns upstream error; existing retry-window state is not force-cleared |
| C11 | P0 | All failover attempts fail | `502` with last error detail |
| C12 | P0 | No active connection targets | `503` |
| C13 | P1 | Header merge order with custom override | Custom headers win over api-family/client headers |
| C14 | P1 | Connection `custom_headers` override | Effective headers follow override |
## D. Connection Health Check and URL Failsafe

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| D01 | P0 | Health check with 2xx | `healthy` |
| D02 | P0 | Health check with 429 | `healthy` |
| D03 | P0 | Health check with 401/403 | `unhealthy`, auth failure detail |
| D04 | P0 | Health check with other non-2xx JSON error | `detail` includes extracted upstream message |
| D05 | P0 | Health check connect error/timeout | `unhealthy` |
| D06 | P1 | Health state persistence | `health_status`, `health_detail`, `last_health_check` updated |
| D07 | P1 | Runtime `/vN/vN` path failsafe | URL auto-correct behavior verified |

## E. Statistics and Token Extraction

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| E01 | P0 | Successful request logging | `request_logs` row exists with required fields |
| E02 | P0 | Failover attempt logging | Both failed and successful attempts logged |
| E03 | P0 | Request log filters (`ingress_request_id`, `model_id`, `status_family`, `endpoint_id`, `from_time`) | Correct subsets returned |
| E04 | P0 | Pagination (`limit`, `offset`, `total`) | Consistent counts and windows |
| E05 | P0 | Summary without `from_time` | Uses all historical data |
| E06 | P1 | Summary grouping (`model/api_family/endpoint`) | Groups and aggregates correct |
| E07 | P1 | Connection success-rate API | Values match request logs |
| E08 | P0 | Non-stream token extraction | Token fields match vendor format rules |
| E09 | P1 | Unsupported/malformed usage fallback | Token fields null |
| E10 | P0 | Stream token extraction | Token fields populated |
| E11 | P1 | Streaming without usage fields | Token fields null |
| E12 | P0 | Model health fields in `/api/models` | Weighted health and request totals correct |
| E13 | P0 | Model metrics batch API | Returns metrics for multiple models |
| E14 | P0 | Dashboard aggregate stats API | `GET /api/stats/dashboard` returns the canonical overview snapshot with `metric_snapshot`, `api_family_rows`, `recent_requests`, top spending models, strategy-family counts, and backend-computed `routing_health_map` |
| E15 | P1 | Throughput API | Returns aggregate RPM metrics plus time buckets for the selected scope |
| E16 | P0 | Endpoint model statistics API | Returns per-model counts, success rates, TTFT percentiles, token totals, and cost for the selected endpoint scope |

## F. Audit Logging

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| F01 | P0 | Audit disabled vendor | No audit row created |
| F02 | P0 | Audit enabled + body capture enabled | Request/response metadata and bodies recorded |
| F03 | P0 | Body capture disabled | Bodies stored as null |
| F04 | P0 | Streaming audited request with body capture | Raw captured SSE response body stored when bytes are captured; metadata-only or empty captures keep `response_body` null |
| F05 | P0 | Failover with audit enabled | One audit row per upstream attempt |
| F06 | P0 | Redaction exact headers | Values redacted before storage |
| F07 | P1 | Redaction by name pattern | Values redacted |
| F08 | P1 | Non-sensitive headers | Preserved |
| F09 | P0 | 64KB truncation | `[TRUNCATED]` appended |
| F10 | P0 | Audit list API | Requires bounded `from`/`to` window, caps windows at 7 days, returns `request_body_preview` max 200 chars, and orders by `(created_at DESC, id DESC)` |
| F11 | P0 | Audit detail API | Full row returned; unknown id is `404` |
| F12 | P0 | Audit filters/pagination | Correct subsets, keyset `next_cursor`, `has_more`, and no audit `offset` or `total` response contract |
| F13 | P0 | Audit delete job validation | `202` creates an async job with `job_id`, `state`, `status_url`, and `Location`; invalid body, scope, reason, or idempotency returns `400` |
| F14 | P1 | Audit non-interference on write failure | Proxy response unaffected |
| F15 | P0 | Orphan audit row visibility | Audit rows with null `request_log_id` remain visible in audit APIs and keep request-time provenance |

## G. Global Log Retention and Weak Link Semantics

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| G01 | P0 | Get global log-retention settings | `200`, returns day policies for `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`; no selected-profile scope |
| G02 | P0 | Update global log-retention settings | `200`, persists nullable day policies for all retention tables; invalid day values return `422` |
| G03 | P0 | Create retention job with missing table | `400` |
| G04 | P0 | Request-log retention job with stored policy | `202`, returns `{ job_id, state, status_url, scope }` and a `Location` header; job scope is global |
| G05 | P0 | Request-log retention preserves audit provenance | Audit rows may outlive request logs and retain weak `request_log_id`, `request_log_created_at`, and `ingress_request_id` metadata |
| G06 | P0 | Request-log retention with explicit cutoff | `202`, computes partition cleanup against the supplied cutoff and exposes job progress through management job APIs |
| G07 | P0 | Retention job rejects invalid table or scope | `400` |
| G08 | P0 | Retention job rejects conflicting cutoff and delete-all modes | `400` |
| G09 | P0 | Request-log delete-all retention mode | Retention flow removes and recreates or reboots partitions for `request_logs`; it does not use a parent-root table delete |
| G10 | P0 | Audit-log retention with explicit cutoff | `202`, returns `{ job_id, state, status_url, scope }` and a `Location` header; cleanup is global, not selected-profile scoped |
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

## H. Config Export and Import

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| H01 | P0 | Export schema and metadata | `version=2`, `bundle_kind=profile_config`, `exported_at`, profile-targeted payload with `vendor_refs`, `profile_settings`, encrypted `secret_payload`, `loadbalance_strategies`, top-level standalone `connections`, ordered model `access_targets`, nullable model `vendor_key`, required `api_family`, and strategy-name model references |
| H01A | P0 | Export includes endpoint position | Endpoints are ordered by `position` and each endpoint includes `position` |
| H02 | P0 | Export excludes IDs/timestamps/health/logs | Exclusion contract respected |
| H03 | P0 | Profile export excludes global vendor audit policy | Profile bundle uses `vendor_refs` only for actually referenced vendor rows; vendor audit metadata remains in the vendor-catalog bundle/global vendor rows |
| H03A | P0 | Profile export includes vendorless model | Vendorless models export `vendor_key: null` and do not synthesize `vendor_refs` |
| H04 | P0 | Export includes connection `custom_headers` | Fields preserved |
| H05 | P0 | Valid import replace (target profile only) | Only effective profile config replaced; other profiles unchanged |
| H05D | P0 | Import vendorless model | Imported model persists with `vendor_id = null` while keeping required `api_family` |
| H05E | P0 | Import preview with selected-profile header | Preview requires `X-Profile-Id`, reports readiness or blocking errors, and does not mutate profile state |
| H05A | P0 | Import with endpoint position hints | Imported endpoint order follows provided `position` values and is normalized contiguously |
| H05B | P0 | Import payload without endpoint position | Imported endpoint order follows file order and remains valid |
| H05C | P0 | Import with duplicate/gapped access-target positions | Imported model targets are normalized to contiguous `0..N-1` while preserving relative order by imported position then payload order |
| H06 | P0 | Import failure rollback | Prior config remains intact |
| H07 | P0 | Validation matrix | Correct `400` errors |
| H08 | P1 | Settings UI export filename | `prism-profile-config-v2-YYYY-MM-DD.json` |
| H09 | P1 | Settings UI import error paths | Parse/backend errors surfaced in toast |
| H10 | P0 | Vendor catalog export schema and metadata | Vendor-catalog bundle metadata, audit flags, descriptions, and `icon_key` included |
| H11 | P0 | Vendor catalog preview validation | Preview is global/no-header, returns create/update counts, and rejects duplicate keys/names or readonly overwrite attempts before mutation |
| H12 | P0 | Vendor catalog import | Editable vendor metadata upserts correctly while readonly system vendors remain protected |

## I. Frontend Workflow Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| I01 | P0 | Sidebar navigation | All routes load |
| I02 | P0 | Dashboard + Models success rate badges | Correct color thresholds and `N/A` |
| I03 | P0 | Model detail connection success badge + tooltip | Correct counts, rates, health detail |
| I04 | P0 | Connection health actions | Toast/banner reflects result |
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
| I12C | P0 | Standalone connection dialog ordering UX | Dialog exposes no numeric priority field and explains that model attachment controls target order |
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
| I26 | P0 | Dashboard websocket data-push recent activity | New request appears in Recent Activity within 1s of a proxy request |
| I27 | P0 | Request logs manual refresh and exact-request mode | Refresh reloads the current server slice; `request_id` mode fetches only the targeted request |
| I28 | P0 | Loadbalance events tab REST refresh | Refresh or page revisit loads the latest failover and Ban Policy events for the model |
| I29 | P0 | Requests audit drawer lazy fetch and retry | Opening the audit tab triggers linked-audit lookup, skips fetches when audit was disabled for that request, and retries empty/transient failures up to five times |
| I30 | P0 | Dashboard reconnect reconciliation | Dashboard refetches ground truth after websocket reconnect and resumes push updates |
| I31 | P1 | Dashboard websocket payload completeness | `dashboard.update` refreshes summary, api_family, spending, throughput, and routing data without a second signal |
| I32 | P1 | Recent activity insertion animations | New dashboard websocket-driven rows animate on insert, with reduced-motion fallback showing static highlight only |
| I33 | P1 | Request-log scroll preservation | Virtualized browsing preserves scroll position while filters, page changes, or detail selection update the view |
| I34 | P1 | Request-log retained-filter state | Changing one retained filter preserves unrelated URL-backed filter state, resets pagination, and refreshes the server-backed slice without client-side refinement gating |
| I35 | P0 | Frontend locale switch (public + protected) | Switching between `en` and `zh-CN` updates shell and page copy, persists across refresh, and updates `document.documentElement.lang` |
| I36 | P0 | Locale-aware management formatting | Settings, statistics, request logs, and proxy-key metadata render numbers/timestamps in the selected locale |
| I37 | P0 | Analytics websocket no-REST data path | `/dashboard?tab=analytics` subscribes with `{type:"subscribe", channel:"analytics", profile_id, preset}`, receives a full `analytics.snapshot` with `endpoint_model_statistics_by_endpoint_id`, manual refresh sends websocket `refresh`, and the browser makes no `/api/stats/*` requests for analytics rendering |

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
   - Visit `/login`
   - Switch the language control
   - Confirm shell/auth copy changes immediately and survives a hard refresh

2. Protected route check
   - Visit one protected route such as `/dashboard` or `/settings`
   - Confirm the selected locale persists after a refresh
   - Confirm `document.documentElement.lang` matches the selected locale

3. Formatting check
   - Verify at least one timestamp, one number, and one currency value on a protected route change formatting between `en` and `zh-CN`

4. Management route spot-check
   - Verify `/settings`, `/models`, `/endpoints`, `/loadbalance-strategies`, `/proxy-api-keys`, and `/pricing-templates` for obvious mixed-language regressions

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
| K22 | P0 | Vendor auth headers remain correct | Auth headers present and correct |
| K23 | P0 | Health-check also applies blocklist rules | Blocked headers excluded |
| K24 | P1 | Disable all rules | Metadata headers flow through |

### K.4 Config Export/Import Integration

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| K25 | P0 | Config export includes `header_blocklist_rules` | Rules present in export JSON |
| K26 | P0 | Config import with rules omitted | Preserves existing rules |
| K27 | P0 | Config import with rules provided | Replaces user rules, applies system states |
| K28 | P0 | Config import with unknown system pattern | `400` rejection |
| K29 | P1 | Config import roundtrip preserves rule state | Identical rules after roundtrip |

### K.5 Frontend UI (Settings Page)

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
| L03 | P0 | Update connection pricing template assignment | `200`, `pricing_template_id` updates |
| L04 | P0 | GET `/api/settings/costing` | Returns defaults |
| L05 | P0 | PUT `/api/settings/costing` with FX mappings | `200`, settings persist |
| L06 | P0 | PUT `/api/settings/costing` rejects `fx_rate <= 0` | `400` |
| L07 | P0 | PUT `/api/settings/costing` rejects duplicate (model, endpoint) | `400` |
| L08 | P0 | Proxy successful request with priced connection | `request_log` has cost fields populated |
| L09 | P0 | Proxy failed request | `billable_flag=false`, all `cost_micros=0` |
| L10 | P0 | Proxy successful request with unpriced connection | `priced_flag=false`, `unpriced_reason` set |
| L11 | P0 | GET `/api/stats/spending` summary | Returns correct totals |
| L12 | P0 | GET `/api/stats/spending` `group_by=model` | Returns grouped rows |
| L13 | P0 | GET `/api/stats/spending` excludes failed requests | Failed requests not in totals |
| L14 | P0 | Config export current format | Safe GET export returns `version: 2`, `bundle_kind: profile_config`, redacted endpoint secrets, empty secret entries for null refs, top-level standalone connections, ordered model access targets, pricing templates, and profile-scoped `profile_settings` |
| L15 | P0 | Config export with secrets | Dangerous POST export returns the full secret-bearing bundle and requires the dangerous-confirm header |
| L16 | P0 | Config import current format | Preview and apply restore vendors, Ban Policy strategies, access targets, templates, connections, vendorless models, and settings into the target profile only |
| L17 | P0 | Config import unsupported version rejection | Unsupported config versions are rejected |
| L18 | P1 | FX conversion with custom rate | Correct converted cost |
| L19 | P1 | Model rename updates FX mapping keys | FX mappings remain valid |
| L20 | P1 | Spending report pagination | `limit`/`offset` respected |
| L21 | P1 | Spending report `top_n` | Returns correct top spenders |
| L22 | P1 | Older request logs without pricing data | Unpriced rows aggregate under `UNKNOWN` bucket when reason is null/empty |
| L23 | P1 | Special token pricing uses explicit values | Templates use explicit token prices only |

| L28 | P1 | Pricing template usage endpoint | `/connections` response matches current assignments |
| L27 | P0 | Delete pricing template not in use | `200`/success response and template removed from list |
| L26 | P0 | Delete pricing template in-use conflict | `409` conflict returns connection dependencies and UI shows them |
| L25 | P0 | Pricing templates CRUD UI in Settings | List/create/edit actions persist and render correctly |
| L24 | P0 | Clear connection pricing template assignment | `200`, `pricing_template_id=null` accepted |
| L29 | P0 | GET `/api/settings/timezone` | Returns current preference |
| L30 | P0 | PUT `/api/settings/timezone` | `200`, preference persists |
## M. Profile Isolation and Context Semantics

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| M01A | P0 | Bootstrap profiles for the shell | `200`, returns `profiles`, `active_profile`, and `profile_limits.max_profiles` in one payload |
| M01 | P0 | List profiles | `200`, excludes soft-deleted profiles from normal listing |
| M02 | P0 | Get active profile | Exactly one active profile returned |
| M03 | P0 | Management API profile resolution (`X-Profile-Id` absent vs present) | Profile-scoped `/api/*` rejects missing header (`400`); valid header scopes requests to selected profile |
| M04 | P0 | Create profile under capacity | `201`, profile created as inactive by default |
| M05 | P0 | Update profile metadata | `200`, name/description persisted |
| M06 | P0 | Activate profile with correct CAS payload | Activation succeeds atomically; active profile/version updated |
| M07 | P0 | Activate profile with stale CAS payload | `409` conflict; previous active profile unchanged |
| M08 | P0 | Delete inactive profile | Soft-delete succeeds; profile omitted from default listings |
| M09 | P0 | Delete active profile | Rejected (`400` or `409`), active profile remains unchanged |
| M09A | P0 | Update/Delete non-editable profile | Rejected (`400`), profile remains unchanged |
| M10 | P0 | Create 11th non-deleted profile | `409` with actionable delete-before-create error |
| M11 | P0 | Runtime request with `X-Profile-Id` override header | Runtime ignores override and uses active profile context only |
| M12 | P0 | Same `model_id` exists in A/B with different access targets | Routing uses active profile mappings only; no cross-profile resolution |
| M13 | P0 | Access target exists only in another profile | Target resolution fails (`404`) under current active profile |
| M14 | P0 | Request-log attribution and stats scope | Every row has immutable `profile_id`; stats/list/delete operate on effective profile only |
| M15 | P0 | Audit attribution and scope | Every row has immutable `profile_id`; list/detail/delete are profile-scoped |
| M16 | P0 | Config export from selected profile | Output is profile-targeted `version=2`, `bundle_kind=profile_config`, top-level standalone connections, ordered model access targets, and safe redacted export details, while the dangerous export path is available separately through `POST /api/config/profile/export/with-secrets` |
| M17 | P0 | Config import preview/apply binding | Apply only succeeds after preview returns a token and the same token is sent in `X-Prism-Preview-Token` |
| M18 | P0 | Config import replace into profile A | Replaces A only; profile B/C scoped data remains unchanged |
| M19 | P0 | Config import unsupported version rejection | Unsupported config versions are rejected |
| M19 | P0 | Costing/settings isolation | Updating currency/FX in A does not mutate B/C settings or spending results |
| M20 | P0 | Header blocklist scope merge | Runtime uses global system rules + active profile user rules; management CRUD/list views stay on effective-profile scope |
| M21 | P1 | Failover Ban Policy isolation by profile | Retry-window and ban state in profile A does not affect profile B |
| N01 | P0 | Auth status check | Returns correct auth/authn state |
| N02 | P0 | Public bootstrap | Initializes session for login |
| N03 | P0 | Login with valid credentials | `200`, session cookies set |
| N04 | P0 | Logout | `200`, cookies cleared |
| N05 | P0 | Session refresh | `200`, new session issued |
| N06 | P0 | Get session | Returns current session info |

## O. CLIProxyAPI Sidecars

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| O01 | P0 | List sidecars | `200`, returns global `items[]` with masked credential state only |
| O02 | P0 | Create sidecar | `201`, persists canonical URL, network policy flags, sync intervals, and no raw password in response/body logs |
| O03 | P0 | Get sidecar | `200` for existing sidecar, `404` for unknown sidecar |
| O04 | P0 | Patch sidecar metadata or management password | `200`, updates metadata; password rotation resets management auth state without returning the raw value |
| O05 | P0 | Delete sidecar | `204`, soft-deletes registration and removes it from list results |
| O06 | P0 | Test sidecar connection | Success returns `state=succeeded`, management auth state, and status code; invalid management auth records pause state |
| O07 | P0 | Manual sync | Success updates provider inventory and sync status; disabled sidecar returns `409`, invalid management auth returns `424` |
| O08 | P0 | Auth inventory read | `auth-files` returns live, normalized, redacted auth observations from CLIProxyAPI |
| O09 | P0 | Provider inventory read | Provider snapshots are normalized for supported provider keys and do not expose raw secrets |
| O10 | P1 | Sync-status read | Status includes `management_auth_state`, `stale`, `due`, `paused`, sync timestamps, and auth-failure pause metadata without profile scope |
| O11 | P0 | `/sidecars` UI load | Route loads outside selected-profile scope, shows sidecar health, can select a detail row, and renders live auth-files plus provider inventory |
| O13 | P1 | Auth status mutation | Status patch succeeds only through Prism backend after live auth-file preflight |
| O14 | P1 | Auth priority mutation | Field patch accepts only priority and rejects any other auth-file mutation field |
| O15 | P1 | Sidecar worker priority | `sidecar_snapshot_sync` rejects elevated priority overrides |

---

## 8. Recommended Execution Order

1. A (startup/health).
2. M01-M10 (profile lifecycle, capacity, and switch safety).
3. B (CRUD and validation).
4. M11-M13 (runtime profile isolation checks).
5. C and D (proxy and health-check behavior).
6. E and F plus M14-M15 (stats/audit with attribution scope).
7. K.1-K.3 plus M20 (header blocklist, including profile scope).
8. L plus M19 (token costing and spending reports with profile isolation).
9. G, H, K.4, and M16-M18 in isolated destructive lane.
10. I and K.5 (frontend full-stack smoke, including selected vs active profile behavior).
11. O (sidecar control-plane smoke when fixtures or a safe live sidecar are available).
12. J and M21 (non-functional quick pass + failover memory isolation).

---

## 9. Acceptance Criteria

- All `P0` tests pass.
- No proxy contract regressions in routing/failover/logging/audit.
- No sidecar secret leakage in sidecar responses, snapshots, run notes, or screenshots.
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
- Destructive tests (`import`, `delete`) must run against isolated smoke DB.
- Streaming token extraction tests should include both usage-present and usage-missing streams.
- Failover tests must verify per-attempt logging in both `request_logs` and `audit_logs` (when enabled).
- Header blocklist rules are resolved from DB per request (no in-memory cache).
- Header blocklist matching is case-insensitive.
- System blocklist rules are seeded on first boot.
- Prefix rules must end with `-` (e.g. `cf-`, `x-cf-`).
- Profile-scoped management APIs use selected/effective profile scope; global management routes remain outside selected-profile scoping; proxy runtime always uses active profile scope.
