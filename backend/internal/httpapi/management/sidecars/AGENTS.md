# BACKEND MANAGEMENT SIDECARS KNOWLEDGE BASE

## OVERVIEW
`management/sidecars/` owns the global `/api/sidecars*` control plane for CLIProxyAPI sidecar instances. It covers instance CRUD, connection tests, manual sync, live auth-file reads and mutations, provider inventory, persisted provider snapshots, and sync status. Prism stores registrations and optional normalized observations; CLIProxyAPI remains the source of truth for live auth/provider state.

## STRUCTURE
```text
sidecars/
├── service.go              # Service construction, global route mounting, memory fallback store
├── routes.go               # Instance CRUD, connection test, response shaping, credential masks
├── routes_sync.go          # Manual sync, auth-file reads, auth-file models, provider reads, sync status
├── routes_mutations.go     # Auth-file status, fields, and delete mutations
├── client.go               # CLIProxyAPI management client and network policy gates
├── providers.go            # Provider inventory normalization
├── sync.go                 # Sync orchestration and management-auth pause state
├── store.go                # PostgreSQL persistence
├── store_memory_sync.go    # Test/fallback in-memory persistence
├── types.go                # Domain types, defaults, error codes
└── worker.go               # Low-priority sync worker
```

## WHERE TO LOOK
- Route list and global mount contract: `service.go`.
- Instance CRUD, masked credential state, and connection test: `routes.go`.
- Manual sync, live auth-file reads, provider inventory, provider snapshots, and sync status: `routes_sync.go`, `sync.go`, `providers.go`.
- Auth-file status, field, and delete mutations: `routes_mutations.go`.
- Strict CLIProxyAPI path allowlist and envelope contract: `client.go`, `cliproxy_contract_test.go`, `routes_removed_surfaces_test.go`.
- Durable sidecar tables and worker wiring: `store.go`, `worker.go`, `../../../platform/lifecycle/production.go`.

## CONVENTIONS
- Treat sidecar management as global instance control-plane state; it does not use selected-profile `X-Profile-Id` scope.
- Keep management passwords write-only. Responses expose `credential_state` metadata and the mask string only.
- Accept live `/auth-files` only as a strict top-level `files` array envelope. Missing, null, non-array, or legacy `auth_files` envelopes fail closed.
- Keep removed legacy auth inventory paths removed; provider inventory is a read-only supplement, never an auth-file fallback.
- Keep the CLIProxyAPI allowlist tight: `/auth-files`, `/auth-files/models`, `/auth-files/status`, `/auth-files/fields`, and provider inventory paths only.
- Keep `sidecar_snapshot_sync` as a bounded low-background worker with queue limit 1 and no priority elevation.

- Prefer steady-state Prism configuration in the plaintext startup config JSON instead of adding new environment-variable knobs. Keep env vars limited to bootstrap-critical startup inputs or process wiring such as `PRISM_CONFIG_PATH`, `DATABASE_URL`, launcher proxy wiring, build metadata, container ports, or test flags.

## LLM UPSTREAM MATRIX
- When sidecar auth or provider inventory logic changes, check CLIProxyAPI observations across provider families without treating inventory as runtime compatibility.

## ANTI-PATTERNS
- Do not let the browser call CLIProxyAPI directly; all sidecar traffic goes through the Prism backend service.
- Do not persist or return raw management passwords, provider secrets, auth tokens, or unredacted provider observation fields.
- Do not accept legacy `auth_files` envelopes or hide auth-sync contract violations behind provider inventory fallback.
- Do not reintroduce removed legacy auth inventory routes; keep live auth-file reads on the strict `files` envelope.
- Do not add destructive or unsupported CLIProxyAPI management paths such as `/usage-queue` to the allowlist.
- Do not make sidecar workers borrow runtime/management request-path capacity or run above low-background priority.
