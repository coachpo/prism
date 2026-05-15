# BACKEND MANAGEMENT SIDECARS KNOWLEDGE BASE

## OVERVIEW
`management/sidecars/` owns the global `/api/sidecars/*` control plane for CLIProxyAPI sidecar instances. Prism stores sidecar registrations, observations, watchdog state, and action history; CLIProxyAPI remains the source of truth for live auth/provider state.

## STRUCTURE
```text
sidecars/
├── service.go              # Service construction, route mounting, memory fallback store
├── routes.go               # CRUD, connection test, response shaping, credential masks
├── routes_sync.go          # Manual sync, auth/provider snapshot reads, sync status
├── routes_mutations.go     # Operator auth-file status/field patches
├── routes_watchdog.go      # Watchdog policy and action-history endpoints
├── client.go               # CLIProxyAPI management client and network policy gates
├── providers.go            # Provider inventory normalization
├── sync.go                 # Sync orchestration and management-auth pause state
├── watchdog.go             # Quota/failure watchdog reconciliation
├── actions.go              # Redacted operator/watchdog action recording
├── store.go                # PostgreSQL persistence
├── store_memory_sync.go    # Test/fallback in-memory persistence
├── types.go                # Domain types, defaults, error codes
└── worker.go               # Low-priority sync and watchdog workers
```

## WHERE TO LOOK
- Route list and global mount contract: `service.go` (`MountManagementRoutes`).
- Instance CRUD, masked credential state, and connection test: `routes.go`.
- Snapshot sync and status payloads: `routes_sync.go`, `sync.go`, `providers.go`.
- Auth-file operator mutations and redacted audit trail: `routes_mutations.go`, `actions.go`.
- Watchdog policy, parent sweep, child sweep items, hold, restore/deprioritize, and action history: `routes_watchdog.go`, `watchdog.go`.
- CLIProxyAPI network/auth policy and supported management paths: `client.go`, `cliproxy_contract_test.go`.
- Durable tables and uniqueness constraints: `../../../../migrations/000014_cli_proxy_sidecars.sql`, `../../../../migrations/000020_sidecar_watchdog_policy_revisions_and_sweeps.sql`, and `../../../../migrations/000022_sidecar_watchdog_sweep_items.sql`.
- Lifecycle wiring and worker priority: `../../../platform/lifecycle/production.go`, `worker.go`, `../../../../tests/priority/sidecar_worker_priority_test.go`.
- Regression coverage: `routes*_test.go`, `sync_test.go`, `watchdog*_test.go`, `store_test.go`, `client_test.go`, `cliproxy_contract_test.go`, `../../../../tests/{contract,integration}/sidecars*_test.go`.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- Treat sidecar management as global instance control-plane state; it does not use selected-profile `X-Profile-Id` scope.
- Keep management passwords write-only. Responses expose `credential_state` metadata and the mask string only.
- Keep live auth/provider inventory owned by CLIProxyAPI; Prism persists normalized snapshots and watchdog/action state.
- Keep the CLIProxyAPI management path allowlist tight: `/auth-files`, `/auth-files/status`, `/auth-files/fields`, and the five provider inventory paths.
- Treat `/auth-files` as a strict top-level `files` envelope; old `auth_files`, missing `files`, null `files`, or non-array `files` payloads fail closed.
- Keep provider inventory as a separate read-only supplement, never as an auth-snapshot fallback.
- Keep private-network, insecure-HTTP, TLS-skip, request-timeout, and management-auth pause behavior on the sidecar instance policy.
- Keep one authoritative running or paused parent sweep per sidecar, with mandatory `sidecar_watchdog_sweep_items` child rows as the executable work source.
- Keep `active_sweep.progress` as the route/UI projection for child item status counts; do not expose cursor, batch, lease, or restart internals as the operator progress contract.
- Keep policy save, apply, and apply-and-restart semantics distinct: save writes pending revisions, apply affects future sweeps only, and apply-and-restart supersedes active parent and child work.
- Keep `sidecar_quota_scan_runs` projection/history only. Initial inventory, manual scan, rolling refresh, and due-hold probe work must execute through child sweep items, not active quota-scan rows.
- Keep `sidecar_snapshot_sync` and `sidecar_watchdog_reconcile` as bounded low-background workers with queue limit 1 and no priority elevation.

## ANTI-PATTERNS
- Do not let the browser call CLIProxyAPI directly; all sidecar traffic goes through the Prism backend service.
- Do not persist or return raw management passwords, provider secrets, auth tokens, action payload secrets, or unredacted snapshot fields.
- Do not add destructive or unsupported CLIProxyAPI management paths such as `/usage-queue` to the allowlist.
- Do not accept legacy `auth_files` envelopes or hide auth-sync contract violations behind provider inventory fallback.
- Do not make sidecar workers borrow runtime/management request-path capacity or run above low-background priority.
