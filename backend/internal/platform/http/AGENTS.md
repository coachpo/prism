# BACKEND PLATFORM HTTP KNOWLEDGE BASE

## OVERVIEW
`platform/http/` owns backend HTTP assembly: mux construction, `/health`, `/api`, `/v1`, `/v1beta`, management mutation middleware, body limits, startup runtime snapshots, and runtime-cache invalidation wiring.

## STRUCTURE
```text
http/
├── server.go                         # Top-level server and route mounting
├── management_branch.go              # `/health` and `/api` branch wiring
├── runtime_branch.go                 # `/v1` and `/v1beta` branch wiring
├── dependencies.go                   # HTTP dependency bundle
├── hot_bootstrap_runtime.go          # Hot-applied config snapshots
├── admission.go                      # Management admission controller, route specs, settings-schema guard
├── runtime_cache_invalidation.go     # Management mutation invalidation hooks
├── route_witness_generations.go      # Sole writer of `route_witness_generations`, one bump per route-affecting mutation
├── management_body_limits.go         # Management request body limits
├── management_csrf.go                # 管理面浏览器写入守卫（Origin / Sec-Fetch-Site / JSON Content-Type）
├── management_route_contract.json    # Generated from managementRouteSpecs — do not edit by hand
└── *_test.go                         # Server, body-limit, and hot-bootstrap coverage
```

## WHERE TO LOOK
- Server assembly and exact mounted branches: `server.go`, `management_branch.go`, `runtime_branch.go`
- Startup runtime snapshots for CORS, auth, runtime proxy transport, and admission: `hot_bootstrap_runtime.go`
- Runtime cache invalidation after management mutations: `runtime_cache_invalidation.go`（注册表查表，spec 声明在 `admission.go` 的 managementRouteSpecs）
- Management admission budgets and the settings-schema guard middleware: `admission.go`, `../admission/`
- Static route-witness generation bumps consumed by the model routing surfaces: `route_witness_generations.go`
- Shared body-size enforcement: `management_body_limits.go`, `../bodylimits/`
- 管理面跨源写入与非 JSON 写入的拒绝：management_csrf.go
- Runtime operation allowlist after `/v1` mount: `../../httpapi/runtime/operations.go`

## CONVENTIONS
- Keep mounting here and handler behavior in `../../httpapi/`.
- Keep `/v1` and `/v1beta` as mounted prefixes only; supported runtime operations remain allowlisted in `../../httpapi/runtime/operations.go`.
- 新增或修改管理路由时改 managementRouteSpecs 并用 `TestManagementRouteContractMatchesRouteSpecs`（`PRISM_UPDATE_MANAGEMENT_ROUTE_CONTRACT=1`）重新生成契约 JSON；不要手工编辑该 JSON。
- Keep startup bootstrap state behind `HotBootstrapConfigRuntime`; direct file edits are not watched state and require restart.
- Keep request body limits centralized through this package and `../bodylimits/`.

## ANTI-PATTERNS
- Do not add management or runtime business logic to platform HTTP assembly.
- Do not duplicate runtime operation matching here.
- Do not update management route scope/invalidation behavior without the contract JSON.
