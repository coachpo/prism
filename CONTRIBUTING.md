# Contributing to Prism

## Development Environment

- Backend: Go 1.26.6+.
- Frontend: Node.js 24+, pnpm 10.30.1 (package manager is pinned via `packageManager`).
- Docker is required for the local PostgreSQL service and the Compose bundle.

## Local Development Startup

```bash
./start.sh full      # backend + frontend dev server + PostgreSQL
./start.sh headless  # backend + PostgreSQL only
```

The launcher loads the root `.env` for variables absent from the invoking shell, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, keeps frontend on `5173` and PostgreSQL on `15432`, and follows the selected bootstrap file's backend port (fresh seeds default to `8000`). In `full` mode browser traffic stays same-origin through the local Vite proxy.

## Tests, Checks, and Builds

Backend:

```bash
cd backend
go build ./cmd/prism-backend
go test ./internal/... ./cmd/...
go test -timeout 30m ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
```

Frontend:

```bash
cd frontend
pnpm install
pnpm run dev
pnpm run test:lib
pnpm run test:e2e
pnpm run lint
pnpm run build
```

## Development Workflow

- Prism is a monorepo: `backend/` and `frontend/` are root-owned directories sharing the root launcher, release helper, and CI wiring.
- The layered AGENTS.md tree (root, `backend/`, `frontend/`, and their children) owns implementation maps and conventions; read the owning AGENTS docs before changing a surface.
- Releases go through `./release.sh` (e.g. `./release.sh patch --dry-run`), which keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata plus the frontend build, then commits, tags, and pushes the single-image release. CI gates releases on `govulncheck` and `pnpm audit`.
- Follow the project- and technology-specific rules in [docs/development-rules.md](docs/development-rules.md), the architecture facts in [docs/architecture.md](docs/architecture.md), and the unified size and responsibility policy in [docs/source-code-size-and-responsibility-rules.md](docs/source-code-size-and-responsibility-rules.md), together with the shared principles below.

<!-- write-project-docs:derived-iteration-strategy:start -->
<!-- write-project-docs:derived-iteration-strategy:metadata {"contentSha256":"sha256:6ef1c0fcb7959e4318c219a098054c2a1805e71387bb6dea2fa79f50826057d0","schemaVersion":1,"sources":[{"normalization":"without-visible-exact-mvp-control-line-terminal-lf-v2","path":"STATUS.md","sha256":"sha256:79e8c8875e8415f0bc53c51fdf0a62224aef7ebe8cad59b81da3dee82173c51b"},{"path":"docs/product.md","sha256":"sha256:71dcc516e7e37147474867e22e97d8e70fde35a96e51b038886f688e2fb09e77"},{"path":"docs/architecture.md","sha256":"sha256:ddf4e66651305e39e45ff492c4df10cd212e4c1f5a0e479600763db996450be3"},{"path":"docs/development-rules.md","sha256":"sha256:fe3927cc5a9ab4e45f39d668c5969795bf18d41e41fbeeeac6e9ca1c380f82a1"}]} -->
## Current Iteration Strategy

Deliver the accepted typed pricing-template architecture as a locally verifiable, provider-neutral change: finish the fresh-only migration, runtime card-selection and symmetric evidence loop first, then align management, currency migration, statistics/export, frontend, and authoritative documentation; treat current source, migrations, tests, and command results as the evidence.

Derived from (the source documents remain authoritative): [`STATUS.md`](STATUS.md), [`docs/product.md`](docs/product.md), [`docs/architecture.md`](docs/architecture.md), [`docs/development-rules.md`](docs/development-rules.md).

> This block scopes only the current iteration. It does not change the MVP fast-validation switch or weaken security, privacy, permissions, data integrity, existing compatibility commitments, or higher-priority requirements.

### Must Complete Now

- Finish the 000023 fresh-only schema, pricing_template_cards, pricing_template_windows, mutually exclusive kind constraints, canonical window digest, and transactional non-empty-state rollback proof.
- Finish one runtime selector for standard, tiered, and peak_valley using the frozen ingress planningReferenceNow, user IANA timezone, and half-open windows; select one complete card before FX, missing-component checks, and five-component arithmetic, and fail closed.
- Finish symmetric typed kind/state/role/selector/schedule projections for request_logs, usage_request_events, accounting, detail, CSV, connection/model/endpoint summaries, and readiness.
- Finish create/update/retype CAS/import, complete-card currency migration, soft-deleted runtime isolation, and the three-kind frontend card/window workflow.
- Run and record backend unit, contract, integration, runtime, priority, and frontend Vitest, test:lib, lint, and build gates; keep external/real-request checks explicitly marked when not run.

### Not Pursued This Iteration

- Vendor price presets, tier plus peak_valley composition, proportional cross-window billing, and a new export endpoint; basis: explicitly outside the current product scope; re-evaluate when the product contract adds the capability.
- Deployment, release, remote repository settings, branch protection, and external-instance operations; basis: this authorization covers local implementation and verification only; re-evaluate when the user separately authorizes an external write or release.
- Unrelated capacity planning, load testing, and broad performance optimization; basis: the current completion criteria require feature, persistence, and evidence closure rather than performance targets; re-evaluate when throughput, concurrency, or latency acceptance appears.

### Non-negotiable Boundaries

- Preserve the operation registry, provider streaming/non-streaming matrix, DB lanes, durable outbox, partition keys/indexes, FX, and exact five-component arithmetic semantics.
- Keep pricing_selection_state and pricing_card_role as separate evidence axes; zero, missing, unresolved, and read-failure states must not be substituted for one another.
- Pass planningReferenceNow explicitly from ingress to routing and the pricing selector; never silently fall back to a live clock or completion time at selection.
- Any retained pricing or currency-migration state must fail with a readable rebuild error before 000023 DDL and roll back completely; do not backfill old data.
- All frontend visible states must follow frontend/DESIGN.md honesty for zero, missing, failure, and truncation, without leaking enum keys or vendor presets.

### Re-derivation Triggers

- Lifecycle, deployment, user, data-policy, compatibility-policy, or version facts in STATUS.md change.
- The typed pricing-template product scope, architecture decisions, completion criteria, or fresh-only authorization changes.
- The operation registry, provider operation shapes, runtime persistence boundary, partition/outbox contract, or frontend design contract changes.
<!-- write-project-docs:derived-iteration-strategy:end -->

<!-- write-project-docs:shared-contributing:start -->
## General Design Principles

While satisfying the confirmed functional scope, architectural boundaries, quality attributes, security, compatibility, and runtime constraints, choose a design in this order:

1. Existing, verified, and still-applicable designs, patterns, interfaces, or components already in the project;
2. Applicable formal standards, standard protocols, and officially recommended platform or framework solutions;
3. Mature industry solutions that are widely adopted in similar scenarios, actively maintained, and backed by reliable evidence of practice;
4. Only when none of the above satisfies a verified constraint, the smallest custom design that meets the current requirement.

"Widely used" is a candidate signal, not sufficient reason to adopt. Before adopting, check requirement fit, security and compatibility, primary failure modes, and maintenance and migration cost against the risk. Do not introduce capabilities, abstractions, or dependencies outside the current scope merely to follow a convention.

For significant design choices that touch architectural boundaries, dependency direction, data ownership, security boundaries, or long-lived dependencies, record the applicable rationale, the primary trade-offs, and the verification method in the design outcome. When adopting a custom design, also state the verified constraints that make mature solutions inapplicable. When risk is high and evidence is thin, first define observable success, failure, and exit conditions, then run the smallest reversible validation your current authority allows. Do not write unaccepted or unimplemented candidates into the record as current architectural fact.

## General Implementation Principles

While satisfying functional scope, architectural boundaries, correctness, security, and verifiability, choose an implementation in this order:

1. Existing implementations in the project;
2. The language standard library;
3. Platform-native capabilities;
4. Dependencies already installed in the project that fit the current scenario;
5. Mature, actively maintained, widely used third-party libraries suited to the current environment;
6. The smallest custom implementation that meets the current requirement.

Search for an existing implementation before adding new code. Do not pull in a large dependency for a small feature; do not create abstraction, extension, or compatibility layers for hypothetical future requirements; keep custom implementations local, simple, and testable.

Implementations must comply with the project architecture facts in [`docs/architecture.md`](docs/architecture.md), the project- and technology-specific rules in [`docs/development-rules.md`](docs/development-rules.md), and the unified size and responsibility rules in [`docs/source-code-size-and-responsibility-rules.md`](docs/source-code-size-and-responsibility-rules.md).

## Definition of Done

A change is complete only when all of the following hold:

- The implementation matches the confirmed functional scope and acceptance conditions;
- Significant design choices have been checked against the applicability of mature solutions; where a custom solution was adopted, the inapplicable constraints, primary trade-offs, and verification method are recorded;
- Existing architectural boundaries and dependency direction are preserved, with no unrelated responsibilities or incidental changes added;
- Applicable project- and technology-specific development rules are satisfied;
- Relevant tests, static checks, format checks, and build verification pass;
- The single authoritative documents, machine contracts, and validation are synchronized as required by the development rules;
- No secrets, credentials, personal data, build artifacts, or unrelated files are committed;
- The source code size and responsibility rules have been applied, and any long files that need explanation are reported.
<!-- write-project-docs:shared-contributing:end -->
