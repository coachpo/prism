# Prism Agent Guide

Prism is a self-hosted LLM proxy gateway. `backend/` and `frontend/` are root-owned monorepo directories; the root owns startup, packaging, releases, and CI.

## Work Ownership

- Read [backend/AGENTS.md](backend/AGENTS.md) for Go runtime, migrations, and backend tests; follow its child guides for the affected package.
- Read [frontend/AGENTS.md](frontend/AGENTS.md) for the dashboard, typed clients, and frontend tests. All UI/UX guidance and changes defer to [frontend/DESIGN.md](frontend/DESIGN.md).
- Read [docs/AGENTS.md](docs/AGENTS.md) for canonical documentation, operator runbooks, and managed-block ownership.
- Root startup and packaging changes belong in `start.sh`, `docker-compose.yml`, `Dockerfile`, and `docker/`. Keep ordinary startup instructions in [README.md](README.md) and development/release commands in [CONTRIBUTING.md](CONTRIBUTING.md).
- Keep scratch plans in ignored `artifacts/plans/` and execution evidence in `artifacts/evidence/`. Maintained operator SQL belongs in `scripts/operations/`; its runbooks stay in `docs/operations/` with integration acceptance under `backend/tests/integration/`.

## Cross-Directory Boundaries

- [STATUS.md](STATUS.md) owns data and compatibility policy. Upgrade work prefers clean architecture over compatibility shims, but retained PostgreSQL history and plaintext bootstrap files are not disposable. A change that cannot carry data forward needs explicit authorization and a verified backup.
- Follow [Development Rules](docs/development-rules.md) for implementation and test ownership. Keep runtime contracts grounded in `backend/internal/httpapi/runtime/operations.go`, not broad `/v1` or `/v1beta` passthrough assumptions.
- Management scope is frozen to Default profile id `1`; `X-Profile-Id` never selects proxy traffic. Runtime model lookup, provider attempts, and telemetry use the frozen request snapshot.
- OpenAI text modes require strict equality across authored model/target relations and runtime gates; operation-coverage diagnostics do not relax this rule. Image capability uses its independent containment rule. See [the native operation contract](docs/architecture.md#22b-openai-native-mode-equality-strict).
- `start.sh` owns local PostgreSQL on `15432` and frontend on `5173`, preserves valid selected bootstrap files, and follows their backend port. Full mode unsets `VITE_API_BASE` and enables same-origin Vite proxying; keep launcher changes and docs aligned.
- The root Compose file is the only checked-in local/self-hosted bundle. The root Dockerfile builds one app image with Go, React assets, and Nginx; PostgreSQL remains separate. Container changes must preserve or explicitly update the non-root `1000:1000` and `/app/config/config.json` contract, its docs, and `backend/tests/integration/dockerfile_contract_test.go`.
- `release.sh` owns all four version surfaces. `.github/workflows/docker-images.yml` publishes one ARM64 GHCR image; tag releases require green CI. Do not invent separate frontend/backend releases or images.
- Validate changes with the affected commands in [CONTRIBUTING.md](CONTRIBUTING.md) and the owning subtree guide. LLM request/response changes must cover the operation shapes and stream/non-stream boundaries specified in the development rules.

<!-- write-project-docs:document-navigation:start -->
## Project Documentation Navigation

Before starting related work, read the authoritative documents that cover the scope of the task:

- [Project Status](STATUS.md)
- [Documentation Index](docs/README.md)
- [Product Overview](docs/product.md)
- [Architecture Overview](docs/architecture.md)
- [Development Rules](docs/development-rules.md)
- [Source Code Size and Responsibility Rules](docs/source-code-size-and-responsibility-rules.md)
- [Contributing Guide](CONTRIBUTING.md)

When implementing, reviewing, or verifying an engineering change, use `STATUS.md` and the product overview for current facts and delivery intent, then read the [Current Development Strategy](CONTRIBUTING.md#current-development-strategy). Consume only the relevant "Must Complete at This Tier," "Not Pursued by Default," "Non-negotiable Boundaries," and "Tier Transition Conditions." New user requirements, an accepted seven-line GOAL, hard project rules or invariants, real or non-discardable data, existing users, and compatibility commitments take precedence over tier defaults. The `YOLO_LOCAL`, `EXPERIMENT`, and `MVP` tiers permanently forgo active investment in security, privacy, data, credential and key management, compatibility, audit/monitoring/SLO, and regulatory compliance requirements; the exemption does not override those precedence sources and does not change a tier's applicability, transition conditions, or existing prohibitions. A tier does not expand user authorization, create exclusion proof, or allow checks required by affected paths or core acceptance to be skipped. `YOLO_LOCAL` applies only to a user-declared disposable local workspace with no real data, production credentials, external users or traffic, or external side effects; change tiers before proceeding when any condition fails.

## Project Documentation Content Boundaries

This project does not add process or administrative management for the sake of documentation completeness.

- Unless the user explicitly asks and provides verifiable evidence, do not add approvals, reporting, meetings, scheduling, personnel governance, release governance, commit management, business KPIs/SLOs, or similar content.
- Do not create documents, sections, placeholders, or "to be confirmed" items for those topics.
- Existing and verified development, test, build, and deployment commands remain recorded in their own authoritative documents; this block does not change product, architecture, or engineering facts.
<!-- write-project-docs:document-navigation:end -->

<!-- write-agent-guides:engineering-router:start -->
## Engineering Router

Use the narrowest matching row. Read the linked authority before changing code and run only validation already authorized by the task.

| Trigger | Authority | Invariant IDs | Validation entry |
| --- | --- | --- | --- |
| `backend`: Changing Go entrypoints, configuration, toolchain, dependencies, generated inputs, tests, or release artifacts | [docs/architecture.md](docs/architecture.md#inv-go-229d2b017a9b) | [INV-GO-229D2B017A9B](docs/architecture.md#inv-go-229d2b017a9b) | cd backend && go build ./cmd/prism-backend && go test ./internal/... ./cmd/... |
| `backend`: Changing Context chains, goroutines, connections, commands, servers, workers, admission, or shutdown | [docs/architecture.md](docs/architecture.md#inv-go-2d7d34ee6112) | [INV-GO-2D7D34EE6112](docs/architecture.md#inv-go-2d7d34ee6112) | cd backend && go test -timeout 30m ./internal/platform/lifecycle ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/... |
| `backend`: Changing transactions, migrations, messages, caches, remote calls, retries, or cross-resource writes | [docs/architecture.md](docs/architecture.md#inv-go-2e9f9d48ab43) | [INV-GO-2E9F9D48AB43](docs/architecture.md#inv-go-2e9f9d48ab43) | cd backend && go test -timeout 30m ./tests/contract ./tests/integration |
| `backend`: Changing package dependencies, synchronous calls, authoritative facts, shared state, or goroutine ownership | [docs/architecture.md](docs/architecture.md#inv-go-a392eb849715) | [INV-GO-A392EB849715](docs/architecture.md#inv-go-a392eb849715) | cd backend && go vet ./internal/... && go test ./internal/... |
| `backend`: Changing HTTP, CLI, file, identity, secret, diagnostic, limit, or public-error boundaries | [docs/architecture.md](docs/architecture.md#inv-go-fd5b268343ae) | [INV-GO-FD5B268343AE](docs/architecture.md#inv-go-fd5b268343ae) | cd backend && go test -timeout 30m ./tests/runtime |
<!-- write-agent-guides:engineering-router:end -->
