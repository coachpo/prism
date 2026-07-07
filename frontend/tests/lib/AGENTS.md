# FRONTEND LIB TEST KNOWLEDGE BASE

## OVERVIEW
`frontend/tests/lib/` owns Node `--test` seam contracts for API clients, config import validation, route/profile scope helpers, websocket protocol, dashboard snapshots, and shared page state.

## STRUCTURE
```text
lib/
├── *_contract.test.mjs       # Contract seams loaded through Node test
├── config-*.test.mjs         # Config import/export edge checks
└── pricing-*.test.mjs        # Pricing form normalization checks
```

## WHERE TO LOOK
- TypeScript module loader and `@/` alias support: `../helpers/loadTsModule.mjs`
- Scripted `test:lib` allowlist: `../../package.json`
- Config/bootstrap/import contracts: `bootstrap_config_contract.test.mjs`, `config_api_contract.test.mjs`, `config_import_validation_contract.test.mjs`
- Management and profile-scope contracts: `management_*.test.mjs`, `profile_*_contract.test.mjs`
- Observability/request-log contracts: `observability_api_contract.test.mjs`, `request_log_*_contract.test.mjs`
- Dashboard/statistics/realtime contracts: `dashboard_*.test.mjs`, `analytics_websocket_contract.test.mjs`, `websocket_contract.test.mjs`
- Form and costing seams: `model_*_contract.test.mjs`, `pricing-template-*.test.mjs`, `costing_reporting_currency_contract.test.mjs`, `reporting_currency_contract.test.mjs`

## CONVENTIONS
- Use `createTsModuleLoader()` for TS modules and inject narrow mocks for browser globals, API calls, websocket transports, or React-only imports.
- Check `package.json` before assuming `pnpm run test:lib` covers a file; several contracts require explicit `node --test tests/lib/<file>.mjs`.
- Keep tests contract-shaped: input payload, exported function, expected server-aligned fields.
- Preserve backend field names in assertions. These tests should catch accidental camelCase or route-scope drift.
- Use Node `assert/strict`; avoid browser or React Testing Library here.

## ANTI-PATTERNS
- Do not move Playwright browser flows into this folder.
- Do not add global mocks that hide profile-scope, route, or payload mismatches.
- Do not duplicate Vitest/jsdom tests from `src/**/*.test.{ts,tsx}`.
