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

<!-- write-project-docs:shared-contributing:start -->
## Current Development Strategy

**Development tier: `MVP`**

Complete the smallest observable end-to-end loop around the confirmed core value, scope, non-goals, acceptance conditions, and exit conditions in [`docs/product.md`](docs/product.md). This tier permanently forgoes active investment in security, privacy, data, credential and key management, compatibility, audit/monitoring/SLO, and regulatory compliance requirements.

### Must Complete at This Tier

- Complete the core user flow, visible result, and error paths directly required by core acceptance.
- Run affected-path tests, checks, and builds sufficient to make the core conclusion observable and repeatable.

### Not Pursued by Default

- Permanently forgo active investment in security, privacy, data, credential and key management, permission-system expansion, compatibility layers and full compatibility regression, audit/monitoring/SLO, and regulatory compliance requirements; do not pursue non-core features, repository-wide default gates, high availability, or production hardening.
- Do not add general capabilities, abstractions, dependencies, or non-primary business branches for unverified needs.

### Non-negotiable Boundaries

- Explicit user requirements, an accepted GOAL, hard project rules or invariants, repository-required checks, and explicit prohibitions in [`STATUS.md`](STATUS.md) remain effective and are not affected by the exemption; existing compatibility commitments are existing contracts and are not deleted by this tier.
- Do not widen permissions, perform unauthorized external writes or destructive operations, delete or reset existing data, or fabricate validation results.

### Tier Transition Conditions

- Move to `PILOT` when limited real users, real or non-discardable data, external traffic, or pilot operating responsibility appears.
- Move to `PRODUCTION` when general availability, explicit SLOs, or sustained production support is required.

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
