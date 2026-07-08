# BACKEND PLATFORM HTTP KNOWLEDGE BASE

## OVERVIEW
`platform/http/` owns backend HTTP assembly: mux construction, `/health`, `/api`, `/v1`, `/v1beta`, management mutation middleware, body limits, startup runtime snapshots, telemetry startup config, and runtime-cache invalidation wiring.

## STRUCTURE
```text
http/
├── server.go                         # Top-level server and route mounting
├── management_branch.go              # `/health` and `/api` branch wiring
├── runtime_branch.go                 # `/v1` and `/v1beta` branch wiring
├── dependencies.go                   # HTTP dependency bundle
├── hot_bootstrap_runtime.go          # Hot-applied config snapshots
├── runtime_cache_invalidation.go     # Management mutation invalidation hooks
├── management_body_limits.go         # Management request body limits
├── telemetry.go                      # Startup-config OTLP helpers
└── management_route_contract.json    # Profile-scope and invalidation contract
```

## WHERE TO LOOK
- Server assembly and exact mounted branches: `server.go`, `management_branch.go`, `runtime_branch.go`
- Startup runtime snapshots for CORS, auth, runtime proxy transport, and admission: `hot_bootstrap_runtime.go`
- Runtime cache invalidation after management mutations: `runtime_cache_invalidation.go`, `management_route_contract.json`
- Shared body-size enforcement: `management_body_limits.go`, `../bodylimits/`
- Startup-config telemetry provider wiring: `telemetry.go`, `../telemetry/`
- Runtime operation allowlist after `/v1` mount: `../../httpapi/runtime/operations.go`

## CONVENTIONS
- Keep mounting here and handler behavior in `../../httpapi/`.
- Keep `/v1` and `/v1beta` as mounted prefixes only; supported runtime operations remain allowlisted in `../../httpapi/runtime/operations.go`.
- Keep management profile-scope and runtime-cache invalidation changes reflected in `management_route_contract.json`.
- Keep startup bootstrap state behind `HotBootstrapConfigRuntime`; direct file edits are not watched state and require restart.
- Keep request body limits centralized through this package and `../bodylimits/`.

## ANTI-PATTERNS
- Do not add management or runtime business logic to platform HTTP assembly.
- Do not duplicate runtime operation matching here.
- Do not update management route scope/invalidation behavior without the contract JSON.
