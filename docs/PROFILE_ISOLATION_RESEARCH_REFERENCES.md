# Profile Isolation Research References

## Document Metadata

- Status: Draft (analysis-only)
- Date: 2026-02-28
- Scope: External references supporting profile/workspace isolation, safe active-context switching, and scoped observability
- Related docs:
  - `docs/PROFILE_ISOLATION_REQUIREMENTS.md`
  - `docs/PROFILE_ISOLATION_SUPPORTING_EVIDENCE.md`

## 1. Purpose

This document curates implementation-oriented references for designing isolated config profiles (A/B/C) with one active profile serving traffic at a time.

## 2. Reference Set

### 2.1 Isolation models (workspace/namespace/org)

| Ref | Source | URL | Practical takeaway for Prism |
|---|---|---|---|
| I-01 | Kong Workspaces | https://developer.konghq.com/gateway/entities/workspace/ | Scope config entities by workspace-like key; isolation is a first-class namespace concept |
| I-02 | Kong decK workspaces | https://developer.konghq.com/deck/gateway/workspaces/ | Config sync/import operations should target explicit workspace/profile context |
| I-03 | Kong migration code (`ws_id`) | https://github.com/Kong/kong/blob/master/kong/db/migrations/operations/200_to_210.lua | Namespace key should exist at persistence layer, not only API layer |
| I-04 | Vault Namespaces | https://developer.hashicorp.com/vault/docs/enterprise/namespaces | Strong namespace boundary model for admin operations and state isolation |
| I-05 | Vault Namespace API | https://developer.hashicorp.com/vault/api-docs/system/namespaces | Profile lifecycle should have dedicated CRUD and explicit scope semantics |
| I-06 | Grafana org management | https://grafana.com/docs/grafana/latest/administration/organization-management/ | Active context switching can be operator-friendly while keeping scoped resources |
| I-07 | Grafana user/org API | https://grafana.com/docs/grafana/latest/developers/http_api/user/ | Expose explicit active-context switch endpoint semantics |
| I-08 | LiteLLM access control | https://docs.litellm.ai/docs/proxy/access_control | Logical isolation can be enforced without introducing auth redesign in first phase |
| I-09 | LiteLLM model access guide | https://docs.litellm.ai/docs/proxy/model_access_guide | Model visibility/access is a boundary primitive that maps to profile-scoped routing |
| I-10 | Tyk user management | https://tyk.io/docs/api-management/user-management/ | Organization-scoped governance patterns map to profile-scoped operations |

### 2.2 Active profile switching safety

| Ref | Source | URL | Practical takeaway for Prism |
|---|---|---|---|
| S-01 | SQLite transactions | https://www.sqlite.org/lang_transaction.html | Use transactionally safe activation updates; avoid partial switch state |
| S-02 | SQLite busy timeout API | https://www.sqlite.org/c3ref/busy_timeout.html | Handle activation contention with bounded retry/backoff semantics |
| S-03 | SQLite `PRAGMA busy_timeout` | https://www.sqlite.org/pragma.html#pragma_busy_timeout | Ensure lock contention behavior is deterministic under concurrent updates |
| S-04 | etcd transactional write guide | https://etcd.io/docs/v3.6/tasks/developer/how-to-transactional-write/ | CAS-style update pattern for expected-version activation writes |
| S-05 | etcd API txn model | https://etcd.io/docs/v3.6/learning/api/ | Use compare-and-swap guard to prevent switch races |
| S-06 | etcd optimistic put code | https://github.com/etcd-io/etcd/blob/main/client/v3/kubernetes/client.go | Real-world CAS implementation pattern (`compare -> put`) for active state updates |
| S-07 | Envoy xDS protocol | https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol | Treat config activation as versioned state transition with ACK/NACK style semantics |
| S-08 | go-control-plane snapshot code | https://github.com/envoyproxy/go-control-plane/blob/main/pkg/cache/v3/simple.go | Build immutable snapshot first, then atomically swap active runtime pointer |
| S-09 | go-control-plane snapshot consistency | https://github.com/envoyproxy/go-control-plane/blob/main/pkg/cache/v3/snapshot.go | Validate snapshot consistency before activation to avoid serving partial config |

### 2.3 Scoped observability and immutable attribution

| Ref | Source | URL | Practical takeaway for Prism |
|---|---|---|---|
| O-01 | Loki multi-tenancy | https://grafana.com/docs/loki/latest/operations/multi-tenancy/ | Stamp tenant/profile context at ingest and default queries to scoped context |
| O-02 | Loki client header stamping code | https://github.com/grafana/loki/blob/main/integration/client/client.go | Context header propagation should be explicit and centralized |
| O-03 | Loki header normalization code | https://github.com/grafana/loki/blob/main/pkg/ruler/registry.go | Server should normalize/override untrusted context headers |
| O-04 | Kubernetes audit API | https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/ | Immutable event attribution fields are required for forensic correctness |
| O-05 | Kubernetes audit type definitions | https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiserver/pkg/apis/audit/v1/types.go | Persist request-level identity/context at write time, not derived at read time |

## 3. Cross-Reference to Prism Requirement Decisions

- FR-002, FR-003, FR-006 (scoped data and runtime isolation): I-01 to I-09
- FR-004 (safe active switch): S-01 to S-09
- FR-009 (observability and immutable attribution): O-01 to O-05
- FR-007 (import/export isolation): I-02 + S-07/S-09 patterns for explicit target + safe activation

## 4. Usage Notes for Implementation Planning

- Prioritize first-party docs as normative behavior references.
- Use code references as evidence for operational patterns (CAS, snapshot swap, context stamping).
- Treat these references as design constraints, not direct feature parity requirements.

---

This reference set supports requirements and feasibility analysis only. No code changes are included in this document.