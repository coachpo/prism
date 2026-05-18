# BACKEND MANAGEMENT SIDECARS KNOWLEDGE BASE

## OVERVIEW
`management/sidecars/` owns the global `/api/sidecars/*` control plane for CLIProxyAPI sidecar instances. Prism stores sidecar registrations and normalized auth/provider observations; CLIProxyAPI remains the source of truth for live auth/provider state.

## STRUCTURE
```text
sidecars/
├── service.go              # Service construction, route mounting, memory fallback store
├── routes.go               # CRUD, connection test, response shaping, credential masks
├── routes_sync.go          # Manual sync, auth/provider snapshot reads, sync status
├── routes_mutations.go     # Operator auth-file status/field patches
├── client.go               # CLIProxyAPI management client and network policy gates
├── providers.go            # Provider inventory normalization
├── sync.go                 # Sync orchestration and management-auth pause state
├── store.go                # PostgreSQL persistence
├── store_memory_sync.go    # Test/fallback in-memory persistence
├── types.go                # Domain types, defaults, error codes
└── worker.go               # Low-priority sync worker
```

## WHERE TO LOOK
- Route list and global mount contract: `service.go` (`MountManagementRoutes`).
- Instance CRUD, masked credential state, and connection test: `routes.go`.
- Snapshot sync and status payloads: `routes_sync.go`, `sync.go`, `providers.go`.
- Auth-file operator mutations: `routes_mutations.go`.
- CLIProxyAPI network/auth policy and supported management paths: `client.go`, `cliproxy_contract_test.go`.
- Durable sidecar tables and uniqueness constraints: `../../../../migrations/000014_cli_proxy_sidecars.sql`.
- Lifecycle wiring and worker priority: `../../../platform/lifecycle/production.go`, `worker.go`, `../../../../tests/priority/sidecar_worker_priority_test.go`.
- Regression coverage: `routes*_test.go`, `routes_removed_surfaces_test.go`, `sync_test.go`, `store_test.go`, `client_test.go`, `cliproxy_contract_test.go`, `../../../../tests/{contract,integration}/sidecars*_test.go`.

## CONVENTIONS

- When doing upgrade work, prefer clean architecture and the best current implementation over backward-compatibility shims; this project is still under development and has no users, so preserve legacy shapes only when explicitly requested.
- For ordinary removal-only validation here, prefer manual confirmation over adding dedicated “proves not” tests unless the missing surface is itself a shipped contract or guardrail.
- Treat sidecar management as global instance control-plane state; it does not use selected-profile `X-Profile-Id` scope.
- Keep management passwords write-only. Responses expose `credential_state` metadata and the mask string only.
- Keep live auth/provider inventory owned by CLIProxyAPI; Prism persists normalized snapshots for operator display.
- Keep the CLIProxyAPI management path allowlist tight: `/auth-files`, `/auth-files/status`, `/auth-files/fields`, and the five provider inventory paths.
- Treat `/auth-files` as a strict top-level `files` envelope; old `auth_files`, missing `files`, null `files`, or non-array `files` payloads fail closed.
- Keep provider inventory as a separate read-only supplement, never as an auth-snapshot fallback.
- Keep private-network, insecure-HTTP, TLS-skip, request-timeout, and management-auth pause behavior on the sidecar instance policy.
- Keep `sidecar_snapshot_sync` as a bounded low-background worker with queue limit 1 and no priority elevation.

## ANTI-PATTERNS
- Do not let the browser call CLIProxyAPI directly; all sidecar traffic goes through the Prism backend service.
- Do not persist or return raw management passwords, provider secrets, auth tokens, or unredacted snapshot fields.
- Do not add destructive or unsupported CLIProxyAPI management paths such as `/usage-queue` to the allowlist.
- Do not accept legacy `auth_files` envelopes or hide auth-sync contract violations behind provider inventory fallback.
- Do not make sidecar workers borrow runtime/management request-path capacity or run above low-background priority.
