# Backend Test Boundaries

Choose one observable boundary for each regression:

- [Contract](contract/AGENTS.md): management HTTP/persistence contracts for one API surface.
- [Integration](integration/AGENTS.md): startup, migration, launcher/container, retention, and cross-service contracts.
- [Runtime](runtime/AGENTS.md): operation routes, request execution, retained request evidence, streaming, caches, and telemetry.
- `priority/`: database/admission isolation, scheduler ownership, durable side effects, and no-inline-fallback behavior. Include this suite when changing admission, scheduler, outbox, pool, cache invalidation, or after-commit behavior.

Go-specific rules:

- Use the existing defaulted harness builders.
- Route additions require operation matrix, rejection isolation, hook residency, and persisted operation-name coverage.
- Migration regressions use the checked-in runner and normalized schema snapshots. Direct-entry changes need management omission/null/boolean, recursive routing versus entry rejection, witness/setup roots, model list/export/self-test selectors, and retained-row preservation coverage at their owning layers.
- Keep bootstrap, launcher, partition retention, and Dockerfile regressions when changing their shipped contracts. Runtime allowlist absence and the container ownership/path contract remain permanent guards.
- Use the narrowest owning suite first.
