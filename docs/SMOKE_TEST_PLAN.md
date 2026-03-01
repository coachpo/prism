# Smoke Test Plan: Prism (Comprehensive)

## 1. Scope and Goals

This smoke test plan validates all documented workflows and core function paths across:

- Backend API contract
- Proxy behavior (routing, aliasing, load balancing, failover, streaming)
- Health detection
- Statistics and token extraction
- Audit logging and redaction
- Header blocklist and sanitization
- Configuration export/import
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
- `docs/DEPLOYMENT_STANDARD.md`

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

- Python `3.11+`, Node `18+`, pnpm `10+`.
- Backend available at `http://localhost:8000`.
- Frontend available at `http://localhost:5173` for UI suites.
- Upstream behavior controlled by test doubles or known test endpoints.
- At least one active model with connections for each provider path under test.

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

1. Providers exist: OpenAI, Anthropic, Gemini (global).
2. Profiles exist: A, B, C; start with A as active runtime profile.
3. Profile-scoped Endpoints (credentials):
   - in profile A: one OpenAI endpoint
   - in profile B: one Anthropic endpoint
   - in profile C: one Gemini endpoint
4. Profile-scoped native models:
   - in profile A: one OpenAI-compatible model with 2+ active connections
   - in profile B: one Anthropic model
   - in profile C: one Gemini model
5. Proxy models:
   - same-provider alias redirecting to a native model in the same profile
6. Connection diversity per profile:
   - active + inactive
   - differing priorities
   - one connection with `custom_headers`
   - one connection with `pricing_enabled=true`
7. Audit toggles initially disabled, then enabled per-case.
8. At least one duplicated `model_id` and endpoint `name` across A/B to validate scoped uniqueness.

---

## 6. API Surface Coverage Matrix

| Endpoint | Coverage IDs |
|---|---|
| `GET /health` | A04 |
| `GET /api/profiles` | M01, M03, M08, M09 |
| `GET /api/profiles/active` | M02, M11 |
| `POST /api/profiles` | M04, M10 |
| `PATCH /api/profiles/{id}` | M05 |
| `POST /api/profiles/{id}/activate` | M06-M07 |
| `DELETE /api/profiles/{id}` | M08-M09 |
| `GET /api/providers` | B01 |
| `GET /api/providers/{id}` | B03 |
| `PATCH /api/providers/{id}` | B02 |
| `GET /api/models` | B04, E12, M03, M12 |
| `GET /api/models/{id}` | B18, M03 |
| `POST /api/models` | B04-B10, M12 |
| `PUT /api/models/{id}` | B08-B10, M03 |
| `DELETE /api/models/{id}` | B11, M03 |
| `GET /api/endpoints` | B12, M03 |
| `POST /api/endpoints` | B13, M03 |
| `PUT /api/endpoints/{id}` | B14, M03 |
| `DELETE /api/endpoints/{id}` | B15, M03 |
| `GET /api/models/{id}/connections` | B18, M03 |
| `POST /api/models/{id}/connections` | B16-B17, L01-L02, M03 |
| `PUT /api/connections/{id}` | B19-B20, L03, M03 |
| `DELETE /api/connections/{id}` | B21, M03 |
| `POST /api/connections/{id}/health-check` | D01-D06 |
| `POST /v1/chat/completions` | C01, C03, C04, C06-C13, E08, E10, L08-L10, M11-M13, M21 |
| `POST /v1/messages` | C02, C04, E08, E10, L08-L10, M11-M13, M21 |
| `GET /api/stats/requests` | E01-E04, M14 |
| `GET /api/stats/summary` | E05-E06, M14 |
| `GET /api/stats/connection-success-rates` | E07 |
| `GET /api/stats/spending` | L11-L13, L19-L20, M19 |
| `DELETE /api/stats/requests` | G01-G03, M14 |
| `GET /api/audit/logs` | F10, F12, M15 |
| `GET /api/audit/logs/{id}` | F11, M15 |
| `DELETE /api/audit/logs` | F13, G04-G05, M15 |
| `GET /api/config/export` | H01-H04, L14, M16 |
| `POST /api/config/import` | H05-H07, L15-L16, M17-M18 |
| `GET /api/config/header-blocklist-rules` | K01, M20 |
| `GET /api/config/header-blocklist-rules/{id}` | K05-K06, M20 |
| `POST /api/config/header-blocklist-rules` | K02-K04, K12-K15, M20 |
| `PATCH /api/config/header-blocklist-rules/{id}` | K07-K09, M20 |
| `DELETE /api/config/header-blocklist-rules/{id}` | K10-K11, M20 |
| `GET /api/settings/costing` | L04, M19 |
| `PUT /api/settings/costing` | L05-L07, M19 |

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
| B05 | P0 | Create duplicate `model_id` in same effective profile | `409` |
| B06 | P0 | Create valid proxy model | `201` |
| B07 | P0 | Proxy missing/invalid `redirect_to` | `400` |
| B08 | P0 | Cross-provider proxy target | `400` |
| B09 | P0 | Proxy target is another proxy | `400` |
| B10 | P0 | Native model with non-null `redirect_to` | `400` |
| B11 | P0 | Delete native model referenced by proxy | `400` with referrer detail |
| B12 | P0 | List profile-scoped endpoints | `200`, returns array scoped to effective profile |
| B13 | P0 | Create profile-scoped endpoint | `201`, endpoint stored in effective profile |
| B14 | P0 | Update profile-scoped endpoint | `200`, changes persist in effective profile |
| B15 | P0 | Delete profile-scoped endpoint in use | `409` conflict |
| B16 | P0 | Create connection on native model | `201` |
| B17 | P0 | Create connection on proxy model | `400` |
| B18 | P1 | List connections for model | `200`, returns array |
| B19 | P0 | Update connection with `custom_headers=null/{}` | Headers removed |
| B20 | P1 | Update connection omitting `custom_headers` | Existing headers retained |
| B21 | P1 | Delete connection | `204`, connection removed |

## C. Proxy Routing, Aliasing, Headers, and Failover

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| C01 | P0 | OpenAI non-stream proxy call | Upstream response proxied as-is |
| C02 | P0 | Anthropic non-stream proxy call | Upstream response proxied as-is |
| C03 | P1 | Gemini route compatibility | Correct routing and auth behavior |
| C04 | P0 | Proxy alias model request | Routed via target native connections; only model rewritten |
| C05 | P0 | Unknown/disabled model | `404` |
| C06 | P0 | `single` strategy | Lowest priority active connection used |
| C07 | P0 | `failover` strategy with recovery | Connection cooldown and passive probe behavior |
| C08 | P0 | Failover on `403/429/500/502/503/529` | Next connection attempted |
| C09 | P0 | Failover on connection error/timeout | Next connection attempted |
| C10 | P0 | All failover attempts fail | `502` with last error detail |
| C11 | P0 | No active connections | `503` |
| C12 | P1 | Header merge order with custom override | Custom headers win over provider/client headers |
| C13 | P1 | Connection `custom_headers` override | Effective headers follow override |

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
| E03 | P0 | Request log filters (`model`, `provider`, `status`, `success`, time) | Correct subsets returned |
| E04 | P0 | Pagination (`limit`, `offset`, `total`) | Consistent counts and windows |
| E05 | P0 | Summary without `from_time` | Uses all historical data |
| E06 | P1 | Summary grouping (`model/provider/connection`) | Groups and aggregates correct |
| E07 | P1 | Connection success-rate API | Values match request logs |
| E08 | P0 | Non-stream token extraction | Token fields match provider format rules |
| E09 | P1 | Unsupported/malformed usage fallback | Token fields null |
| E10 | P0 | Stream token extraction | Token fields populated |
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
| F06 | P0 | Redaction exact headers | Values redacted before storage |
| F07 | P1 | Redaction by name pattern | Values redacted |
| F08 | P1 | Non-sensitive headers | Preserved |
| F09 | P0 | 64KB truncation | `[TRUNCATED]` appended |
| F10 | P0 | Audit list API | `request_body_preview` max 200 chars, ordered desc |
| F11 | P0 | Audit detail API | Full row returned; unknown id is `404` |
| F12 | P0 | Audit filters/pagination | Correct subsets and totals |
| F13 | P0 | Audit delete validation | `400` |
| F14 | P1 | Audit non-interference on write failure | Proxy response unaffected |

## G. Batch Deletion and FK Semantics

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| G01 | P0 | Stats delete with missing mode | `400` |
| G02 | P0 | Stats delete with preset days | Correct `deleted_count`, cutoff semantics |
| G03 | P0 | Delete request logs with linked audit rows | Audit rows remain, `request_log_id` becomes null |
| G04 | P0 | Audit delete with `older_than_days` | Correct deletion |
| G05 | P1 | Audit delete with `before` timestamp | Correct deletion; request logs unaffected |
| G06 | P0 | Stats delete with custom day value | `200`, correct `deleted_count` |
| G07 | P0 | Stats delete rejects invalid day values | `422` |
| G08 | P0 | Stats delete rejects conflicting modes | `400` |
| G09 | P0 | Stats delete all mode | Deletes entire `request_logs` table |
| G10 | P0 | Audit delete with custom day value | `200`, correct `deleted_count` |
| G11 | P0 | Audit delete all mode | Deletes entire `audit_logs` table |
| G12 | P0 | Audit delete rejects multiple active modes | `400` |

## H. Config Export and Import

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| H01 | P0 | Export schema and metadata | `version=7`, `exported_at`, profile-targeted payload with logical refs |
| H02 | P0 | Export excludes IDs/timestamps/health/logs | Exclusion contract respected |
| H03 | P0 | Export includes provider audit policy | Fields preserved |
| H04 | P0 | Export includes connection `custom_headers` | Fields preserved |
| H05 | P0 | Valid import replace (target profile only) | Only effective profile config replaced; other profiles unchanged |
| H06 | P0 | Import failure rollback | Prior config remains intact |
| H07 | P0 | Validation matrix | Correct `400` errors |
| H08 | P1 | Settings UI export filename | `gateway-config-YYYY-MM-DD.json` |
| H09 | P1 | Settings UI import error paths | Parse/backend errors surfaced in toast |

## I. Frontend Workflow Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| I01 | P0 | Sidebar navigation | All routes load |
| I02 | P0 | Dashboard + Models success rate badges | Correct color thresholds and `N/A` |
| I03 | P0 | Model detail connection success badge + tooltip | Correct counts, rates, health detail |
| I04 | P0 | Connection health actions | Toast/banner reflects result |
| I05 | P0 | Statistics cards and request table | Data renders and updates |
| I06 | P0 | Statistics "All" time range consistency | Summary totals align with table totals |
| I07 | P0 | Statistics provider filter | Only OpenAI/Anthropic/Gemini options |
| I08 | P0 | Audit list/filter/detail UI | Works end-to-end; stream notice shown |
| I09 | P0 | Settings audit toggles | Persist and reflect backend |
| I10 | P0 | Settings data management preset buttons | Correct API calls and toasts |
| I11 | P1 | Connection custom header editor | Add/remove/persist roundtrip |
| I12 | P1 | Frontend error details | Backend `detail` surfaced to user |
| I13 | P0 | Settings data management custom days flow | Custom day input validates, calls API correctly |
| I14 | P0 | Settings data management delete-all flow | Confirmation dialog shows "ALL", calls `delete_all=true` API |
| I15 | P0 | Settings data management in-flight disable | All delete buttons disabled during active deletion |
| I16 | P0 | Model detail connection dialog token pricing section | Pricing fields save and reload correctly |
| I17 | P0 | Settings costing and currency card | Report currency + symbol load/save |
| I18 | P0 | Settings FX mapping editor | Add/remove mapping enforces unique `(model_id, connection_id)` |
| I19 | P0 | Statistics spending tab filters and pagination | Controls update data correctly |
| I20 | P0 | Statistics operations request log costing columns | Breakdown columns render without UI regressions |
| I21 | P0 | Operations special-token row filter behavior | Filter only changes request-log rows |
| I22 | P0 | Null-vs-zero rendering in request log metrics | Null values render `N/A`, zero renders as `0` |
| I23 | P0 | Spending "Special Tokens Captured" card correctness | Card shows cached total and detail |
| I24 | P0 | Responsive token visibility below `xl` | Compact `Usage` column shows summary |
| I25 | P0 | No-regression check for existing costing indicators | Existing spend columns still render correctly |

## J. Non-Functional Smoke

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| J01 | P1 | 10+ concurrent proxy requests | No crashes; logs complete |
| J02 | P1 | Added proxy latency quick check | Within acceptable envelope |
| J03 | P1 | No-auth local operation | Expected unrestricted local usage |
| J04 | P1 | OpenAPI sanity | Core routes present |
| J05 | P1 | PostgreSQL hygiene | DB schema and migration state are valid for smoke environment |

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
| K10 | P0 | Delete user rule | `204` |
| K11 | P0 | Delete system rule | `400` |

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
| K22 | P0 | Provider auth headers remain correct | Auth headers present and correct |
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
| K37 | P0 | System rule edit/delete buttons disabled | Icons disabled for system rules |
| K38 | P1 | Add rule validation: prefix without trailing `-` | Error toast prevents save |
| K39 | P1 | Add rule validation: empty name or pattern | Save button behavior prevents empty submission |

## L. Token Costing and Spending Reports

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| L01 | P0 | Create connection with pricing enabled | `201`, pricing fields persisted |
| L02 | P0 | Create connection with pricing disabled | `201`, no validation on price fields |
| L03 | P0 | Update connection pricing | `200`, `pricing_config_version` increments |
| L04 | P0 | GET `/api/settings/costing` | Returns defaults |
| L05 | P0 | PUT `/api/settings/costing` with FX mappings | `200`, settings persist |
| L06 | P0 | PUT `/api/settings/costing` rejects `fx_rate <= 0` | `400` |
| L07 | P0 | PUT `/api/settings/costing` rejects duplicate (model, connection) | `400` |
| L08 | P0 | Proxy successful request with priced connection | `request_log` has cost fields populated |
| L09 | P0 | Proxy failed request | `billable_flag=false`, all `cost_micros=0` |
| L10 | P0 | Proxy successful request with unpriced connection | `priced_flag=false`, `unpriced_reason` set |
| L11 | P0 | GET `/api/stats/spending` summary | Returns correct totals |
| L12 | P0 | GET `/api/stats/spending` `group_by=model` | Returns grouped rows |
| L13 | P0 | GET `/api/stats/spending` excludes failed requests | Failed requests not in totals |
| L14 | P0 | Config export version 1 | Includes pricing and profile-scoped `user_settings` |
| L15 | P0 | Config import v1 | Restores pricing and settings into target profile |
| L16 | P0 | Config import non-v1 rejection | `400` error (only `v1` is accepted) |
| L17 | P1 | FX conversion with custom rate | Correct converted cost |
| L18 | P1 | Model rename updates FX mapping keys | FX mappings remain valid |
| L19 | P1 | Spending report pagination | `limit`/`offset` respected |
| L20 | P1 | Spending report `top_n` | Returns correct top spenders |
| L21 | P1 | Legacy request logs (pre-costing) | `unpriced_reason=LEGACY_NO_COST_DATA` |
| L22 | P1 | `MAP_TO_OUTPUT` fallback price policy | Missing special tokens use output price |
| L23 | P1 | `ZERO_COST` fallback price policy | Missing special tokens use zero price |

## M. Profile Isolation and Context Semantics

| ID | Pri | Scenario | Expected Result |
|---|---|---|---|
| M01 | P0 | List profiles | `200`, excludes soft-deleted profiles from normal listing |
| M02 | P0 | Get active profile | Exactly one active profile returned |
| M03 | P0 | Management API profile resolution (`X-Profile-Id` absent vs present) | Absent uses active profile; header scopes to selected profile |
| M04 | P0 | Create profile under capacity | `201`, profile created as inactive by default |
| M05 | P0 | Update profile metadata | `200`, name/description persisted |
| M06 | P0 | Activate profile with correct CAS payload | Activation succeeds atomically; active profile/version updated |
| M07 | P0 | Activate profile with stale CAS payload | `409` conflict; previous active profile unchanged |
| M08 | P0 | Delete inactive profile | Soft-delete succeeds; profile omitted from default listings |
| M09 | P0 | Delete active profile | Rejected (`400` or `409`), active profile remains unchanged |
| M10 | P0 | Create 11th non-deleted profile | `409` with actionable delete-before-create error |
| M11 | P0 | Runtime request with `X-Profile-Id` override header | Runtime ignores override and uses active profile context only |
| M12 | P0 | Same `model_id` exists in A/B with different connections | Routing uses active profile mappings only; no cross-profile resolution |
| M13 | P0 | Proxy alias target exists only in another profile | Alias resolution fails (`404`) under current active profile |
| M14 | P0 | Request-log attribution and stats scope | Every row has immutable `profile_id`; stats/list/delete operate on effective profile only |
| M15 | P0 | Audit attribution and scope | Every row has immutable `profile_id`; list/detail/delete are profile-scoped |
| M16 | P0 | Config export from selected profile | Output is profile-targeted `version=1` and uses logical refs (`endpoint_ref`, `connection_ref`) |
| M17 | P0 | Config import v1 replace into profile A | Replaces A only; profile B/C scoped data remains unchanged |
| M18 | P0 | Config import non-v1 rejection | Non-v1 payloads are rejected with `400` |
| M19 | P0 | Costing/settings isolation | Updating currency/FX in A does not mutate B/C settings or spending results |
| M20 | P0 | Header blocklist scope merge | Runtime/effective rules include global system rules + selected profile user rules only |
| M21 | P1 | Failover recovery-state isolation by profile | Cooldown/recovery state in profile A does not affect profile B |
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
11. J and M21 (non-functional quick pass + failover memory isolation).

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

- Time cutoff tests use server UTC (`older_than_days` and `before` semantics).
- `delete_all=true` mode deletes all records without a time cutoff.
- Destructive tests (`import`, `delete`) must run against isolated smoke DB.
- Streaming token extraction tests should include both usage-present and usage-missing streams.
- Failover tests must verify per-attempt logging in both `request_logs` and `audit_logs` (when enabled).
- Header blocklist rules are resolved from DB per request (no in-memory cache).
- Header blocklist matching is case-insensitive.
- System blocklist rules are seeded on first boot.
- Prefix rules must end with `-` (e.g. `cf-`, `x-cf-`).
- Management APIs use selected/effective profile scope; proxy runtime always uses active profile scope.


## 12. Latest Frontend Verification Snapshot (Profile Isolation F1-F4)

```text
Run ID: FRONTEND-PROFILE-ISOLATION-2026-02-28
Date: 2026-02-28 20:25:56 EET
Commit: a2c86c7
Environment: local frontend (pnpm)

P0 Pass/Fail: Partial (build passes; lint currently fails due to pre-existing unrelated errors)
P1 Pass/Fail: Partial

Results:
- pnpm run build: PASS
- pnpm run lint: FAIL

Failures:
- [lint] src/components/layout/AppLayout.tsx:180
  - Observed: no-useless-escape for escaped quotes
  - Expected: no lint errors
  - Repro: cd frontend && pnpm run lint
  - Evidence: ESLint output

- [lint] src/context/ProfileContext.tsx:238
  - Observed: react-refresh/only-export-components
  - Expected: no lint errors
  - Repro: cd frontend && pnpm run lint
  - Evidence: ESLint output

Warnings:
- src/hooks/useConnectionNavigation.ts:26 unnecessary dependency warning
- src/pages/ModelDetailPage.tsx:181 warning resolved in follow-up change (fetch effect now keyed by revision via useEffect dependency)

Notes:
- Frontend profile-isolation wiring completed for revision-driven refresh on: ModelDetail, Endpoints, Statistics, RequestLogs, Audit, and Settings pages.
- Settings import/export copy now states selected-profile scope and confirm dialog clarifies only selected profile is replaced.
- Config import validation supports version 1 only with optional mode and default mode=replace.
```

## 13. Profile Isolation Revision Evidence Matrix (2026-02-28)

Source inputs: `docs/PROFILE_ISOLATION_REQUIREMENTS.md`, `docs/PROFILE_ISOLATION_UPGRADE_PLAN.md`, `docs/PROFILE_ISOLATION_FRONTEND_ITERATION_PLAN.md`, `docs/PROFILE_ISOLATION_RESEARCH_REFERENCES.md`, and `docs/PROFILE_ISOLATION_SUPPORTING_EVIDENCE.md`.


This appendix provides a provenance map between smoke scenarios, requirement IDs, and implementation revisions for the profile-isolation rollout.

### 13.1 Commit-to-Test Mapping

| Revision | Areas validated by this plan | Primary smoke IDs |
|---|---|---|
| Backend `c0f2daa` | Active/effective scope split, profile CRUD/activation/delete guards, profile-attributed routing/logging/audit, strict v1 config behavior, failover memory namespace | M01-M21, C01-C13, E01-E12, F01-F14, H01-H07, L04-L16 |
| Frontend `02c70ce` | Profile context bootstrap, selected-vs-active UX, header propagation, revision-based scoped refetch, settings import copy/flow | I01-I25, M03, M11, M16-M19 |
| Root/docs `f6f0106` | Documentation/bootstrap alignment for profile-isolated operation model | A01-A06, documentation trace checks in release review |

### 13.2 Requirement Trace Anchors

| Requirement | Smoke IDs |
|---|---|
| FR-001 Profile lifecycle and limits | M01-M10 |
| FR-002 Scoped data model behavior | M03, M12, B04-B21 |
| FR-003 Runtime isolation | M11-M13, C01-C11 |
| FR-004 CAS-safe activation | M06-M07 |
| FR-005 In-memory failover isolation | M21, C07-C09 |
| FR-006 API scope semantics | M03, M11, M14-M15 |
| FR-007 Config export/import isolation | M16-M18, H01-H07, L14-L16 |
| FR-008 Costing/settings isolation | M19, L04-L13 |
| FR-009 Immutable observability attribution | M14-M15, E01-E07, F10-F13 |
| FR-010 Frontend selected-vs-active behavior | I01-I15, M03, M11 |

Execution note: this appendix is traceability metadata only; it does not replace the authoritative scenario definitions in sections A-M.