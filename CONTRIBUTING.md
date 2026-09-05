# Contributing to Prism

## Development Environment

- Backend: Go 1.26.6 or newer, as declared by `backend/go.mod`.
- Frontend: Node.js 24+ and pnpm 10.30.1, pinned in `frontend/package.json`.
- Docker with Compose is required for local PostgreSQL and the backend suites' disposable database harness.
- Install frontend dependencies from `frontend/` with `pnpm install --frozen-lockfile`. CI enables the pinned pnpm through Corepack.

## Local Development Startup

Run from the repository root:

```bash
./start.sh full      # backend + frontend dev server + PostgreSQL
./start.sh headless  # backend + PostgreSQL only
```

The launcher loads the root `.env` for variables absent from the invoking shell, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, keeps frontend on `5173` and PostgreSQL on `15432`, and follows the selected bootstrap file's backend port (fresh seeds default to `8000`). It validates that the existing bootstrap file matches the local launcher database/listener contract and preserves valid files. In `full` mode it unsets `VITE_API_BASE` and enables `PRISM_VITE_PROXY_ENABLED=1` with `PRISM_VITE_PROXY_TARGET` pointing at that backend, including `/health`.

For a direct Go run, start the database first, then run `go run ./cmd/prism-backend` from `backend/` with an absolute `PRISM_CONFIG_PATH` pointing at the existing bootstrap file. A fresh standalone backend seeds PostgreSQL on `5432` unless `DATABASE_URL` is supplied; the launcher seeds its own DSN on `15432`. Existing files remain authoritative over the seed input. Do not reset a retained database or bootstrap file to fix a configuration mismatch; follow [STATUS.md](STATUS.md#data).

For frontend-only work, run `pnpm run dev` from `frontend/`. A standalone Vite server has no launcher proxy by default: set the development `VITE_API_BASE` as shown in [frontend/.env.example](frontend/.env.example) to reach a backend, and configure that backend's CORS accordingly. Without the launcher proxy, Vite's `/health` reports the frontend server's health only. Production uses the root Nginx/app image.

## Tests, Checks, and Builds

Run the affected checks and all checks required by the owning AGENTS guide. The CI-equivalent backend commands, from `backend/`, are:

```bash
go test ./internal/... ./cmd/...
go test -timeout 30m ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/...
go build ./cmd/prism-backend
```

The frontend commands, from `frontend/`, are:

```bash
pnpm exec vitest run
pnpm run test:lib
pnpm run build
pnpm run lint
pnpm exec playwright install chromium --with-deps
pnpm run test:e2e
```

`pnpm test` starts Vitest in watch mode. The non-watch command above matches CI. [backend/tests/AGENTS.md](backend/tests/AGENTS.md) and [frontend/tests/AGENTS.md](frontend/tests/AGENTS.md) own runner boundaries; [Test Ownership](docs/development-rules.md#test-ownership) owns the shared regression policy. Playwright's ordinary suite uses the local Vite server and mocked APIs; the separately invoked `pnpm run test:e2e:closed-loop` runs the same browser suite with an owned local Vite lifecycle inside a pinned Playwright container, as described by [the e2e guide](frontend/tests/e2e/AGENTS.md).

`.github/workflows/ci.yml` also runs blocking `govulncheck ./...` (with govulncheck 1.1.4) and `pnpm audit --prod --audit-level=high`, plus non-blocking single-image Trivy evidence collection. These scanner jobs do not replace the affected behavior checks.

## Development Workflow

- Prism is one repository: backend, frontend, launcher, release helper, and CI are versioned together. Read the owning AGENTS hierarchy before editing a surface.
- Use [STATUS.md](STATUS.md) for current lifecycle, deployment, data, and compatibility facts and [docs/product.md](docs/product.md) for delivery scope. The selected static development strategy below does not override them.
- Follow [docs/development-rules.md](docs/development-rules.md), [docs/architecture.md](docs/architecture.md), and [docs/source-code-size-and-responsibility-rules.md](docs/source-code-size-and-responsibility-rules.md). UI work follows [frontend/DESIGN.md](frontend/DESIGN.md) and the checked-in `frontend/components.json`; add primitives through that existing shadcn registry into `frontend/src/components/ui/`.
- Keep active plans and execution evidence under ignored `artifacts/`, while canonical docs describe the implemented product and contracts.

## Releases

`./release.sh patch --dry-run` previews a release. Live `./release.sh patch` (or `minor`, `major`, or an explicit version) requires release authorization: it keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata and the frontend build, commits, tags, and pushes the root release. The helper requires clean, current `main` and a forward version; it does not deploy an instance.

`.github/workflows/docker-images.yml` publishes only `ghcr.io/coachpo/prism` for `linux/arm64`. A `v*` tag requires green CI for its commit and moves `latest`; manual workflow dispatch does not move `latest`. `.github/workflows/cleanup.yml` only prunes untagged single-image container versions. Instance inspection, backup/restore, and authorized rollout procedures are indexed in [docs/README.md](docs/README.md#specialized-documents).

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
