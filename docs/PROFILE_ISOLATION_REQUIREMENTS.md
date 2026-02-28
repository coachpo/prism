# Profile Isolation Requirements (Config Profiles A/B/C)

## Document Metadata

- Status: Draft (analysis-only, no code changes)
- Date: 2026-02-28
- Scope: Backend + Frontend requirements for isolated config profiles
- Primary user intent: one active profile serves traffic; inactive profiles are fully isolated

## 1. Purpose

Define product and system requirements for introducing multiple isolated config profiles in Prism so users can keep standalone model-to-connection mappings and switch active runtime context safely.

## 2. User Story and Expected Behavior

Target story:

- User has 3 profiles: A, B, C.
- Profile A contains OpenAI models/connections.
- Profile B contains Anthropic models/connections.
- Profile C contains Gemini models/connections.
- When A is active, only A mappings are routable.
- When B is active, only B mappings are routable.
- When C is active, only C mappings are routable.
- Profile data must not disturb other profiles (similar to OS user-profile isolation).

## 3. Current-State Constraints (Observed)

Current implementation is single-namespace and globally scoped:

- `ModelConfig.model_id` is globally unique in `backend/app/models/models.py:47`.
- `Endpoint.name` is globally unique in `backend/app/models/models.py:80`.
- Proxy runtime resolves model globally in `backend/app/services/loadbalancer.py:23`.
- Proxy handler calls model lookup without profile context in `backend/app/routers/proxy.py:119`.
- Config import deletes global config data in `backend/app/routers/config.py:397` to `backend/app/routers/config.py:401`.
- Costing settings replace global FX mappings in `backend/app/routers/settings.py:103`.
- Request logs and audit logs do not include profile context in `backend/app/models/models.py:163` and `backend/app/models/models.py:298`.
- In-memory failover recovery state is keyed only by connection id in `backend/app/services/loadbalancer.py:11`.

## 4. Goals

- Provide strict isolation boundary between profiles for routing-related configuration.
- Support active profile switching with transactional safety.
- Preserve observability attribution per profile for historical analysis.
- Minimize disruption to existing API/UI usage patterns where possible.
- Keep backward compatibility through phased rollout and default profile backfill.

## 5. Non-Goals (This Phase)

- Multi-user authentication and RBAC redesign.
- Public-internet security hardening.
- Cross-profile blended routing in a single request.
- Full data-plane process isolation per profile.

## 6. Definitions

- Profile: named namespace containing a standalone routing configuration.
- Active profile: the profile currently used for proxy routing by default.
- Selected profile: the profile currently targeted by management APIs and UI operations.
- Isolation: reads/writes for scoped entities are constrained to one profile unless explicit privileged cross-profile operation is invoked.
- Scoped entity: DB object that must carry profile context.

## 7. Functional Requirements

### FR-001 Profile Entity and Lifecycle

System must support profile CRUD and activation state.

- Must support creating, listing, updating, deleting profiles.
- Must enforce exactly one active profile at a time.
- Must reject deletion of the currently active profile.
- Must support setting a default profile for initial migrations.
- Default delete behavior must be soft-delete for inactive profiles to preserve historical attribution links.
- Must enforce a maximum of 10 non-deleted profiles (`deleted_at IS NULL`).
- If the 10-profile limit is reached, create-profile must fail until user deletes a profile.

Minimum profile fields:

- `id`, `name`, `description` (optional)
- `is_active`
- `version` (for optimistic switch control)
- `deleted_at` (nullable; set on routine inactive-profile deletion)
- `created_at`, `updated_at`

### FR-002 Scoped Data Model

System must scope routing configuration by profile.

At minimum, these entities must be profile-scoped:

- models (`model_configs`)
- endpoints (`endpoints`)
- connections (`connections`)
- endpoint FX mappings (`endpoint_fx_rate_settings`)
- user costing/settings (currently singleton row)
- request logs and audit logs (attribution scope)

Recommended DB constraints:

- Unique `(profile_id, model_id)` instead of global `model_id` unique.
- Unique `(profile_id, name)` for endpoint names.
- Unique `(profile_id, model_id, endpoint_id)` for FX mappings.

### FR-003 Proxy Runtime Isolation

Proxy routing must use active profile context for all request decisions.

- Model resolution must filter by profile.
- Proxy alias (`redirect_to`) resolution must stay within same profile.
- Endpoint and connection selection must be profile-scoped.
- If model exists only in another profile, return not found for active profile context.

### FR-004 Active Profile Switch Safety

Profile switch must be atomic and conflict-safe.

- Must perform switch in a single transaction.
- Must use optimistic guard (expected version/CAS-style check).
- Must fail safely on concurrent switch conflict.
- Must provide deterministic rollback path.

### FR-005 In-Memory State Isolation

Failover/recovery memory must not leak across profiles.

- `_recovery_state` behavior in `backend/app/services/loadbalancer.py` must be profile-aware.
- Recovery entries must be namespaced by profile context.
- Switching profile must not inherit stale cooldown from another profile unless explicitly intended and safe.

### FR-006 API Scope Semantics

Management APIs must support profile-aware operations.

- Existing CRUD endpoints should default to active profile if no explicit profile override is supplied.
- API should support explicit profile override for administrative operations.
- Cross-profile reads/writes must be explicit and protected (future auth policy dependent).
- Profile-scoped routes must resolve effective profile through one shared dependency/mechanism.
- Detail endpoints (`GET /.../{id}`) must return `404` when resource exists in another profile.
- Profile list/create semantics must exclude soft-deleted profiles from active capacity counting.

### FR-007 Config Export/Import Isolation

Config operations must avoid unintended cross-profile mutation.

- Export must support profile-targeted output.
- Import must support explicit target profile and mode:
  - replace target profile only
  - merge into target profile (optional future)
- Import must not globally delete other profile data by default.
- Config format version must be incremented to include profile metadata.
- Config v7 must be ID-agnostic for scoped resources:
  - endpoint and connection references use logical keys (for example `endpoint_ref`) rather than assuming target DB PK identity
  - server performs source-to-target remap during import
  - v6 import remains supported through compatibility translation of legacy IDs into logical references

### FR-008 Costing and Settings Isolation

Costing inputs and settings must be profile-scoped.

- Report currency and symbol must be profile-aware.
- FX mappings must be validated within profile-bound model/endpoint pairs.
- Cost computations must load profile-scoped settings snapshot.

### FR-009 Observability and Audit Attribution

All logs must carry immutable profile attribution.

- `request_logs` must include profile context.
- `audit_logs` must include profile context.
- Stats/audit queries must default to active profile filter.
- Historical rows must keep original profile attribution even after profile switch.

### FR-010 Frontend UX Requirements

UI must expose selected and active profile context distinctly and propagate selected profile context to management APIs.

- Add profile selector in top-level layout (`frontend/src/components/layout/AppLayout.tsx`).
- Show active profile indicator globally.
- Update API request layer (`frontend/src/lib/api.ts`) to attach profile context.
- On selected profile switch, pages must refresh/reload scoped management data.
- Active profile changes require explicit operator action (activate) and must not occur implicitly on selector change.

### FR-011 Migration and Backward Compatibility

Rollout must preserve existing installations.

- Create default profile and backfill all existing data to it.
- Keep previous behavior equivalent under default profile until migration completes.
- Apply schema changes via Alembic migration chain (`backend/app/core/migrations.py:17`, `backend/alembic/env.py`).

## 8. Non-Functional Requirements

- Isolation correctness over convenience: no cross-profile leakage in routing decisions.
- Switch operation should be short-lived transaction with clear operator feedback on conflicts.
- Profile switch must not corrupt in-flight request handling.
- Logging and audit writes remain best-effort and non-blocking to client path.
- Existing route latency should not regress materially from profile filtering.

## 9. Acceptance Criteria

### A. Core Isolation

- Same `model_id` can exist in multiple profiles without collision.
- Active profile routes only to its own models/connections/endpoints.
- Proxy alias cannot resolve target from another profile.
- At most 10 non-deleted profiles can exist at once.

### B. Data Safety

- Importing profile A config does not delete profile B or C data.
- Updating costing settings in one profile does not modify another profile.
- Deleting logs in one profile does not delete another profile's logs.

### C. Runtime Consistency

- Switching profile is atomic and conflict-safe.
- In-memory failover state is profile-isolated.
- In-flight requests complete with consistent profile context captured at request start.

### D. Observability Integrity

- Every request and audit row includes profile attribution.
- Stats and audit APIs default to active profile filtering.
- Historical attribution remains immutable after future profile switches.

### E. Frontend Behavior

- Profile selector is visible in app shell.
- Switching selected profile refreshes all profile-scoped management views.
- Switching selected profile does not change active runtime profile until explicit activate succeeds.
- API requests include profile context consistently.
- When 10 non-deleted profiles already exist, create-profile shows a clear delete-before-create instruction.

## 10. Phased Delivery Plan (Requirement-Level)

- Phase 1: Schema additions + default profile backfill (no behavior change).
- Phase 2: Proxy runtime scoping to active profile.
- Phase 3: Management API scoping and profile-specific import/export semantics.
- Phase 4: Logging/audit/stats profile attribution and filtering.
- Phase 5: Frontend selector + active-profile UX + refresh behavior.

## 11. Key Risks and Mitigations

- Risk: Cross-profile leakage due to missed query filters.
  - Mitigation: enforce profile_id in service/repository boundary and add targeted tests.
- Risk: Switch races causing inconsistent active state.
  - Mitigation: optimistic version guard + transactional switch.
- Risk: Historical reporting ambiguity.
  - Mitigation: immutable profile stamping at write time for request/audit logs.
- Risk: Legacy import behavior wiping global data.
  - Mitigation: profile-targeted import modes and explicit operator intent.

## 12. Resolved Decisions

- Providers remain global seed records/shared references in this phase.
- Header blocklist is split: system rules remain global; user rules are profile-scoped.
- Active profile is global server state in this phase (not user/session-specific).
- Explicit management override header is `X-Profile-Id`.

## 13. Traceability to Existing System

Primary impacted areas:

- Backend models: `backend/app/models/models.py`
- Proxy runtime: `backend/app/routers/proxy.py`, `backend/app/services/loadbalancer.py`
- Config and settings: `backend/app/routers/config.py`, `backend/app/routers/settings.py`, `backend/app/services/costing_service.py`
- Observability: `backend/app/services/stats_service.py`, `backend/app/services/audit_service.py`, `backend/app/routers/stats.py`, `backend/app/routers/audit.py`
- Frontend request/UX: `frontend/src/lib/api.ts`, `frontend/src/components/layout/AppLayout.tsx`, core pages under `frontend/src/pages/`

---

This document is requirements-only and intentionally excludes implementation code changes.
