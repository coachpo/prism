# Contributing to Prism

## Development Environment

- Backend: Go 1.26.6 or newer, as declared by `backend/go.mod`.
- Frontend: Node.js 24+ and pnpm 10.30.1, pinned in `frontend/package.json`.
- Docker with Compose is required for launcher-managed PostgreSQL. The backend regression suites use disposable PostgreSQL, provisioned by Docker by default. The integration harness can use an already provisioned disposable cluster through `PRISM_TEST_POSTGRES_PORT`. Dump checks additionally require the container named by `PRISM_TEST_POSTGRES_CONTAINER`.
- Install frontend dependencies from `frontend/` with `pnpm install --frozen-lockfile`. CI enables the pinned pnpm through Corepack.

## Local Development Startup

Run from the repository root:

```bash
./start.sh full      # backend + frontend dev server + PostgreSQL
./start.sh headless  # backend + PostgreSQL only
```

The launcher loads the root `.env` for variables absent from the invoking shell, defaults `PRISM_CONFIG_PATH` to repo-local `config.json`, keeps frontend on `5173` and PostgreSQL on `15432`, and follows the selected bootstrap file's backend port (fresh seeds default to `8000`). It validates that the existing bootstrap file matches the local launcher database/listener contract and preserves valid files. In `full` mode it unsets `VITE_API_BASE` and enables `PRISM_VITE_PROXY_ENABLED=1` with `PRISM_VITE_PROXY_TARGET` pointing at that backend, including `/health`.

For a direct Go run, start the database first, then run `go run ./cmd/prism-backend` from `backend/` with an absolute `PRISM_CONFIG_PATH` pointing at the existing bootstrap file. A fresh standalone backend seeds PostgreSQL on `5432` unless `DATABASE_URL` is supplied; the launcher seeds its own DSN on `15432`. Existing files remain authoritative over the seed input. Do not reset a retained database or bootstrap file to fix a configuration mismatch; follow the [retained-data policy](#retained-data-and-compatibility-policy).

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

`pnpm test` starts Vitest in watch mode. The non-watch command above matches CI. [backend/tests/AGENTS.md](backend/tests/AGENTS.md) and [frontend/tests/AGENTS.md](frontend/tests/AGENTS.md) own runner boundaries; [Test Ownership](#test-ownership) owns the shared regression policy. Playwright's ordinary suite uses the local Vite server and mocked APIs; the separately invoked `pnpm run test:e2e:closed-loop` runs the same browser suite with an owned local Vite lifecycle inside a pinned Playwright container, as described by [the e2e guide](frontend/tests/e2e/AGENTS.md).

`.github/workflows/ci.yml` also runs blocking `govulncheck ./...` (with govulncheck 1.1.4) and `pnpm audit --prod --audit-level=high`, plus non-blocking single-image Trivy evidence collection. These scanner jobs do not replace the affected behavior checks.

### Real OpenCode Export Acceptance

The export has a separate real-client boundary in `backend/tests/runtime/opencode_*_test.go`. With OpenCode **1.18.27** installed and the ordinary backend disposable PostgreSQL harness available, run from `backend/`:

```bash
PRISM_OPENCODE_BINARY=/absolute/path/to/opencode \
PRISM_OPENCODE_EVIDENCE_DIR=/absolute/path/to/evidence \
go test -timeout 10m ./tests/runtime -run '^TestOpenCodeProviderRoundTrip$' -count=1 -v
```

`PRISM_OPENCODE_BINARY` is required for this check. Ordinary Go test discovery skips it when the external binary is unspecified; that skip proves no client compatibility. The runner invokes the actual binary with isolated temporary configuration, environment and data directories, uses a production local Prism handler and controlled loopback SSE upstreams, and requires successful registration and text/tool/result continuation for Chat Completions, Responses, dual-native Responses, Anthropic and Gemini with both credential modes. Existing `rg` must be available to the client. It creates no product dependency and sends no real vendor requests.

Evidence includes the actual client version, generated configurations with synthetic credentials, loaded model registry, CLI events and observed HTTP exchanges. Generated gateway URLs use temporary ports and are examples rather than reusable running endpoints. Configuration parsing, model registration and completed requests are distinct assertions. The frontend mock journey and domain golden files do not replace this check.

### Real Pi Export Acceptance

The independent Pi boundary uses the actual **0.84.3** package, CLI model listing and `ModelRuntime` registration. Install that exact `@earendil-works/pi-coding-agent` version in an isolated tool directory, then run from `backend/`:

```bash
PRISM_PI_PACKAGE_DIR=/absolute/path/to/node_modules/@earendil-works/pi-coding-agent \
PRISM_PI_EVIDENCE_DIR=/absolute/path/to/evidence \
go test -timeout 10m ./tests/runtime -run '^TestPiClientRegistration$' -count=1 -v
```

Node.js must be on `PATH`. This check loads Prism-rendered files with omitted and synthetic manually entered credentials, checks all five native model shapes, and blocks network access in the client. It proves actual loading and registration; it does not claim a Pi vendor round trip. Missing package configuration skips the ordinary suite and is not acceptance evidence. Keep fixed clients and artifacts outside product dependencies. The OpenCode check above separately owns streamed text/tool continuation.

## Development Workflow

- Prism is one repository: backend, frontend, launcher, release helper, and CI are versioned together. Read the owning AGENTS hierarchy before editing a surface.
- Use [Product Scope](#product-scope) and the [retained-data and compatibility policy](#retained-data-and-compatibility-policy) for delivery scope and data commitments, and [Operations](docs/operations.md#deployed-instances) for deployment facts. The selected static development strategy below does not override them.
- Follow [Project Engineering Rules](#project-engineering-rules), [ARCHITECTURE.md](ARCHITECTURE.md), and [Source Code Size and Responsibility Rules](#source-code-size-and-responsibility-rules). UI work follows [frontend/DESIGN.md](frontend/DESIGN.md) and the checked-in `frontend/components.json`; add primitives through that existing shadcn registry into `frontend/src/components/ui/`.
- Keep canonical docs describing the implemented product and contracts; task progress and execution evidence do not belong in them.

## 设计与实现

- 围绕当前核心业务流程和正确结果设计。不为假设中的需求增加功能；已确认适用于当前工作的强制义务或约束仍须满足。适用性按实际业务判断，尚未确认不等于不适用；记录义务也不等于立即实现所有相关功能。
- 按正常、正当使用设计。保留安全、隐私和运行方面已接受的要求；额外的限流、防滥用或最坏情况处理应有明确需求或实际证据，而非默认加入。
- 不为不可能发生的情况添加错误处理、兜底或校验。信任内部代码和框架的保证，只在系统边界（如用户输入、外部 API）做校验。
- 复用已有代码和依赖。只为解决具体问题才引入 helper、额外的层、特例或抽象。
- 改动只包含必要的内容，不夹带无关清理；任务结束前移除为它创建的临时文件和调试代码。
- 修复已经核实的根因，不围绕未经验证的假设搭建绕行方案。
- 多个方案都能满足需求时，优先沿用项目中合适的做法；没有先例时参考适用的行业惯例。按职责组织源文件，只有职责难区分、行为难定位或改动难审查时才拆分；遵循项目已有的大小限制，没有限制时不用统一行数代替判断。
- 设计或审查公共 API 时，从调用方的角度评估可发现性、抗误用、错误处理、配置和演进。
- 注释解释当前的、不自明的理由、不变量、安全约束或外部系统的怪癖，不复述代码，也不记录变更历史。公共 API 文档描述契约。
- 不围绕假想套餐或配额预先收紧设计。以官方支持的宽松选项作为工作假设，注明什么观察会触发复核，不把假设写成实测事实；若具体限制决定已接受需求能否实现，在目标环境核实。

### Source Code Size and Responsibility Rules

#### Purpose

These rules constrain the responsibility boundaries and maintainable size of hand-written source code, helping developers and coding agents preserve the existing architecture, avoid responsibility sprawl, and review structure proactively before a file keeps growing.

Line count triggers a review; it never delivers the verdict. Whether to split is decided by the responsibility self-check: a long file that passes may stay, and a short file that fails must still be split. Any split must improve responsibility, dependency direction, testability, or change isolation; never break cohesion just to satisfy a number.

Line counts in these rules are physical lines, as reported by ordinary tools such as `wc -l`. Because line count only decides when a review is triggered, no greater precision is needed.

#### Scope

These rules apply to ordinary hand-written source files that carry business behavior, interaction behavior, state management, orchestration, adaptation, data access, or infrastructure behavior.

The following do not take the ordinary behavior-file size signals and self-check directly, but must still stay clear, reviewable, and compliant with the project architecture:

- Generated code that a deterministic process can regenerate;
- Database migrations and indivisible records of historical evolution;
- Schemas, protocols, catalogs, mapping tables, and other files that are primarily declarative data;
- Third-party or vendored code;
- Test snapshots, fixtures, and large test data;
- Integration tests kept cohesive by a complete narrative or end-to-end flow.

Do not evade these rules by disguising behavioral code as configuration, macros, callbacks, generated code, or data tables.

#### Responsibility Self-Check

The self-check is the gate in these rules. None of the three questions requires understanding the full business logic — each can be answered from the file's header, its exported symbols, and its call relationships — and each one points directly at a split boundary when it fails.

**1. Can the responsibility be stated in a single phrase?**

Can the file's primary responsibility be stated as one noun phrase that uses no conjunction (`and`, `or`) and does not lean on vague words such as `utils`, `helpers`, `common`, `core`, `base`, `manager`, or `misc`?

Split boundary when it fails: if a conjunction is required, each side of it is a separate responsibility — split there. If only a vague word fits, the file has no responsibility yet; move its symbols into the modules that actually own them.

**2. Do the dependencies stay within their permitted layers?**

Do the file's imports cross only the layers permitted by `ARCHITECTURE.md`? A file that directly depends on a transport framework, a persistence implementation, domain models, and an external SDK at once has already crossed layers, however short it is.

Split boundary when it fails: the out-of-layer dependencies and the code that calls them are what must be extracted — into the permitted layer, or into a new module with an explicit interface.

**3. Is the export surface cohesive?**

Do the caller sets of the file's exported symbols overlap? If the callers of symbol A and symbol B are entirely disjoint, the two do not belong to the same responsibility.

Split boundary when it fails: group the exported symbols by caller set; each disjoint group is a split boundary.

If a file cannot pass one of these questions for a verifiable structural reason over the long term, record that exception and its rationale in `ARCHITECTURE.md`; an exception must not rewrite these general rules.

#### Size Review Signals

- When a file reaches about 240 lines, review its primary responsibility, dependency direction, and natural split boundaries early, so it does not swell passively under later requirements.
- When an ordinary hand-written behavior file exceeds 300 lines, run the responsibility self-check. Passing all three questions is enough to continue, with no added justification in the change description; failing any question means splitting along the boundary that question identifies.
- When a function or method reaches about 50 lines, check whether it mixes several phases, abstraction layers, error-handling strategies, or side effects. To keep a longer function, you must be able to explain its continuity and its testable boundary.

These numbers decide only when a review is triggered, never whether to split. The logical density they correspond to varies widely across languages, so they must not be enforced as caps.

#### Responsibility and Splitting Principles

- Every file should have one clearly stateable primary responsibility and one primary reason to change.
- Split along domain boundaries, module boundaries, state lifecycles, input/output boundaries, or side-effect boundaries.
- Entry points, composition roots, and routing layers stay thin, mainly handling assembly, dispatch, and boundary conversion; do not impose a uniform line limit on them that is detached from project facts.
- New responsibilities belong in the correct existing module, or in a new module with an explicit interface; convenience of access is never a placement reason.
- Extraction should reduce cognitive load and preserve or improve dependency direction, naming, tests, and error handling.

The following mechanical splits are prohibited:

- Cutting by line count into `part1`, `part2`, or sibling files with unclear meaning;
- Collecting unrelated logic into a generic `utils`, `helpers`, or "common" module;
- Adding pass-through wrappers, proxy layers, or interfaces with no business meaning;
- Hiding complexity behind nested callbacks, macros, configuration, or generation steps;
- Moving code without forming a new responsibility boundary or test boundary.

#### Legacy Large Files

- An existing large file does not become a refactoring target for unrelated changes because of its size alone.
- When modifying a legacy large file, do not keep adding new independent responsibilities to it.
- Change history is verifiable evidence when judging whether a natural split boundary exists: if one region of the file has never been modified together with the rest across recent commits, it carries an independent reason to change, and its extent is the split boundary.
- If a safe, natural, verifiable split boundary exists near the current change, extract that boundary first and run the applicable verification.
- If splitting would widen the scope, change behavior, or lack verification conditions, keep the current modification local and explain in the change description why it was not split; never use a sweeping incidental refactor to mask the current requirement.

#### Coding Agent Requirements

Before starting to code:

1. Read `ARCHITECTURE.md`, the [project engineering rules](#project-engineering-rules), and these rules;
2. Confirm the target module, the permitted dependency directions, and the existing implementation; a new file must have an explicit owning module in `ARCHITECTURE.md` — settle that placement before implementing;
3. Check the current size and responsibility of the files you plan to modify or add.

While coding:

1. Keep checking whether you have added an independent responsibility, crossed an architectural boundary, or duplicated an existing capability;
2. Split at natural boundaries as they appear, rather than handling it mechanically once the file is finished;
3. Keep entry points and composition code thin, leaving business rules in the modules that own them.

Before finishing:

1. Run the project's applicable tests, static checks, and build verification;
2. Run the responsibility self-check on every ordinary hand-written behavior file this change added or grew past 300 lines, and list the ones that fail; if there are none, say so explicitly;
3. For each listed file, give the split boundary you will apply, or explain why it stays local in this change (see Legacy Large Files);
4. Confirm that these rules were not evaded through mechanical splitting, meaningless abstraction, or hidden complexity.

### Project Engineering Rules

Follow [Architecture](ARCHITECTURE.md) and the [shared design and implementation principles](#general-design-principles). [Product Scope](#product-scope) defines the single-operator scope, with its [requirements](#non-functional-requirements) and [v1 exclusions](#out-of-scope-v1).

#### Backend Rules (Go)

- Runtime operations are registry-allowlisted: `backend/internal/httpapi/runtime/operations.go` owns the supported method/path pairs, hook collections, and model-binding rules; unsupported or wrong-method requests reject before provider transport, telemetry, audit, feedback, or durable runtime side effects.
- `api_family` is runtime compatibility truth; catalog metadata does not participate in runtime compatibility.
- Database capacity is split into named lanes (`runtime_execution`, `runtime_telemetry`, `runtime_feedback`, `management`, `cache_refresh`, `background_jobs`); background or management work must not borrow protected proxy capacity.
- Request-path side effects stay on durable outboxes, scheduler-owned workers, or after-commit wakeups; do not put provider sends, cache invalidations, or dashboard materialization inline.
- Partitioned log retention is owned by `backend/internal/platform/logretention/` plus runtime partition ensuring; the managed tables are `request_logs`, `audit_logs`, `usage_request_events`, and `loadbalance_events`.
- Statistics must declare `ingress`, `final_execution`, or `route_attempt` caliber and use that scope's identity, outcome, latency, cost, and dataset-coverage contract. Canonical cost is `priced` plus `trusted`; route-attempt aggregates make no cost claim, and missing samples never become zero.
- Bootstrap config is plaintext and file-backed: steady-state settings prefer the startup JSON over new environment-variable knobs; `PRISM_CONFIG_PATH` and `DATABASE_URL` remain bootstrap-only env exceptions.
- The root single-image container contract (non-root `prism:prism` UID/GID `1000:1000`, writable `/app/config` ownership, default `/app/config/config.json`) is regression-backed by `backend/tests/integration/dockerfile_contract_test.go`.
- Regression layers live under `backend/tests/`: contract, integration, runtime (operation matrix, rejected routes, hook residency), and priority (DB lanes and admission) suites.

#### Frontend Rules (TypeScript/React)

- React 19 + Vite + TypeScript + Tailwind CSS 4 + shadcn/ui + TanStack Router; Node >= 24, pnpm 10.30.1.
- Any UI/UX-facing, visual, styling, layout, component, page, dialog, drawer, table, form, status/feedback, or navigation change defers to `frontend/DESIGN.md`; use `@/shared/design-system` before `@/components/ui`, and keep design-system components free of route state and API calls.
- `src/app/router/appRouter.tsx` and `src/app/router/rewriteRoutes.ts` are the source of truth for mounted routes, search schemas, and route scopes; `src/App.tsx` stays the thin wrapper.
- Keep backend access on the typed `src/lib/api.ts` boundary; management scope stays pinned to Default profile id `1` (no profile-selection UI).
- Keep the single zh-CN locale state, shared formatting, and static non-hook labels in `src/i18n/`.
- Keep shadcn/ui additions aligned with `components.json`; primitives live under `src/components/ui/`.
- Use the [runner commands and setup](#tests-checks-and-builds) and the [shared test ownership policy](#test-ownership).

#### Cross-Cutting Rules

- Each fact or policy has exactly one authoritative source; other places link to it instead of restating it.
- For ordinary removal-only validation, prefer manual confirmation over dedicated "proves not" tests; keep absence assertions only when the missing surface is a shipped contract or guardrail.
- Keep active implementation plans and live execution evidence out of `docs/`; the root `AGENTS.md` names where task state lives.
- LLM upstream changes must evaluate streaming and non-streaming coverage across operation shapes, not just provider families: OpenAI Chat Completions (`/v1/chat/completions`), Responses (`/v1/responses`), and image generations/edits (`/v1/images/generations`, `/v1/images/edits`), Gemini, and Anthropic.
- OpenAI capability authoring has two independent dimensions: `openai_accepted_format`/`openai_text_capability` (text wire protocol, strict equality) and `openai_image_operations`/`openai_image_capability` (image support, containment). Neither dimension may be used to answer for the other, and an OpenAI model or Terminal Target must declare at least one of them.
- When a change touches the launcher or deployment surface, keep `start.sh`, the root Compose bundle, and the container docs aligned (ports, same-origin proxying, bootstrap defaults).
- Do not use `v1`/`v2` generation markers (including `_v2`, `-v2`, or `V2`) in hand-written source-file names or Go/TypeScript symbols as a simple split; keeping `foo_v2` beside `foo` instead of restructuring `foo` is the mechanical split prohibited by [Source Code Size and Responsibility Rules](#source-code-size-and-responsibility-rules).
- When a second implementation is required, refactor the original and new implementation together in one change: settle the final file set and each file's single responsibility, then run the responsibility self-check on that final set; do not leave source-level generation names as the migration's steady state.
- This rule governs hand-written source-file names and Go/TypeScript symbol names, where the compilers verify references after a rename; string literals and wire values are outside this naming rule.
- This rule does not govern database identifiers: tables such as `observability_v2_upgrade_state`, columns such as `v1_accepted_outbox_count`, `CHECK`-constrained values such as `schema_version` 1/2, and persisted state values such as `draining_v1` are data. Renaming them is a migration that needs explicit authorization and a verified backup per the [retained-data policy](#retained-data-and-compatibility-policy).
- Two source generations may coexist only inside a live migration window, and the owning `AGENTS.md` must record the objective fact that ends that window; unregistered coexistence is a violation, not a pending task.

## 运营背景

- 不代项目假定经营主体、经营地点或上线市场。信息未定时保持设计与市场无关；已知地点不等于要求本地化或分析该市场的合规问题。
- 只有用户要求，或为判断、履行本次工作涉及的强制义务或已接受的约束确有必要时，才分析特定市场或运营安排。

### Product Scope

Prism is a lightweight, self-hosted application that acts as a unified proxy for multiple LLM API families. It lets a single user configure, route, and load-balance requests through a web-based management UI.

A person using models from multiple LLM API families faces:

- Managing multiple API keys and base URLs across different tools
- No unified endpoint for switching between API families
- No automatic failover when an API family is down or rate-limited
- Manual configuration changes when rotating keys or endpoints

The target user is a single operator running the application locally or on a local network, who wants to connect models, understand usage and cost, and recover from failed requests without learning Prism internals. Prism supports optional operator authentication for management APIs and proxy API keys for runtime traffic. Management configuration is pinned to frozen Default profile id `1` while runtime traffic always resolves to frozen Default profile id `1`; `X-Profile-Id` and profile fields are compatibility/storage attribution only. This is not auth multi-tenancy.

Prism is in active development, and the operator's day-to-day use of the running home-LAN instances drives priorities. There are no external users; product direction comes from that operator's own usage. It is a personal home-LAN deployment, not a public internet service: development and deployment convenience takes priority over security hardening, and optional operator login and proxy keys, plaintext bootstrap ownership, and operator-managed network exposure remain the established posture. This does not relax the [retained-data policy](#retained-data-and-compatibility-policy). Shipped behavior is described by the topic documents linked from [ARCHITECTURE.md](ARCHITECTURE.md#product-data-and-operations-contracts); UI design rules belong to [frontend/DESIGN.md](frontend/DESIGN.md).

The Requests investigation surface (`/observe/requests`, contract in [requests.md](docs/requests.md)) is scoped as follows:

- Provide a dedicated request-history route with URL-addressable state for deep investigation of a single request and for server-backed retained browse filters.
- Default to the server-side retained ingress chain as the investigation unit; show requested model identity separately from final target, selected Terminal Target, and endpoint.
- Load saved conversation content only on explicit inspection; explain missing, unsupported, or truncated capture without displaying raw diagnostic data.
- Support the implemented drill-down entry points from dashboard overview and recent activity.
- Do not replace dashboard or statistics summaries, do not re-derive retained chains client-side (chain grouping is server-owned and paged by ingress with signed cursors), and do not change frozen Default-profile runtime routing for `/v1/*` and `/v1beta/*`.

### Non-Functional Requirements

| Requirement | Target |
| --- | --- |
| Deployment | Root Compose runs one Prism app image plus PostgreSQL. The app packages the Go backend behind Nginx. The local launcher runs PostgreSQL, backend, and optional Vite frontend. Go's `time.LoadLocation` must be able to resolve configured IANA timezones in the runtime image. An unresolvable timezone excludes the affected scheduled Terminal Target and can produce `terminal_target_schedule_unresolvable`. Recheck timezone availability when changing the base image. |
| Authentication | Optional operator auth for `/api/*`; optional proxy API keys for `/v1/*` and `/v1beta/*` |
| Latency overhead | Target below 50 ms of added proxy latency |
| Concurrent requests | Target at least 10 simultaneous proxy requests |
| Database | PostgreSQL (Go-managed startup migrations) |
| API standard | [HTTP API references](ARCHITECTURE.md#api-contracts) |
| CORS | Local launcher traffic stays same-origin through the Vite proxy in `full` mode; standalone frontend workflows can still target an explicit backend base URL |

### Out of Scope (v1)

- Auth-based multi-tenancy, multi-operator RBAC, and per-user data isolation beyond the single-operator auth surface and profile namespace isolation
- Usage-based billing/accounting integrations beyond Prism's built-in telemetry and costing reports
- Global runtime rate limiting outside per-Terminal Target `qps_limit` and in-flight limits
- Routing-schedule extensions beyond recurring weekly windows: calendar date ranges, holiday calendars, and cron expressions; a separate enable switch that keeps the windows while suspending them (clearing the schedule deletes the windows); an audit trail for schedule changes
- External secret-manager integrations beyond Prism's built-in endpoint-secret encryption at rest
- Proxy API key scoping. A key is an instance-wide credential: it can call every enabled direct-entry model through that model's accepted registered operations, and direct entries enabled later are automatically in scope. v1 deliberately ships this single scope instead of per-model, per-Endpoint, per-API-family, per-operation, rate, token, or spend limits on an individual key, and the Proxy API Keys page states the limitation in the UI rather than leaving it implicit. Introducing scopes later would touch the key data model and its migration, the runtime authorization path, the management API and its UI, and would require deciding the default scope for keys that already exist.

## 兼容与迁移

- 默认不为假想调用方保留旧实现、别名、回退、双读写或格式转换。明确要求的兼容性，以及仍受支持的调用方、升级或恢复契约必须保留。
- 替换实现时，在授权范围内同步调用方、配置、派生契约、测试和文档，移除已退役路径；不借机扩大清理范围。
- 只有非生产数据受影响时，表结构或数据的改动直接切换，不为保留这些数据写分阶段或可回滚迁移、回填、双读双写或数据转换。
- 已确认尚无生产部署，且无升级、恢复或保留义务依赖历史时，可在获准修改迁移的任务中重写、合并或重新生成基线。出现生产部署配置、部署作业、生产环境文件、发布记录或用户说明时，重新核对前提。
- 已有生产部署时，保证在生产运行的迁移正确，不改写或合并已发布的迁移。本地库可丢弃不代表可以删除迁移历史。更新部署事实本身不改变现行迁移政策。

### Retained Data and Compatibility Policy

Prism’s running LAN databases and their matching bootstrap files are retained state, listed under 生产标识 in the root `AGENTS.md`. They are not disposable workspaces, and the development tier does not make them so. Apply direct schema changes to disposable development data only when the migration and restore contracts allow it.

- Recovery requires a PostgreSQL backup and the matching bootstrap file; an inventory listing alone does not verify restorability.
- Startup applies the checked-in migration sequence and its guards. An old database is not assumed compatible merely because the current version starts on a fresh database. In particular, `000023_pricing_template_kind_cards.sql` refuses retained old pricing/currency-migration rows before its destructive shape change; any rebuild requires explicit authorization and a verified backup.
- Later catalog bindings, Pi bind-time identity, output-rate evidence, Terminal Target upstream identity, direct-entry qualification, and request-scoped reroute status codes have data-preserving migration contracts. Historical gaps remain explicit rather than being fabricated from current configuration. Detailed schemas and evidence semantics belong in the [data model](docs/data-model.md).
- The [direct-entry reclassification runbook](docs/operations.md#direct-request-entry-reclassification), its `scripts/operations/direct-request-entry-reclassification.sql` companion, and the integration acceptance test ship in source. The procedure is operator-invoked only and never runs during startup; its existence or the 000032 migration stamp does not prove that it has been applied to an instance.
- Bootstrap settings are file-backed and restart-applied. Valid existing files are preserved. The removed `runtime.transport` block is rejected; mail and exporter telemetry fields are parse-only compatibility inputs. The full startup contract is in [Startup config file](docs/operations.md#startup-config-file).
- Machine contracts (the operation registry, the file-backed bootstrap v1 schema, the partitioned log-retention tables) evolve within their documented ownership and stay covered by backend regression tests. Bootstrap v1 is preserve-only for `runtime.secretEncryptionKey`, parse-only for mail compatibility fields, and restart-required for external file edits.
- The no-shim convention governs code, API, and schema shape. A shape change that cannot carry existing data forward needs an explicit decision and a verified backup first.
- Allowed: minimal, correct changes within the existing architecture and documented boundaries; clean-architecture-first upgrades that replace legacy shapes rather than wrapping them in compatibility layers; evolution of existing contracts (operation registry, DB lanes, partitioned retention, durable outboxes) within their documented ownership and with the applicable regression coverage.
- Prohibited without explicit authorization: destructive data resets or data loss (a verified backup is also required); deleting, moving, or reorganizing existing documents or machine contracts; external deployment, publication, or release actions.

## 验证改动

- 优先运行能覆盖改动行为的最小范围检查。用户或项目要求全量回归，或更窄的检查不足以验证改动时，再运行全套。通过后，只有后续修改、失败或尚未解决的实质风险才需要重跑或扩大范围。
- 写通用且正确的实现，不写只能让检查通过的实现：不硬编码测试期望值，不为让检查通过而削弱测试、屏蔽真实错误或删除必要行为。测试本身有错，或要求不合理、不可行时，直接指出，不绕过。
- 优先在可观察的行为边界上复用已有覆盖。不写只复述实现或只为提高覆盖率的测试，除非该行为本身是契约。
- 修复缺陷时，若能在可观察的行为边界上复现，补一个修复前失败的回归测试。
- 预期行为有意改变时，同步更新对应测试；这不属于削弱测试。
- 测试结果须确定，不依赖执行顺序，也不依赖外部服务，除非被测对象就是该集成。
- 不添加或保留只为证明某次编辑、删除或清理已完成的测试，例如搜索源码文本，或检查某个路径、字段、符号已不存在；这类工作靠审查或任务结束前删除的临时检查确认。表达持久可观察契约的负向测试要保留，例如鉴权、拒绝非法输入、禁止的副作用和兼容性保证。

### Test Ownership

- Keep ownership single-layer: process-local backend unit tests own pricing, planning, and stream classification without a database; DB contract suites own one API surface; frontend Vitest/lib owns pure frontend logic; browser journeys own cross-page interaction. Do not duplicate a behavior across layers or add INSERT-then-SELECT mirror tests.
- Keep setup before the first act within 10 lines; use defaulted builders when it grows. Share PostgreSQL through package `TestMain` and template cloning. Do not run container creation, `go build`, or `go run` inside test functions.
- Use baseline-plus-override helpers or golden files when expectations exceed eight fields. Use golden files for large shapes, assert only the fields relevant to the behavior, and use normalized schema dumps instead of per-column DDL assertions.
- Table-drive three or more cases with the same act/assert shape; keep at most one narrative story per resource.
- Test Prism behavior rather than platform internals or dependency output. Do not grep production Go/TypeScript source, assert chart-library rendering, manually recalculate aggregate internals, or use browser specs for table-cell text and i18n fallback checks.
- Keep the existing Playwright budget near five journeys and replace a journey when adding one. This is a change budget, not a claim about the current spec count; reuse the existing journey and evidence fixtures.
- Removal-only assertions normally expire with the removal; retain them when absence is a shipped contract, such as route parity or the Dockerfile contract.
- Retained tests must run in CI or document why they do not. Use runner globs rather than hand-written file lists. Feature test-line additions must not exceed product-line additions; do not use plan-numbered test names, and remove the duplication a new helper replaces in the same change.

Run the [affected checks](#tests-checks-and-builds) after choosing the regression layer. The [ingress-chain read benchmark](backend/tests/perf/README.md) is a separate, manually run performance check with disposable PostgreSQL and a synthetic corpus.

## 界面文案

- 删掉一句话若不会让用户做错事，也不会丢失同屏其他地方找不到的信息，就不必保留。同一事实只说一次，放在最接近相关操作的位置。
- 只有会改变用户行动时才显示提示。正常状态不挂横幅；空区块和未启用功能的区块不渲染。
- 不在界面解释数据模型、校验顺序、配置名、内部标识或算法；标签足够清楚时，不另加文字解说控件。
- 精简解释，不删保护措施。保留不可逆操作或金融操作的明确确认、同屏唯一展示的值，以及承担实际功能的文字。

The [design system](frontend/DESIGN.md) owns required empty, unavailable, and recovery states, including the cues that let operators act.

## 提交约定

- 每个提交只包含相关改动，保持可独立审查。
- 使用 Conventional Commits：`<type>: <description>`。类型为 `feat`、`fix`、`chore`、`refactor`、`docs`、`test`、`build`、`ci` 或 `perf`。仅在有助于理解时加 scope，写作 `<type>(<scope>): <description>`。
- 描述仅反映实际差异，使用英文小写祈使语气，末尾不加句号。主题尽量不超过 72 个字符；一般只写主题，必要时再加正文。

## 拉取请求（PR）

- 标题沿用项目的提交主题格式；包含多个提交时，选最能概括整体变更的表述。提交格式含类型时，类型以整体变更为准。
- 正文用英文，按以下顺序组织，省略没有内容的部分：
  - `Summary`：一至三句话说明改了什么及原因。
  - `Changes`：列出主要变更，必要时按领域分组。明确指出数据模式、迁移、配置、环境变量和公共 API 的变化；破坏性变更以 `**Breaking:**` 开头。
  - `Testing`：列出实际运行的命令及结果；未运行检查时写 `Not tested`。与本次改动无关的失败，写明失败项与现象。
  - `Notes`：仅写审查者需要了解的限制、合并后的手动步骤或需重点审查的事项。
- 仅依据分支差异和提交撰写正文；只在分支名、提交或请求提及时引用关联议题。不写工期、工作量估算或人员安排。

## Releases

Run the [release helper](release.sh) from the repository root with Git, Go, Node.js, pnpm, and a reachable `origin`. `./release.sh patch --dry-run` previews the file changes and commands. It checks the four current versions, requires a forward version and an unused tag, and queries `origin` for that tag without pushing.

An authorized live release uses `./release.sh patch`, `./release.sh minor`, `./release.sh major`, or `./release.sh X.Y.Z` from a clean `main` that is current with `origin/main`. It updates `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` together, runs the backend entrypoint tests, verifies backend version metadata, builds the frontend, and then commits, tags, and pushes. Instance upgrades are a separate operator procedure.

`.github/workflows/docker-images.yml` publishes only `ghcr.io/coachpo/prism` for `linux/arm64`. A `v*` tag requires green CI for its commit and moves `latest`; manual workflow dispatch does not move `latest`. `.github/workflows/cleanup.yml` only prunes untagged single-image container versions. See [Operations](docs/operations.md) for deployed-instance inspection, backup, upgrades, and recovery.

## Current Development Strategy

**Development tier: `MVP`**

Complete the smallest observable end-to-end loop around the confirmed core value, scope, non-goals, acceptance conditions, and exit conditions in [Product Scope](#product-scope), [Non-Functional Requirements](#non-functional-requirements), and [Out of Scope (v1)](#out-of-scope-v1). This tier permanently forgoes active investment in security, privacy, data, credential and key management, compatibility, audit/monitoring/SLO, and regulatory compliance requirements.

### Must Complete at This Tier

- Complete the core user flow, visible result, and error paths directly required by core acceptance.
- Run affected-path tests, checks, and builds sufficient to make the core conclusion observable and repeatable.

### Not Pursued by Default

- Permanently forgo active investment in security, privacy, data, credential and key management, permission-system expansion, compatibility layers and full compatibility regression, audit/monitoring/SLO, and regulatory compliance requirements; do not pursue non-core features, repository-wide default gates, high availability, or production hardening.
- Do not add general capabilities, abstractions, dependencies, or non-primary business branches for unverified needs.

### Non-negotiable Boundaries

- Explicit user requirements, an accepted GOAL, hard project rules or invariants, repository-required checks, and explicit prohibitions in the [retained-data and compatibility policy](#retained-data-and-compatibility-policy) remain effective and are not affected by the exemption; existing compatibility commitments are existing contracts and are not deleted by this tier.
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

Implementations must comply with the project architecture facts in [`ARCHITECTURE.md`](ARCHITECTURE.md), the [project- and technology-specific rules](#project-engineering-rules), and the [Source Code Size and Responsibility Rules](#source-code-size-and-responsibility-rules).

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