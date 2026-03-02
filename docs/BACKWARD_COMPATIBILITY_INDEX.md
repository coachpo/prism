# Backward Compatibility Index

Generated: 2026-03-02  
Repo: `prism` (root + `backend/` submodule + `frontend/` submodule)

## 1) Scan Coverage

This index was built from full history and code scans:

- Root git history scanned: `git log --all` (126 commits)
- Backend git history scanned: `git -C backend log --all` (66 commits)
- Frontend git history scanned: `git -C frontend log --all` (87 commits)
- Exhaustive code search: `grep`, `rg`, `ast-grep` (where available), plus parallel internal/external research agents
- Compatibility-focused history matches (`git log -G` term-set): root 40, backend 35, frontend 26

Primary search terms included: `compat`, `legacy`, `deprecated`, `fallback`, `v1beta`, `migrate`, `round_robin`, `stream_options`, `alias`, `backward`.

## 2) Compatibility Taxonomy Used

- `protocol_versioning`: multiple API/version surfaces retained at once
- `schema_data_compatibility`: old/new payload shapes accepted together
- `config_migration`: import/export and settings evolution with safe defaults
- `behavioral_fallback`: fallback behavior when data/inputs are missing or invalid
- `deprecation_signaling`: explicit rejection/error for removed legacy values
- `client_fallback_ui`: UI parsing/formatting fallback to keep older data usable

Reference set used for taxonomy alignment:
- SemVer: https://semver.org/
- OpenAPI 3.1: https://spec.openapis.org/oas/v3.1.0
- Google AIP-180: https://google.aip.dev/180
- Protobuf evolution: https://protobuf.dev/programming-guides/proto3/#updating
- RFC 8594 Sunset: https://www.rfc-editor.org/rfc/rfc8594.html
- MDN Progressive Enhancement: https://developer.mozilla.org/en-US/docs/Glossary/Progressive_Enhancement

## 3) Current Backward Compatibility Code Index

### 3.1 Root Runtime

| ID | Type | Location | Compatibility behavior preserved |
|---|---|---|---|
| R01 | behavioral_fallback | `start.sh:9`, `start.sh:19` | Startup mode keeps legacy invocation compatibility with fallback chain: CLI arg -> `START_MODE` env -> `headless`. |
| R02 | behavioral_fallback | `start.sh:47`, `start.sh:53`, `start.sh:55`, `start.sh:67` | Database URL discovery is backward-compatible across setups: process env -> `backend/.env` -> default local PostgreSQL DSN. |
| R03 | behavioral_fallback | `start.sh:82`, `start.sh:83` | Parsed DB host/port normalize missing URL components to `localhost:5432` for compatibility with partial DSNs. |
| R04 | behavioral_fallback | `start.sh:154`, `start.sh:156`, `start.sh:161` | Local-runtime recovery fallback auto-starts PostgreSQL via Docker Compose when a local DB target is unreachable. |

### 3.2 Backend

| ID | Type | Location | Compatibility behavior preserved |
|---|---|---|---|
| B01 | protocol_versioning | `backend/app/routers/proxy.py:736`, `backend/app/routers/proxy.py:747` | Both `/v1/{path}` and `/v1beta/{path}` routes are supported and routed through the same handler. |
| B02 | schema_data_compatibility | `backend/app/routers/proxy.py:153`, `backend/app/routers/proxy.py:161`, `backend/app/routers/proxy.py:163` | Request body/path model IDs are rewritten to upstream target IDs so proxy aliases keep working for both body and Gemini-style path model routing. |
| B03 | schema_data_compatibility | `backend/app/services/proxy_service.py:86`, `backend/app/services/proxy_service.py:101`, `backend/app/services/proxy_service.py:111` | Upstream URL joining preserves compatibility by preventing duplicate version segments (for example `/v1/v1`) when endpoint `base_url` already carries a version prefix. |
| B05 | config_migration | `backend/app/schemas/schemas.py:662`, `backend/app/schemas/schemas.py:680`, `backend/app/routers/config.py:219` | Config contract is pinned to replace-mode imports/exports (`mode: "replace"`). |
| B06 | config_migration | `backend/app/schemas/schemas.py:661`, `backend/app/schemas/schemas.py:673` | Config export/import schema pins version to `1` (`Literal["1"]`). |
| B08 | deprecation_signaling | `backend/app/schemas/schemas.py:311`, `backend/app/schemas/schemas.py:633` | Deprecated `round_robin` is rejected in model and import schemas (`lb_strategy` allows only `single`/`failover`). |
| B11 | behavioral_fallback | `backend/app/services/costing_service.py:240`, `backend/app/services/costing_service.py:260`, `backend/app/services/costing_service.py:262`, `backend/app/services/costing_service.py:318` | Missing special-token prices fall back by policy (`MAP_TO_OUTPUT` or `ZERO_COST`) and persist policy in pricing snapshots. |
| B14 | schema_data_compatibility | `backend/app/routers/models.py:23`, `backend/app/routers/models.py:51`, `backend/app/routers/config.py:422`, `backend/app/routers/config.py:430` | Proxy alias behavior is preserved safely with non-chained, same-provider redirect validation in both CRUD and config-import paths. |
| B15 | config_migration | `backend/app/routers/config.py:52`, `backend/app/routers/config.py:56`, `backend/app/routers/config.py:308`, `backend/app/routers/config.py:399` | Canonical v1 logical references (`endpoint_ref`, `connection_ref`) are required and normalized, preserving stable import/export contracts across migrations. |
| B16 | schema_data_compatibility | `backend/app/models/models.py:193` | Connection display name stays compatible with legacy storage via ORM mapping (`name` field backed by `description` column). |
| B17 | schema_data_compatibility | `backend/app/routers/config.py:162`, `backend/app/routers/config.py:594`, `backend/tests/test_smoke_defect_regressions.py:452` | Empty custom header objects survive config export/import roundtrips (`{}` is preserved, not coerced to null). |
| B18 | deprecation_signaling | `backend/app/routers/models.py:162`, `backend/app/routers/models.py:244` | Removed legacy `model_type` values are explicitly rejected; only `native` or `proxy` are accepted. |

### 3.3 Frontend

| ID | Type | Location | Compatibility behavior preserved |
|---|---|---|---|
| F01 | config_migration | `frontend/src/lib/configImportValidation.ts:73`, `frontend/src/lib/configImportValidation.ts:80` | Import schema requires `config_version: "1"` and `mode: "replace"`. |
| F03 | config_migration | `frontend/src/lib/configImportValidation.ts:26`, `frontend/src/lib/configImportValidation.ts:37`, `frontend/src/lib/configImportValidation.ts:69` | Optional import fields get compatibility defaults, including connection pricing defaults and `user_settings.endpoint_fx_mappings` defaulting to `[]`. |
| F04 | deprecation_signaling | `frontend/src/pages/SettingsPage.tsx:867` | UI blocks invalid imports with explicit message: only schema version 1 + replace mode accepted. |
| F05 | config_migration | `frontend/src/lib/types.ts:398`, `frontend/src/lib/types.ts:409`, `frontend/src/lib/types.ts:416` | Type definitions keep config contract pinned to v1 and replace mode. |
| F06 | behavioral_fallback | `frontend/src/lib/api.ts:43` | API base normalizes trailing slashes; empty base keeps same-origin behavior for older deployment assumptions. |
| F07 | behavioral_fallback | `frontend/src/lib/api.ts:56`, `frontend/src/lib/api.ts:68`, `frontend/src/lib/api.ts:75`, `frontend/src/lib/api.ts:99` | API error handling supports multiple backend error payload shapes (`detail`, `detail[]`, `detail[].msg`) and falls back to `HTTP <status> <statusText>` on body parse failure. |
| F08 | behavioral_fallback | `frontend/src/lib/api.ts:103` | HTTP 204 responses are handled gracefully (`undefined` return). |
| F09 | client_fallback_ui | `frontend/src/context/ProfileContext.tsx:34`, `frontend/src/context/ProfileContext.tsx:37`, `frontend/src/context/ProfileContext.tsx:97` | Profile selection compatibility chain validates persisted IDs first, then falls back persisted valid profile -> active profile -> no selection. |
| F10 | schema_data_compatibility | `frontend/src/context/ProfileContext.tsx:162` | Profile activation carries expected profile version for optimistic concurrency with stale-tab safety. |
| F11 | client_fallback_ui | `frontend/src/pages/StatisticsPage.tsx:134` | Query parameter parsers clamp/validate and fallback to safe defaults for enum/int filters. |
| F12 | client_fallback_ui | `frontend/src/pages/RequestLogsPage.tsx:252` | Request log URL parameter parsers preserve old/bad URL states by coercing to defaults. |
| F13 | client_fallback_ui | `frontend/src/pages/StatisticsPage.tsx:182`, `frontend/src/pages/RequestLogsPage.tsx:304`, `frontend/src/pages/AuditPage.tsx:298`, `frontend/src/pages/ModelDetailPage.tsx:869` | Connection label fallback chain is consistent across views: name -> synthetic `Connection #id`. |
| F14 | client_fallback_ui | `frontend/src/pages/AuditPage.tsx:880`, `frontend/src/components/layout/AppLayout.tsx:90`, `frontend/src/components/layout/AppLayout.tsx:91` | UI preserves readability with fallback labels when names are missing (`Provider #id`, `No profile selected`, `No active profile`). |
| F15 | client_fallback_ui | `frontend/src/pages/ModelDetailPage.tsx:64`, `frontend/src/pages/ModelDetailPage.tsx:72`, `frontend/src/pages/ModelDetailPage.tsx:73` | Cross-page deep links to Request Logs set compatibility-safe defaults (`time_range=24h`, `outcome_filter=all`) when optional params are omitted. |

## 4) Regression Tests Locking Compatibility Behavior

| Test evidence | Location | Locked behavior |
|---|---|---|
| `test_rewrite_gemini_path` / `test_rewrite_gemini_path_stream` | `backend/tests/test_smoke_defect_regressions.py:325`, `backend/tests/test_smoke_defect_regressions.py:335` | Gemini path model rewrite preserves alias compatibility for `/v1beta/models/...` routes. |
| `test_lb_strategy_rejects_round_robin` | `backend/tests/test_smoke_defect_regressions.py:828` | Rejects deprecated `round_robin` strategy. |
| `test_config_import_rejects_round_robin_in_models` | `backend/tests/test_smoke_defect_regressions.py:883` | Config import blocks legacy unsupported strategy values. |
| `test_roundtrip_custom_headers_empty_dict` | `backend/tests/test_smoke_defect_regressions.py:452` | Empty custom header objects remain stable through compatibility roundtrip. |
| `TestDEF009_ConnectionDefaultsPersist` | `backend/tests/test_smoke_defect_regressions.py:1455` | Connection config defaults and create-path persistence stay stable after compatibility cleanup. |
| `TestDEF016_MapToOutputFallback` / `TestDEF017_ZeroCostFallback` | `backend/tests/test_smoke_defect_regressions.py:1928`, `backend/tests/test_smoke_defect_regressions.py:2008` | Costing compatibility fallback policy behavior is locked for missing special-token prices. |
| `test_proxy_routes_use_active_profile_dependency` | `backend/tests/test_smoke_defect_regressions.py:2410` | Both `/v1` and `/v1beta` proxy route families preserve active-profile dependency semantics. |
| `test_validate_import_accepts_v1_logical_refs` / `test_validate_import_rejects_duplicate_logical_connection_refs` | `backend/tests/test_smoke_defect_regressions.py:2433`, `backend/tests/test_smoke_defect_regressions.py:2477` | v1 logical-reference import contract (`endpoint_ref`, `connection_ref`) is enforced and deduplicated. |

## 5) Git History Index (Compatibility-Relevant Commits)

### 5.1 Root Repository

| Hash | Date | Subject | Category |
|---|---|---|---|
| `343ded6` | 2026-02-19 | feat: catch-all proxy for all /v1/* endpoints, UI display name and provider info | initial protocol surface compatibility |
| `57d7f45` | 2026-02-19 | refactor: rename model type 'redirect' to 'proxy', update docs and submodules | terminology migration rollout |
| `67f69e2` | 2026-02-20 | fix: proxy alias rewrite, priority UX clarity, config backup hardening | aliasing + config hardening |
| `1e498a2` | 2026-02-26 | Update backend and frontend for stream_options forwarding | cross-provider request compatibility |
| `a84c9cc` | 2026-02-26 | chore: align docs and references to v1 namespace | versioning documentation sync |
| `1732ea3` | 2026-02-28 | Add PER_1M migration plan and audit UI updates | pricing migration planning |
| `f6f0106` | 2026-02-28 | docs: update architecture docs and bootstrap script | startup/runtime fallback hardening |

### 5.2 Backend Submodule

| Hash | Date | Subject | Category |
|---|---|---|---|
| `89e4012` | 2026-02-20 | fix: rewrite model ID in Gemini URL paths for proxy aliases and harden config export/import | provider/path compatibility |
| `e4fd9f5` | 2026-02-22 | Remove round_robin and add failover recovery policy | deprecated strategy removal |
| `29fc77d` | 2026-02-26 | chore: migrate management API namespace to v1 | API version migration |
| `4ccec03` | 2026-02-26 | Strip stream_options before forwarding upstream | cross-provider compatibility |
| `1f8af60` | 2026-02-26 | Add endpoint toggle to forward stream_options | request compatibility toggle |
| `3ec80bf` | 2026-02-27 | Add connection name field compatibility across API schemas | schema compatibility shim |
| `da95da9` | 2026-02-28 | Drop connection pricing_unit and standardize PER_1M pricing | schema/data migration |
| `6c15b08` | 2026-02-28 | chore: remove pricing_unit migration shim | post-migration cleanup |
| `c0f2daa` | 2026-02-28 | feat: add profile-scoped routing and config isolation | profile-scope compatibility hardening |
| `5848253` | 2026-03-01 | Switch config import/export to v1 ref-only schema | config version pinning |
| `529e365` | 2026-03-01 | refactor: drop backward-compat shims and harden config contract | shim removal after migration |
| `44c0032` | 2026-03-01 | cleanup stream options compatibility shims and connection schema | post-migration compatibility cleanup |
| `c1d069a` | 2026-03-01 | squash migration baseline and require explicit profile attribution | migration baseline stabilization |

### 5.3 Frontend Submodule

| Hash | Date | Subject | Category |
|---|---|---|---|
| `be88b4f` | 2026-02-22 | Default frontend API calls to same-origin and add dev proxy | deployment compatibility fallback |
| `8c417e8` | 2026-02-22 | Add failover recovery UI and strict config import validation | import compatibility validation |
| `45eef63` | 2026-02-26 | Expose stream_options forwarding in endpoint form | request compatibility control |
| `216e5ba` | 2026-02-25 | align frontend with v4 config and renamed costing fields | config field migration |
| `cea4fe0` | 2026-02-28 | Remove pricing unit field from UI and config types | post-migration cleanup |
| `02c70ce` | 2026-02-28 | feat: add profile context and profile-aware dashboard flows | profile migration compatibility |
| `06a7c2f` | 2026-03-01 | fix: improve API error extraction and endpoint delete messaging | client error-shape compatibility |
| `87cc584` | 2026-03-01 | refactor: remove legacy UI/client fallbacks and v1 compat aliases | shim retirement after migration |
| `13a91d7` | 2026-03-01 | harden fallback labels for costing/provider/timezone helpers | UI fallback hardening |
| `4aebf8d` | 2026-03-01 | refresh settings UX and enforce v1 import schema | config version enforcement |

## 6) Quick Index by Compatibility Type

- `protocol_versioning`: B01
- `schema_data_compatibility`: B02, B03, B14, B16, B17, F10
- `config_migration`: B05, B06, B15, F01, F03, F05
- `behavioral_fallback`: R01, R02, R03, R04, B11, F06, F07, F08
- `deprecation_signaling`: B08, B18, F04
- `client_fallback_ui`: F09, F11, F12, F13, F14, F15
- `historical_resolved_or_removed`: see section 5 commit index (root/backend/frontend).

---

If this index needs to be split into "active shims" vs "historical migrations", use section 3 as active/runtime code and section 5 as historical commit provenance.
