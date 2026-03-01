# Backward Compatibility Index

Generated: 2026-03-02  
Repo: `prism` (root + `backend/` submodule + `frontend/` submodule)

## 1) Scan Coverage

This index was built from full history and code scans:

- Root git history scanned: `git log --all` (120 commits)
- Backend git history scanned: `git -C backend log --all` (60 commits)
- Frontend git history scanned: `git -C frontend log --all` (83 commits)
- Exhaustive code search: `grep`, `rg`, `ast-grep` (where available), plus parallel internal/external research agents

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

### 3.1 Backend

| ID | Type | Location | Compatibility behavior preserved |
|---|---|---|---|
| B01 | protocol_versioning | `backend/app/routers/proxy.py:736`, `backend/app/routers/proxy.py:747` | Both `/v1/{path}` and `/v1beta/{path}` routes are supported and routed through the same handler. |
| B02 | schema_data_compatibility | `backend/app/routers/proxy.py:154`, `backend/app/routers/proxy.py:162` | Request body/path model IDs are rewritten to upstream target IDs so proxy aliases keep working. |
| B03 | schema_data_compatibility | `backend/app/services/proxy_service.py:92`, `backend/app/services/proxy_service.py:108` | Upstream URL joining prevents duplicate version segments (for example `/v1/v1`) when `base_url` already carries a version prefix. |
| B05 | config_migration | `backend/app/schemas/schemas.py:662`, `backend/app/schemas/schemas.py:680` | Config contract is pinned to replace-mode imports (`mode: "replace"`). |
| B06 | config_migration | `backend/app/schemas/schemas.py:661`, `backend/app/schemas/schemas.py:673` | Config export/import schema pins version to `1` (`Literal["1"]`). |
| B07 | config_migration | `backend/app/routers/config.py:219` | Config export explicitly emits `config_version="1"`. |
| B08 | deprecation_signaling | `backend/app/schemas/schemas.py:633` | `round_robin` is rejected by schema validation (`lb_strategy` only allows `single`/`failover`). |
| B11 | behavioral_fallback | `backend/app/services/costing_service.py:240` | Missing special-token prices fall back via policy: `MAP_TO_OUTPUT` or `ZERO_COST`. |
| B14 | schema_data_compatibility | `backend/app/routers/models.py:31` | Proxy validation enforces non-chained aliasing (`redirect_to` must target native model), preserving alias behavior safely. |

### 3.2 Frontend

| ID | Type | Location | Compatibility behavior preserved |
|---|---|---|---|
| F01 | config_migration | `frontend/src/lib/configImportValidation.ts:73`, `frontend/src/lib/configImportValidation.ts:80` | Import schema requires `config_version: "1"` and `mode: "replace"`. |
| F03 | config_migration | `frontend/src/lib/configImportValidation.ts:26`, `frontend/src/lib/configImportValidation.ts:37` | Optional connection import fields get defaults (`pricing_enabled`, `pricing_config_version`, policy). |
| F04 | deprecation_signaling | `frontend/src/pages/SettingsPage.tsx:861` | UI blocks invalid imports with explicit message: only schema version 1 + replace mode accepted. |
| F05 | config_migration | `frontend/src/lib/types.ts:398`, `frontend/src/lib/types.ts:409`, `frontend/src/lib/types.ts:416` | Type definitions keep config contract pinned to v1 and replace mode. |
| F06 | behavioral_fallback | `frontend/src/lib/api.ts:43` | API base normalizes trailing slashes; empty base keeps same-origin behavior for older deployment assumptions. |
| F07 | behavioral_fallback | `frontend/src/lib/api.ts:99` | Error parsing falls back to `statusText` if JSON decode fails. |
| F08 | behavioral_fallback | `frontend/src/lib/api.ts:103` | HTTP 204 responses are handled gracefully (`undefined` return). |
| F09 | client_fallback_ui | `frontend/src/context/ProfileContext.tsx:97` | Profile selection fallback chain is persisted profile -> active profile -> no selection. |
| F10 | schema_data_compatibility | `frontend/src/context/ProfileContext.tsx:163` | Profile activation carries expected profile version for optimistic concurrency with stale-tab safety. |
| F11 | client_fallback_ui | `frontend/src/pages/StatisticsPage.tsx:134` | Query parameter parsers clamp/validate and fallback to safe defaults for enum/int filters. |
| F12 | client_fallback_ui | `frontend/src/pages/RequestLogsPage.tsx:252` | Request log URL parameter parsers preserve old/bad URL states by coercing to defaults. |
| F13 | client_fallback_ui | `frontend/src/pages/StatisticsPage.tsx:182`, `frontend/src/pages/RequestLogsPage.tsx:303` | Connection label fallback chain: name -> synthetic `Connection #id`. |

## 4) Regression Tests Locking Compatibility Behavior

| Test evidence | Location | Locked behavior |
|---|---|---|
| `test_lb_strategy_rejects_round_robin` | `backend/tests/test_smoke_defect_regressions.py:854` | Rejects deprecated `round_robin` strategy. |
| `test_config_import_rejects_round_robin_in_models` | `backend/tests/test_smoke_defect_regressions.py:908` | Config import blocks legacy unsupported strategy values. |
| `TestDEF009_ConnectionDefaultsPersist` | `backend/tests/test_smoke_defect_regressions.py:1451` | Connection config defaults and create-path persistence stay stable after compatibility cleanup. |
| (removed) | `TestDEF019_StripStreamOptionsHostAgnostic` | (Removed from active regression set after stream-options toggle cleanup.) |
| Route dependency check (`/v1` + `/v1beta`) | `backend/tests/test_smoke_defect_regressions.py:2410` | Both proxy route families preserve active-profile dependency behavior. |

## 5) Git History Index (Compatibility-Relevant Commits)

### 5.1 Root Repository

| Hash | Date | Subject | Category |
|---|---|---|---|
| `67f69e2` | 2026-02-20 | fix: proxy alias rewrite, priority UX clarity, config backup hardening | aliasing + config hardening |
| `1e498a2` | 2026-02-26 | Update backend and frontend for stream_options forwarding | cross-provider compatibility |
| `a84c9cc` | 2026-02-26 | chore: align docs and references to v1 namespace | versioning documentation sync |
| `1732ea3` | 2026-02-28 | Add PER_1M migration plan and audit UI updates | pricing migration planning |

### 5.2 Backend Submodule

| Hash | Date | Subject | Category |
|---|---|---|---|
| `5848253` | 2026-03-01 | Switch config import/export to v1 ref-only schema | config version pinning |
| `3ec80bf` | 2026-02-27 | Add connection name field compatibility across API schemas | schema compatibility shim |
| `da95da9` | 2026-02-28 | Drop connection pricing_unit and standardize PER_1M pricing | schema/data migration |
| `6c15b08` | 2026-02-28 | chore: remove pricing_unit migration shim | post-migration cleanup |
| `1f8af60` | 2026-02-26 | Add endpoint toggle to forward stream_options | request compatibility toggle |
| `4ccec03` | 2026-02-26 | Strip stream_options before forwarding upstream | cross-provider compatibility |
| `29fc77d` | 2026-02-26 | chore: migrate management API namespace to v1 | API version migration |
| `e4fd9f5` | 2026-02-22 | Remove round_robin and add failover recovery policy | deprecated strategy removal |
| `89e4012` | 2026-02-20 | rewrite model ID in Gemini URL paths for proxy aliases and harden config export/import | provider/path compatibility |
| `379bdf4` | 2026-02-19 | add health_detail field, upstream error extraction, and /v1/v1 failsafe | URL/path fallback compatibility |
| `604a613` | 2026-02-19 | rename model_type 'redirect' to 'proxy' | terminology migration |

### 5.3 Frontend Submodule

| Hash | Date | Subject | Category |
|---|---|---|---|
| `4aebf8d` | 2026-03-01 | refresh settings UX and enforce v1 import schema | config version enforcement |
| `45eef63` | 2026-02-26 | Expose stream_options forwarding in endpoint form | request compatibility control |
| `8c417e8` | 2026-02-22 | Add failover recovery UI and strict config import validation | import compatibility validation |
| `be88b4f` | 2026-02-22 | Default frontend API calls to same-origin and add dev proxy | deployment compatibility fallback |
| `02c70ce` | 2026-02-28 | add profile context and profile-aware dashboard flows | profile migration compatibility |
| `216e5ba` | 2026-02-25 | align frontend with v4 config and renamed costing fields | config field migration |
| `cea4fe0` | 2026-02-28 | Remove pricing unit field from UI and config types | post-migration cleanup |

## 6) Quick Index by Compatibility Type

- `protocol_versioning`: B01
- `schema_data_compatibility`: B02, B03, B14, F10
- `config_migration`: B05, B06, B07, F01, F03, F05
- `behavioral_fallback`: B11, F06, F07, F08
- `deprecation_signaling`: B08, F04
- `client_fallback_ui`: F09, F11, F12, F13
- `historical_resolved_or_removed`: B04, B09, B10, B12, B13, F02, F14, F15, F16, F17, F18, F19, F20

---

If this index needs to be split into "active shims" vs "historical migrations", use section 3 as active/runtime code and section 5 as historical commit provenance.
