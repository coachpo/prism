# Domain Ownership

Keep these packages HTTP-neutral: handlers, authentication, profile selection, and response serialization belong in `../httpapi/`; process composition and lane allocation belong in `../platform/`. Domain stores may use injected query/partition interfaces without owning infrastructure lifecycle.

Read the local guides for [loadbalance](loadbalance/AGENTS.md), [models.dev](modelsdev/AGENTS.md), [safe diagnostics](safediag/AGENTS.md), and [statistics](stats/AGENTS.md).

- `modelrouting/` owns graph diagnostics and route witnesses. Keep all configured nodes available for recursive Model Targets; `direct_request_enabled` gates client-entry roots and representative witnesses only.
- `terminaltarget/upstream_model_id.go` is the shared trim/non-blank/200-rune wire-identity validator. `custom_request_parameters.go` is the shared parser, validator, and shallow overlay; consumers must not fork its protected keys, limits, or canonicalization rules.
- `terminaltarget/routing_schedule.go` owns eligibility through `DecideAt`; no windows means unrestricted. Keep compiled schedules immutable and methods on value receivers because runtime snapshots share their backing slices. Pricing windows in `pricing_schedule.go` reuse geometry but belong to pricing revisions and the frozen ingress clock.
- `pidev/` owns catalog transport, revision/schema checks, exact candidates, and bounded same-API model-ID search. `modelexport/` owns the deterministic Pi renderer and clock-free digest. Keep pi.dev bindings independent from models.dev metadata/pricing bindings.
- Render from the persisted effective Pi template plus caller-supplied Prism origin/provider/credential and current pricing facts. Do not read stored endpoint keys, infer client URLs from upstream endpoints, fetch the live catalog, or trust a request coordinate as anything beyond an assertion against the binding.
- Validate source and override Pi leaves with the pinned schema in `pidev/compat.go` and `modelexport/render_pi_schema.go`. Sanitize catalog compatibility fields through the per-API allowlist, preserve sorted dropped paths, and reject invalid manual overrides. Preserve explicit null versus omitted fields and thinking-level keys.
- Export cost only for one representable USD/PER_1M five-component shape across all reachable targets with reasoning price equal to output price. Missing components omit the entire cost group with a warning; explicit zero stays free. The renderer supports at most one strict positive-threshold tier.
