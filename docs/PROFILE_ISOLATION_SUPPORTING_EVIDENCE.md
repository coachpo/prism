# Profile Isolation Supporting Evidence

## Document Metadata

- Status: Draft (analysis-only)
- Date: 2026-02-28
- Scope: Internal evidence, risk controls, validation planning, and doc consistency findings
- Related requirement spec: `docs/PROFILE_ISOLATION_REQUIREMENTS.md`
- Constraint: No implementation changes; documentation only

## 1. Purpose

This document provides the technical evidence and validation plan supporting isolated config profiles (A/B/C), where each profile owns independent model-to-connection mappings and only the active profile serves runtime traffic.

## 2. Internal Evidence Map (Current-State)

### 2.1 Global namespace and uniqueness (no profile boundary)

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `backend/app/models/models.py:47` | `ModelConfig.model_id` is globally unique | Same model id cannot be reused across profiles |
| `backend/app/models/models.py:80` | `Endpoint.name` is globally unique | Endpoint names collide across profiles |
| `backend/app/models/models.py:259` | FX mapping uniqueness is `(model_id, endpoint_id)` | Mapping scope is global, not profile-scoped |

### 2.2 Runtime model resolution is global

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `backend/app/services/loadbalancer.py:23` | Model lookup filters by `ModelConfig.model_id` only | Active profile cannot constrain routing |
| `backend/app/services/loadbalancer.py:39` | Proxy redirect target lookup is by `redirect_to` model id only | Alias resolution can cross profile boundaries |
| `backend/app/routers/proxy.py:119` | Proxy path resolves model via global lookup call | Request handling has no profile context input |

### 2.3 Config and settings mutation are global/destructive

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `backend/app/routers/config.py:397` to `backend/app/routers/config.py:401` | Import deletes all FX mappings, connections, endpoints, models, providers | Import can wipe unrelated profile data |
| `backend/app/routers/settings.py:25` | Costing settings fetches first `UserSetting` row (singleton behavior) | Costing preferences are global |
| `backend/app/routers/settings.py:103` | Costing update deletes all `EndpointFxRateSetting` rows before insert | Updating one profile would overwrite all profiles |

### 2.4 Observability and audit lack profile attribution

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `backend/app/models/models.py:163` to `backend/app/models/models.py:237` | `RequestLog` has no `profile_id` field | Historical logs cannot be partitioned by profile |
| `backend/app/services/stats_service.py:61` | `log_request()` writes request rows without profile context | Runtime attribution loss |
| `backend/app/models/models.py:298` to `backend/app/models/models.py:330` | `AuditLog` has no `profile_id` field | Audit records cannot prove profile origin |
| `backend/app/services/audit_service.py:90` | `record_audit_log()` writes audit rows without profile context | Cross-profile audit ambiguity |

### 2.5 In-memory failover state is globally keyed

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `backend/app/services/loadbalancer.py:11` | `_recovery_state` keyed by `connection_id` only | Cooldown/recovery can leak across profiles |
| `backend/app/services/loadbalancer.py:93` | Reads state by `connection.id` only | No runtime profile namespace for health memory |
| `backend/app/services/loadbalancer.py:113` | Writes state by `connection_id` only | Profile switching can inherit stale state |

### 2.6 Frontend request pipeline is single-context

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `frontend/src/lib/api.ts:42` to `frontend/src/lib/api.ts:48` | Shared `request<T>()` helper injects content-type only | No profile header/query propagation |
| `frontend/src/components/layout/AppLayout.tsx:77` | Sidebar footer shows version only (`v1.0`) | No active profile indicator/selector in shell |
| `frontend/src/App.tsx:43` to `frontend/src/App.tsx:53` | All routes mounted under one static layout | No profile context provider in app root |

### 2.7 Migration mechanism evidence (authoritative path)

| Evidence | Current behavior | Isolation implication |
|---|---|---|
| `backend/app/main.py:147` to `backend/app/main.py:159` | Startup runs DB migrations before serving requests | Profile schema rollout should be Alembic-first |
| `backend/app/core/migrations.py:17` to `backend/app/core/migrations.py:19` | Executes `alembic upgrade head` programmatically | Backfill and constraint phases should be migration revisions |
| `backend/app/core/config.py:5` and `backend/app/core/config.py:21` | DB URL defaults to PostgreSQL; non-PostgreSQL rejected | Docs should align with PostgreSQL reality |

## 3. Risk Matrix and Guardrails

| Risk ID | Risk | Evidence | Required guardrail | Validation signal |
|---|---|---|---|---|
| R1 | Cross-profile routing leakage | Global model lookup (`loadbalancer.py:23`) | Every model/alias query includes profile scope | Same model id in A/B routes only within active profile |
| R2 | Cross-profile config destruction | Import global deletes (`config.py:397-401`) | Import must target one profile only | Import A does not change B/C counts |
| R3 | Costing configuration overwrite | Global FX replacement (`settings.py:103`) | Profile-scoped settings and mappings | Updating A costing leaves B/C unchanged |
| R4 | Failover state contamination | `_recovery_state` global key (`loadbalancer.py:11`) | Namespace memory by profile context | Fail in A does not cooldown B |
| R5 | Untraceable historical events | No profile in logs/audit (`models.py`) | Immutable `profile_id` stamp at write time | Historical rows retain original profile after switches |
| R6 | Frontend context mismatch | No profile propagation (`api.ts:42-48`) | Central request-level profile propagation | Every API call carries effective profile context |
| R7 | Migration risk to live installs | Existing global schema | Phased migration with backfill then constraints | Existing installs boot with default profile and no data loss |
| R8 | Documentation drift | Docs still describe SQLite/global behavior | Document consistency updates before implementation | PRD/Architecture/Data Model references aligned with backend |

## 4. Acceptance Validation Matrix (Docs-Level)

| Requirement area | Validation scenario | Evidence endpoint/surface |
|---|---|---|
| Isolation core | Same `model_id` exists in A/B/C without collision | Model uniqueness constraints become profile composite |
| Runtime routing | Active profile A only resolves A mappings | `/v1/*` request path and model resolution flow |
| Import safety | Import into profile A does not mutate B/C | `/api/config/import` behavior and post-import row checks |
| Costing safety | Profile A currency/FX update does not alter B/C | `/api/settings/costing` read/write checks |
| Observability integrity | Request and audit rows include immutable profile attribution | `/api/stats/*`, `/api/audit/*` output fields |
| Frontend behavior | Profile selector visible; switching refreshes scoped data | `AppLayout`, `api.ts` propagation, page reload/refetch behavior |
| Runtime memory isolation | Failover cooldown state does not leak across profiles | Failover scenarios in `/v1/*` under profile switch |

## 5. Phased Rollout and Validation Checklist

### Phase 1: Schema and backfill (no behavioral change)

- Introduce profile entity and profile key columns on scoped tables
- Backfill existing rows into default profile
- Keep old behavior equivalent via default active profile
- Validate migration idempotency on restart

### Phase 2: Runtime routing scoping

- Scope model and alias resolution by active profile
- Scope connection selection and failover memory by profile
- Validate same model id in A/B/C resolves correctly under active profile switch

### Phase 3: Management API scoping

- Scope model/endpoint/connection/settings/config endpoints
- Add explicit profile-target import/export semantics
- Validate no cross-profile writes unless explicitly privileged

### Phase 4: Observability and reporting scoping

- Stamp `request_logs` and `audit_logs` with immutable profile attribution
- Default stats/audit queries to active profile
- Validate historical attribution remains unchanged after profile switches

### Phase 5: Frontend context and UX

- Add global profile selector and active profile indicator
- Propagate profile context through API client layer
- Trigger data refresh on profile switch across pages

### Release Gate Checklist

- Isolation acceptance scenarios pass for A/B/C
- No global destructive behavior remains for scoped operations
- Observability queries and deletions are profile-safe
- Backward compatibility validated on pre-profile data set
- Documentation updates merged for PRD/Architecture/Data Model alignment

## 6. Consistency Pass Findings (PRD, Architecture, Data Model)

### 6.1 PRD mismatches

| File evidence | Finding | Recommended doc update |
|---|---|---|
| `docs/PRD.md:109`, `docs/PRD.md:207`, `docs/PRD.md:217` | PRD states SQLite; backend runtime enforces PostgreSQL (`backend/app/core/config.py:5`, `backend/app/core/config.py:21`) | Update database/storage sections to PostgreSQL + Alembic startup migration behavior |
| `docs/PRD.md:42` | PRD states model ids are globally unique | Add profile-isolated uniqueness model (`profile_id + model_id`) for profile feature scope |
| `docs/PRD.md:55`, `docs/PRD.md:102` | PRD describes global endpoints | Clarify endpoints become profile-scoped in profile isolation mode |
| `docs/PRD.md:17`, `docs/PRD.md:224` | Single-user/non-multi-tenancy positioning remains true, but profile feature introduces namespace isolation | Clarify this is not auth multi-tenancy; it is config namespace isolation for one operator |

### 6.2 Architecture mismatches

| File evidence | Finding | Recommended doc update |
|---|---|---|
| `docs/ARCHITECTURE.md:14` | Diagram still labels SQLite database | Replace with PostgreSQL data store representation |
| `docs/ARCHITECTURE.md:65` | Says `alembic/ (future)` | Update to current-state: Alembic is active and startup-applied |
| `docs/ARCHITECTURE.md:418`, `docs/ARCHITECTURE.md:435`, `docs/ARCHITECTURE.md:438` | SQLite operational notes remain | Replace with PostgreSQL operational notes |
| `docs/ARCHITECTURE.md:219` | Global model-id uniqueness documented | Add profile-scoped uniqueness for profile feature |

### 6.3 Data Model gaps

| File evidence | Finding | Recommended doc update |
|---|---|---|
| `docs/DATA_MODEL.md:128`, `docs/DATA_MODEL.md:141` | Model ID documented as globally unique | Introduce profile composite uniqueness and profile FK |
| `docs/DATA_MODEL.md:150`, `docs/DATA_MODEL.md:214` | Endpoint/settings documented as global | Add profile-scoped ownership fields and constraints |
| `docs/DATA_MODEL.md:7`, `docs/DATA_MODEL.md:73` | ERD has no profile entity or profile attribution in logs | Extend ERD with profile table and profile keys on scoped entities |
| `docs/DATA_MODEL.md:239`, `docs/DATA_MODEL.md:253` | FX uniqueness/indexing is global | Update to profile-composite FX uniqueness/indexing |

## 7. Recommended Next Documentation Updates

1. Add profile-aware schema and ERD extensions to `docs/DATA_MODEL.md`.
2. Update runtime flow diagrams in `docs/ARCHITECTURE.md` to show active profile context in request resolution.
3. Update `docs/PRD.md` language from global mappings to profile-isolated mappings (feature-scoped).
4. Add profile isolation smoke section to `docs/SMOKE_TEST_PLAN.md` (new scenarios for A/B/C boundaries).

---

This document captures evidence and validation planning only. No application code or runtime behavior has been modified.