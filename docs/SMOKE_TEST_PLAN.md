# Smoke Test Plan: Prism (Comprehensive)

## 1. Scope and Goals

This smoke test plan validates all documented workflows and core function paths across:

- Backend API contract
- Proxy behavior (routing, aliasing, load balancing, failover, streaming)
- Health detection
- Statistics and token extraction
- Audit logging and redaction
- Configuration export/import
- Batch data deletion semantics
- Frontend management flows

The objective is a fast but thorough confidence pass that catches regressions before release.

---

## 2. Source Documents Covered

This plan is synthesized from:

- `docs/API_SPEC.md`
- `docs/ARCHITECTURE.md`
- `docs/DATA_MODEL.md`
- `docs/PRD.md`
- `docs/DEPLOYMENT.md`
- `docs/DESIGN_REQUEST_AUDIT.md`
- `docs/DESIGN_CONFIG_EXPORT_IMPORT.md`
- Existing `docs/SMOKE_TEST_PLAN.md` (replaced by this file)

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

- Use dedicated DB for smoke: `backend/gateway_smoke.db`.
- Reset DB between destructive scenarios.
- Never run destructive tests on production-like DB.

---

## 4. Environment Prerequisites

- Python `3.11+`, Node `18+`, npm `9+`.
- Backend available at `http://localhost:8000`.
- Frontend available at `http://localhost:5173` for UI suites.
- Upstream behavior controlled by test doubles or known test endpoints.
- At least one active model with endpoints for each provider path under test.

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

1. Providers exist: OpenAI, Anthropic, Gemini.
2. Native models:
   - one OpenAI-compatible model with 2+ active endpoints
   - one Anthropic model
   - one Gemini model
3. Proxy models:
   - same-provider alias redirecting to a native model
4. Endpoint diversity:
   - active + inactive
   - differing priorities
   - one endpoint with `custom_headers`
   - one endpoint with `auth_type` override
5. Audit toggles initially disabled, then enabled per-case.

---

## 6. API Surface Coverage Matrix

| Endpoint | Coverage IDs |
|---|---|
| `GET /health` | A04 |
| `GET /api/providers` | B01 |
| `GET /api/providers/{id}` | B03 |
| `PATCH /api/providers/{id}` | B02 |
| `GET /api/models` | B04, E12 |
| `GET /api/models/{id}` | B18 |
| `POST /api/models` | B04-B10 |
| `PUT /api/models/{id}` | B08-B10 |
| `DELETE /api/models/{id}` | B11 |
| `GET /api/models/{id}/endpoints` | B18 |
| `POST /api/models/{id}/endpoints` | B12-B15 |
| `PUT /api/endpoints/{id}` | B16-B17 |
| `DELETE /api/endpoints/{id}` | B18 |
| `POST /api/endpoints/{id}/health-check` | D01-D06 |
| `POST /v1/chat/completions` | C01, C03, C04, C06-C13, E08, E10 |
| `POST /v1/messages` | C02, C04, E08, E10 |
| `GET /api/stats/requests` | E01-E04 |
| `GET /api/stats/summary` | E05-E06 |
| `GET /api/stats/endpoint-success-rates` | E07 |
| `DELETE /api/stats/requests` | G01-G03 |
| `GET /api/audit/logs` | F10, F12 |
| `GET /api/audit/logs/{id}` | F11 |
| `DELETE /api/audit/logs` | F13, G04-G05 |
| `GET /api/config/export` | H01-H04 |
| `POST /api/config/import` | H05-H07 |

---

## 7. Detailed Test Cases

## A. Startup and Deployment

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| A01 | P0 | Start backend in `headless` mode | Backend process starts, API reachable |
| A02 | P0 | Start in `full` mode | Backend + frontend reachable |
| A03 | P0 | First boot with empty DB | DB created, providers seeded |
| A04 | P0 | `GET /health` | `200`, JSON contains `status=ok` |
| A05 | P1 | OpenAPI endpoints (`/docs`, `/redoc`, `/openapi.json`) | Accessible |
| A06 | P1 | CORS preflight | Wildcard CORS headers present |

## B. Configuration CRUD and Validation

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| B01 | P0 | List providers | Includes `audit_enabled`, `audit_capture_bodies` |
| B02 | P0 | Patch provider audit fields | Fields persist; omitted field unchanged |
| B03 | P1 | Get/patch unknown provider | `404` |
| B04 | P0 | Create native model | `201`, model stored |
| B05 | P0 | Create duplicate `model_id` | `409` |
| B06 | P0 | Create valid proxy model | `201` |
| B07 | P0 | Proxy missing/invalid `redirect_to` | `400` |
| B08 | P0 | Cross-provider proxy target | `400` |
| B09 | P0 | Proxy target is another proxy | `400` |
| B10 | P0 | Native model with non-null `redirect_to` | `400` |
| B11 | P0 | Delete native model referenced by proxy | `400` with referrer detail |
| B12 | P0 | Create endpoint on native model | `201` |
| B13 | P0 | Create endpoint on proxy model | `400` |
| B14 | P0 | Base URL trailing slash normalization | Stored without trailing slash |
| B15 | P0 | Invalid base URL (`/v1/v1` or missing scheme/host) | `422` |
| B16 | P0 | Update endpoint with `custom_headers=null/{}` | Headers removed |
| B17 | P1 | Update endpoint omitting `custom_headers` | Existing headers retained |
| B18 | P1 | Delete endpoint then list/get model | Endpoint absent, model still valid |

## C. Proxy Routing, Aliasing, Headers, and Failover

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| C01 | P0 | OpenAI non-stream proxy call | Upstream response proxied as-is |
| C02 | P0 | Anthropic non-stream proxy call | Upstream response proxied as-is |
| C03 | P1 | Gemini route compatibility | Correct routing and auth behavior |
| C04 | P0 | Proxy alias model request | Routed via target native endpoints; only model rewritten |
| C05 | P0 | Unknown/disabled model | `404` |
| C06 | P0 | `single` strategy | Lowest priority active endpoint used |
| C07 | P0 | `round_robin` strategy | Endpoint rotation across calls |
| C08 | P0 | Failover on `429/500/502/503/529` | Next endpoint attempted |
| C09 | P0 | Failover on connection error/timeout | Next endpoint attempted |
| C10 | P0 | All failover attempts fail | `502` with last error detail |
| C11 | P0 | No active endpoints | `503` |
| C12 | P1 | Header merge order with custom override | Custom headers win over provider/client headers |
| C13 | P1 | Endpoint `auth_type` override | Effective auth header follows override |

## D. Endpoint Health Check and URL Failsafe

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
| E03 | P0 | Request log filters (`model`, `provider`, `status`, `success`, time) | Correct subsets returned |
| E04 | P0 | Pagination (`limit`, `offset`, `total`) | Consistent counts and windows |
| E05 | P0 | Summary without `from_time` | Uses all historical data |
| E06 | P1 | Summary grouping (`model/provider/endpoint`) | Groups and aggregates correct |
| E07 | P1 | Endpoint success-rate API | Values match request logs |
| E08 | P0 | Non-stream token extraction (OpenAI, Anthropic messages, count_tokens) | Token fields match provider format rules |
| E09 | P1 | Unsupported/malformed usage fallback | Token fields null |
| E10 | P0 | Stream token extraction (OpenAI include_usage, Anthropic events) | Token fields populated |
| E11 | P1 | Streaming without usage fields | Token fields null |
| E12 | P0 | Model health fields in `/api/models` | Weighted health and request totals correct |

## F. Audit Logging

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| F01 | P0 | Audit disabled provider | No audit row created |
| F02 | P0 | Audit enabled + body capture enabled | Request/response metadata and bodies recorded |
| F03 | P0 | Body capture disabled | Bodies stored as null |
| F04 | P0 | Streaming audited request | `response_body` null; other fields recorded |
| F05 | P0 | Failover with audit enabled | One audit row per upstream attempt |
| F06 | P0 | Redaction exact headers (`authorization`, `x-api-key`, `x-goog-api-key`) | Values redacted before storage |
| F07 | P1 | Redaction by name pattern (`key|secret|token|auth`) | Values redacted |
| F08 | P1 | Non-sensitive headers | Preserved |
| F09 | P0 | 64KB truncation | `[TRUNCATED]` appended |
| F10 | P0 | Audit list API | `request_body_preview` max 200 chars, ordered desc |
| F11 | P0 | Audit detail API | Full row returned; unknown id is `404` |
| F12 | P0 | Audit filters/pagination | Correct subsets and totals |
| F13 | P0 | Audit delete validation (both/neither params) | `400` |
| F14 | P1 | Audit non-interference on write failure | Proxy response unaffected |

## G. Batch Deletion and FK Semantics

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| G01 | P0 | Stats delete with missing mode (neither `older_than_days` nor `delete_all`) | `400` |
| G02 | P0 | Stats delete with preset days (7/15/30) | Correct `deleted_count`, cutoff semantics |
| G03 | P0 | Delete request logs with linked audit rows | Audit rows remain, `request_log_id` becomes null |
| G04 | P0 | Audit delete with `older_than_days` | Correct deletion |
| G05 | P1 | Audit delete with `before` timestamp | Correct deletion; request logs unaffected |
| G06 | P0 | Stats delete with custom day value (`older_than_days=45`) | `200`, correct `deleted_count` |
| G07 | P0 | Stats delete rejects invalid day values (`0`, negative) | `422` (FastAPI validation) |
| G08 | P0 | Stats delete rejects conflicting modes (`older_than_days` + `delete_all=true`) | `400` |
| G09 | P0 | Stats delete all mode (`delete_all=true`) | Deletes entire `request_logs` table, returns count |
| G10 | P0 | Audit delete with custom day value (`older_than_days=45`) | `200`, correct `deleted_count` |
| G11 | P0 | Audit delete all mode (`delete_all=true`) | Deletes entire `audit_logs` table, returns count |
| G12 | P0 | Audit delete rejects multiple active modes (`before` + custom, custom + all, before + all) | `400` |

## H. Config Export and Import

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| H01 | P0 | Export schema and metadata | `version=1`, `exported_at`, providers/models arrays |
| H02 | P0 | Export excludes IDs/timestamps/health/logs | Exclusion contract respected |
| H03 | P0 | Export includes provider audit policy | Fields preserved |
| H04 | P0 | Export includes endpoint `auth_type` and `custom_headers` | Fields preserved |
| H05 | P0 | Valid import full replacement | Existing config replaced, counts accurate |
| H06 | P0 | Import failure rollback | Prior config remains intact |
| H07 | P0 | Validation matrix (version/provider/model/redirect/proxy endpoints/native redirect rules) | Correct `400` errors |
| H08 | P1 | Settings UI export filename | `gateway-config-YYYY-MM-DD.json` |
| H09 | P1 | Settings UI import error paths | Parse/backend errors surfaced in toast |

## I. Frontend Workflow Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| I01 | P0 | Sidebar navigation (`/dashboard`, `/models`, `/statistics`, `/audit`, `/settings`) | All routes load |
| I02 | P0 | Dashboard + Models success rate badges | Correct color thresholds and `N/A` |
| I03 | P0 | Model detail endpoint success badge + tooltip | Correct counts, rates, health detail |
| I04 | P0 | Endpoint health actions (table + dialog test) | Toast/banner reflects result |
| I05 | P0 | Statistics cards and request table | Data renders and updates |
| I06 | P0 | Statistics "All" time range consistency | Summary totals align with table totals |
| I07 | P0 | Statistics provider filter | Only OpenAI/Anthropic/Gemini options |
| I08 | P0 | Audit list/filter/detail UI | Works end-to-end; stream notice shown |
| I09 | P0 | Settings audit toggles | Persist and reflect backend |
| I10 | P0 | Settings data management preset buttons | Correct API calls and toasts |
| I11 | P1 | Endpoint custom header editor | Add/remove/persist roundtrip |
| I12 | P1 | Frontend error details | Backend `detail` surfaced to user |
| I13 | P0 | Settings data management custom days flow | Custom day input validates (≥1, integer), calls API correctly |
| I14 | P0 | Settings data management delete-all flow | Confirmation dialog shows "ALL", calls `delete_all=true` API |
| I15 | P0 | Settings data management in-flight disable | All delete buttons disabled during active deletion |

## J. Non-Functional Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| J01 | P1 | 10+ concurrent proxy requests | No crashes; logs complete |
| J02 | P1 | Added proxy latency quick check | Within acceptable envelope |
| J03 | P1 | No-auth local operation | Expected unrestricted local usage |
| J04 | P1 | OpenAPI sanity | Core routes present |
| J05 | P1 | SQLite hygiene | DB files created in expected location |

---

## 8. Recommended Execution Order

1. A (startup/health).
2. B (CRUD and validation).
3. C and D (proxy and health-check behavior).
4. E and F (stats and audit).
5. G and H in isolated destructive lane.
6. I (frontend full-stack smoke).
7. J (non-functional quick pass).

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

- Time cutoff tests use server UTC (`older_than_days` and `before` semantics). `older_than_days` accepts any integer ≥ 1 (not limited to presets).
- `delete_all=true` mode deletes all records without a time cutoff.
- Destructive tests (`import`, `delete`) must run against isolated smoke DB.
- Streaming token extraction tests should include both usage-present and usage-missing streams.
- Failover tests must verify per-attempt logging in both `request_logs` and `audit_logs` (when enabled).
