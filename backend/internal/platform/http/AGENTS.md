# HTTP Assembly

`server.go`, `management_branch.go`, and `runtime_branch.go` mount handlers. Business behavior belongs in `../../httpapi/`; mounted runtime prefixes do not widen `../../httpapi/runtime/operations.go`.

- `management_route_specs.go` owns route tier, Default-profile scope, and cache-invalidation effects. Update this registry with route changes; never hand-edit generated `management_route_contract.json`.
- `route_witness_generations.go` is the sole writer of route-witness generations. Preserve one bump for each route-affecting mutation and registry-driven invalidation in `runtime_cache_invalidation.go`.
- `StartupConfigRuntime` holds the startup-only snapshot consumed by CORS, auth, admission, alerting, and upstream transport. External file edits require restart.
- Keep body limits in `management_body_limits.go` and `../bodylimits/`, admission in `management_admission.go`/`runtime_proxy_admission.go`, and schema-transition rejection in `settings_schema_guard.go`.
- `management_csrf.go` owns browser mutation checks for Origin, Sec-Fetch-Site, and JSON Content-Type; preserve this boundary when mounting new management writes.

To regenerate the management contract after an authorized route change, run from `backend/`:

```bash
PRISM_UPDATE_MANAGEMENT_ROUTE_CONTRACT=1 go test ./internal/platform/http -run '^TestManagementRouteContractMatchesRouteSpecs$'
```

The same command without the environment variable checks the generated artifact.
