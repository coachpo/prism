# FRONTEND LIB TEST KNOWLEDGE BASE

## OVERVIEW

`frontend/tests/lib/` owns Node `--test` seam contracts for API clients, route/profile scope helpers, observability snapshots, and shared page state.

## STRUCTURE

```text
lib/
├── *_contract.test.mjs       # Contract seams loaded through Node test
└── pricing-*.test.mjs        # Pricing kind/card/window form normalization and honest-state checks
```

## WHERE TO LOOK

- TypeScript module loader and `@/` alias support: `../helpers/loadTsModule.mjs`
- Scripted `test:lib` glob coverage: `../../package.json` runs `tests/lib/*.test.mjs`; historical model-detail node tests are absent.
- Management and profile-scope contracts: `management_*.test.mjs`, `profile_*_contract.test.mjs`
- Observability/request-log contracts: `observability_api_contract.test.mjs`, `request_log_*_contract.test.mjs`
- Request-log type ownership and number/micros wire-shape guard: `request_log_type_ownership_contract.test.mjs`
- Observability contracts: `observability_*_contract.test.mjs`
- Form and costing seams: `model_*_contract.test.mjs`, `pricing-template-*.test.mjs`, `costing_reporting_currency_contract.test.mjs`, `reporting_currency_contract.test.mjs`

## CONVENTIONS

- Use `createTsModuleLoader()` for TS modules and inject narrow mocks for browser globals, API calls, or React-only imports.
- `pnpm run test:lib` covers every `*.test.mjs` directly under `tests/lib/`; run a specific `node --test` file only for narrow debugging.
- Keep tests contract-shaped: input payload, exported function, expected server-aligned fields. Typed pricing cases must cover mutually exclusive kind branches, complete card roles, explicit zero versus null, and no legacy flat-price fallback.
- Preserve backend field names in assertions. These tests should catch accidental camelCase or route-scope drift.
- Use Node `assert/strict`; avoid browser or React Testing Library here.

## ANTI-PATTERNS

- Do not move Playwright browser flows into this folder.
- Do not add global mocks that hide profile-scope, route, or payload mismatches.
- Do not duplicate Vitest/jsdom tests from `src/**/*.test.{ts,tsx}`.
