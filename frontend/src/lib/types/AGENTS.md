# FRONTEND LIB TYPES KNOWLEDGE BASE

## OVERVIEW
`frontend/src/lib/types/` owns backend-aligned TypeScript contracts re-exported by `../types.ts`; it is a schema mirror, not a frontend view-model layer.

## STRUCTURE
```text
types/
├── auth.ts
├── config-audit-settings.ts
├── loadbalance.ts
├── model-stats.ts
├── routing.ts
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
- Audit settings contract: `config-audit-settings.ts`

## CONVENTIONS
- Keep server field names exactly as JSON uses them: snake_case stays snake_case.
- Preserve nullable versus optional semantics from backend responses; do not collapse `null`, missing, and empty values.
- Add new contract fields in the narrow leaf file and re-export only through `../types.ts`.
- Keep frontend-only display labels, derived state, and form drafts outside this directory.
- Cross-check backend structs, migrations, and API docs when changing a type used by request-log/statistics flows.

## ANTI-PATTERNS
- Do not camelCase backend payloads in this layer.
- Do not put Zod/form validation schemas or UI helper types here unless they are true backend contract mirrors.
- Do not split one backend response family across unrelated type files.
