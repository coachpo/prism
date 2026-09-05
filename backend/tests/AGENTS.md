# Backend Test Boundaries

Follow [shared test ownership rules](../../docs/development-rules.md#test-ownership). Choose one observable boundary for each regression:

- [Contract](contract/AGENTS.md): management HTTP/persistence contracts for one API surface.
- [Integration](integration/AGENTS.md): startup, migration, launcher/container, retention, and cross-service contracts.
- [Runtime](runtime/AGENTS.md): operation routes, request execution, retained request evidence, streaming, caches, and telemetry.
- `priority/`: database/admission isolation, scheduler ownership, durable side effects, and no-inline-fallback behavior. Include this suite when changing admission, scheduler, outbox, pool, cache invalidation, or after-commit behavior.

Go-specific rules:

- Share PostgreSQL through each package's `TestMain` and template-database cloning. Use existing defaulted harness builders; do not start Docker, `go build`, or `go run` inside test functions.
- Process-local unit tests own pure pricing, planning, and stream classification. Upstream changes require both package-local runtime/provider tests and `tests/runtime`; route additions require operation matrix, rejection isolation, hook residency, and persisted operation-name coverage.
- Migration regressions use the checked-in runner and normalized schema snapshots. Direct-entry changes need management omission/null/boolean, recursive routing versus entry rejection, witness/setup roots, model list/export/self-test selectors, and retained-row preservation coverage at their owning layers.
- Keep bootstrap, launcher, partition retention, and Dockerfile regressions when changing their shipped contracts. Runtime allowlist absence and the container ownership/path contract remain permanent guards.

Full backend commands are in [backend guidance](../AGENTS.md) and [CONTRIBUTING](../../CONTRIBUTING.md). Keep CI discovery glob-based and use the narrowest owning suite first.
