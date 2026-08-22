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
<!-- write-project-docs:derived-iteration-strategy:metadata {"contentSha256":"sha256:9cbaaac428978dccc55d9506c5f4265dd16488ef1f26527f0c245ed93a0a84b1","schemaVersion":1,"sources":[{"normalization":"without-visible-exact-mvp-control-line-terminal-lf-v2","path":"STATUS.md","sha256":"sha256:76c51dc615f28b1d11423421704e11a5486fff0b03e19221ffe98a5c416b00c4"},{"path":"docs/product.md","sha256":"sha256:c119f9c4f1b439ad8a0ab2d449fe5f55eb5e901ce4973bdcb7f951f6a0707b06"},{"path":"docs/architecture.md","sha256":"sha256:0edcb56a4ffd5d16e9affcf0d22f2d70466d9638c204a2391e31e295056c9d23"},{"path":"docs/development-rules.md","sha256":"sha256:c654dedd88097c8434d6e4cf8a7cf09463b0ea063ac9b8d36135698adf8e4d49"}]} -->
## Current Iteration Strategy

Convenience-first active development on the operator's personal home-LAN instance: work is prioritized from gaps observed in day-to-day use of the running instance, and where a change trades off against data-security hardening the convenient path wins. Keep the local dev/deploy loop (launcher full|headless, root Compose bundle, same-origin Vite proxying) fast, easy, and accurately documented.

Derived from (the source documents remain authoritative): [`STATUS.md`](STATUS.md), [`docs/product.md`](docs/product.md), [`docs/architecture.md`](docs/architecture.md), [`docs/development-rules.md`](docs/development-rules.md).

> This block scopes only the current iteration. It does not change the MVP fast-validation switch, expand user authorization, authorize external writes or destructive operations, delete or reset existing data, fabricate validation results, or override higher-priority user requirements or explicit prohibitions.

### Must Complete Now

- Keep the documented dev/deploy loop (launcher full|headless, root Compose bundle, same-origin Vite proxying, plaintext bootstrap defaults) working and convenient, green on the applicable quick checks.
- Pick up operator-facing gaps observed in day-to-day use of the running instance and ship them as the smallest verified changes, per the no-shim clean-architecture convention.

### Not Pursued This Iteration

- Data-security hardening beyond the shipped controls (TLS termination, external secret-manager integration, mandatory operator/query auth, global rate limiting, proxy-key scoping): basis: no positive trigger on a personal home-LAN deployment without external exposure; re-evaluate when the instance gains public exposure, additional operators or tooling, or the operator's convenience-vs-security priority changes.

### Non-negotiable Boundaries

- Machine contracts (runtime operation registry with hook residency, file-backed bootstrap v1 with the required timeout fields, partitioned log-retention ownership) and their backend regression coverage are not lowered by the convenience-first stance.
- Destructive data resets or data loss still require explicit authorization and a verified backup; the convenience-first priority does not authorize them.
- Existing required checks still gate changes and releases: backend contract/integration/runtime/priority suites, frontend lint/build/test, and the CI dependency scanners (govulncheck, pnpm audit).

### Re-derivation Triggers

- The instance is exposed beyond the home LAN or operated for anyone else.
- Deployment, user, or data-policy facts in STATUS.md change, including the recorded convenience-vs-security priority.
- A release or lifecycle milestone changes the recorded version or the development mode in STATUS.md.
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

### MVP Fast Validation

This phase targets validating the confirmed core assumptions, scope, non-goals, and observable acceptance and exit conditions in [`docs/product.md`](docs/product.md), prioritizing the smallest observable end-to-end loop that produces usable evidence. "Shortest" means the shortest path to trustworthy feedback, not the fewest lines of code, steps, or tests.

This subsection is the enabled-MVP scope overlay for the "General Design Principles" and "General Implementation Principles": include only work that directly serves the core validation above, without expanding scope for completeness.

**Explicitly out of scope:** security or privacy governance, permission-system expansion, data-integrity enhancements, compatibility-layer construction and full compatibility regression, dedicated reachable-risk governance, repository-wide full checks, and production hardening that are unrelated to the core assumptions, observable acceptance conditions, and exit conditions, together with additional features and non-primary business branches that do not affect the current conclusion. Existing compatibility commitments and repository-required checks are not themselves in this category and do not authorize exclusion; only their non-core specialized implementation, full validation, or default gates may be excluded. Existing compatibility commitments still constrain solution choice, and checks required for affected paths or core acceptance still run.

**May be deferred:** work that does not currently affect the conclusion but must return when observable triggers arise, such as real users, real or non-discardable data, external traffic, compatibility acceptance, the corresponding risk, or another observable condition. Every item must state the work, the current basis for deferral, and at least one observable re-evaluation trigger. An item without such a trigger that is unrelated to core validation is explicitly out of scope; an item that affects the current conclusion belongs in the current loop.

**Still constraints:**

- Do not widen permissions.
- Do not perform unauthorized external writes.
- Do not perform unauthorized destructive operations.
- Do not delete or reset existing data.
- Do not fabricate validation results.
- Do not intentionally violate explicit prohibitions in [`STATUS.md`](STATUS.md).

These constraints limit available actions without automatically adding specialized implementation or full checks.

Within that narrowed scope, design and implementation still follow their respective ordering above. When several options can complete the core validation, prefer the one with the smallest change surface, the fewest new dependencies, and the easiest observation and rollback. Local, low-risk, reversible implementation details that do not change authorization boundaries may be decided directly within existing authority and the established development workflow.

Implement only what the current validation requires; do not add generalized capabilities, abstractions, or dependencies for unvalidated requirements. Run the narrow validation needed to make the conclusion observable and reproducible. Non-core repository-wide full checks, default gates, and production hardening are not completion prerequisites, but checks explicitly required for affected paths or core acceptance still run. Do not present an unaccepted validation implementation as formal architecture or production capability.

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
