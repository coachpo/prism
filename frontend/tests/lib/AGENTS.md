# Node seam tests

- Run `pnpm run test:lib` from `frontend/`; the package script discovers every `*.test.mjs` directly in this directory. A focused check may use `node --test tests/lib/<file>.test.mjs`.
- Use `createTsModuleLoader()` from `../helpers/loadTsModule.mjs` for TS imports/`@/` resolution and inject narrow mocks. It transpiles modules for Node; this layer is not a TypeScript compiler or jsdom test runner.
- Use Node `assert/strict` against exported behavior and server-shaped payloads. Preserve route, profile-scope, snake_case, null/omission, and pricing-role distinctions in the existing client/form seams.
- Do not move Playwright/React Testing Library here or hide contract mismatches behind global mocks. Colocated Vitest tests own React component/hook lifecycles.
