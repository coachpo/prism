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
<!-- write-project-docs:derived-iteration-strategy:metadata {"contentSha256":"sha256:15b7d81e6c1f340965919944b7722ed06bdbae80f52cbabf534a762da416956e","schemaVersion":1,"sources":[{"normalization":"without-visible-exact-mvp-control-line-terminal-lf-v2","path":"STATUS.md","sha256":"sha256:79e8c8875e8415f0bc53c51fdf0a62224aef7ebe8cad59b81da3dee82173c51b"},{"path":"docs/product.md","sha256":"sha256:e2c29705dce6de2acf20b7a3f03fe644ffc6c1c298e4c666e9f34b18d5d872aa"},{"path":"docs/architecture.md","sha256":"sha256:9ea161b7366b1ea41888f7c623e1d806c3f57b0f22534cfc553ad11c5ed5f20f"},{"path":"docs/development-rules.md","sha256":"sha256:c654dedd88097c8434d6e4cf8a7cf09463b0ea063ac9b8d36135698adf8e4d49"}]} -->
## Current Iteration Strategy

Convenience-first active development on the operator's personal home-LAN instance: work is prioritized from gaps observed in day-to-day use of the running instance, and where a change trades off against data-security hardening the convenient path wins. Keep the local dev/deploy loop (launcher full|headless, root Compose bundle, same-origin Vite proxying) fast, easy, and accurately documented.

Derived from (the source documents remain authoritative): [`STATUS.md`](STATUS.md), [`docs/product.md`](docs/product.md), [`docs/architecture.md`](docs/architecture.md), [`docs/development-rules.md`](docs/development-rules.md).

> This block scopes only the current iteration. It does not change the MVP fast-validation switch or weaken security, privacy, permissions, data integrity, existing compatibility commitments, or higher-priority requirements.

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
