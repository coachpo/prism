# FRONTEND LIB TYPES KNOWLEDGE BASE

## OVERVIEW

`frontend/src/lib/types/` owns backend-aligned TypeScript contracts re-exported by `../types.ts`; it is a schema mirror, not a frontend view-model layer.

## STRUCTURE

```text
types/
├── auth.ts
├── config-audit-settings.ts  # Compatibility barrel for split management contracts
├── management-settings.ts    # Audit, retention, and configuration policy settings
├── audit-logs.ts              # Requests/Audit log and coverage contracts
├── retention-jobs.ts          # Retention preflight, job, and impact contracts
├── currency-migration.ts      # Costing and currency migration contracts
├── loadbalance.ts
├── model-stats.ts
├── request-logs.ts       # Requests/Audit request-log contracts; BIGINT/micros are JSON numbers
├── routing.ts
├── routing-diagnostics.ts   # Static routing-diagnostics types; the backend analyzer is authoritative
├── setup.ts                 # Readiness axes, route-witness refs, model entity refs
├── target-compatibility.ts
├── usage-statistics.ts
└── vendor.ts
```

## WHERE TO LOOK

- Public barrel: `../types.ts`
- Auth/session surfaces: `auth.ts`
- Terminal Target, routing, vendor, and model stats contracts: `target-compatibility.ts`, `routing.ts`, `vendor.ts`, `model-stats.ts`
- `JsonValue`/`JsonObject` and the `custom_request_parameters` field on `Connection`, `ConnectionCreate`, `ConnectionUpdate`, and their aliases: `routing.ts`
- Usage, analytics, and proxy-key stats payloads: `usage-statistics.ts`
- Ban Policy and load-balance payloads: `loadbalance.ts`
- Management settings contracts: `management-settings.ts`
- Audit log and coverage contracts: `audit-logs.ts`
- Retention job and impact contracts: `retention-jobs.ts`
- Costing and currency migration contracts: `currency-migration.ts`; currency drafts/previews carry `template_kind` and complete role-keyed `cards`, never a projected base/offpeak scalar.
- Compatibility import barrel: `config-audit-settings.ts`

## CONVENTIONS

- Keep server field names exactly as JSON uses them: snake_case stays snake_case.
- Preserve nullable versus optional semantics from backend responses; do not collapse `null`, missing, and empty values.
- Add new contract fields in the narrow leaf file and re-export only through `../types.ts`; request-log list, chain, detail, filter, and query types stay together in `request-logs.ts`.
- Keep frontend-only display labels, derived state, and form drafts outside this directory.
- Cross-check backend structs, migrations, and API docs when changing a type used by request-log/statistics flows. Request-log pricing evidence keeps `pricing_selection_state` and `pricing_card_role` as separate nullable fields, and peak schedule evidence is nullable independently.

## ANTI-PATTERNS

- Do not camelCase backend payloads in this layer.
- Do not put Zod/form validation schemas or UI helper types here unless they are true backend contract mirrors.
- Do not split one backend response family across unrelated type files.
