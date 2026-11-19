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

This check loads Prism-rendered files with omitted and synthetic manually entered credentials, checks all five native model shapes, and blocks network access in the client. It proves actual loading and registration; it does not claim a Pi vendor round trip. Missing package configuration skips the ordinary suite and is not acceptance evidence. Keep fixed clients and artifacts outside product dependencies. The OpenCode check above separately owns streamed text/tool continuation.


## Development Workflow

- Prism is one repository: backend, frontend, launcher, release helper, and CI are versioned together. Read the owning AGENTS hierarchy before editing a surface.
- Use [STATUS.md](STATUS.md) for current lifecycle, deployment, data, and compatibility facts and [docs/product-contract.md](docs/product-contract.md) for delivery scope. The selected static development strategy below does not override them.
- Follow [docs/development-rules.md](docs/development-rules.md), [ARCHITECTURE.md](ARCHITECTURE.md), and [docs/source-code-size-and-responsibility-rules.md](docs/source-code-size-and-responsibility-rules.md). UI work follows [frontend/DESIGN.md](frontend/DESIGN.md) and the checked-in `frontend/components.json`; add primitives through that existing shadcn registry into `frontend/src/components/ui/`.
- Keep active plans and execution evidence under ignored `artifacts/`, while canonical docs describe the implemented product and contracts.

## 设计与实现

- 围绕当前核心业务流程和正确结果设计。不为假设中的需求增加功能；已确认适用于当前工作的强制义务或约束仍须满足。适用性按实际业务判断，尚未确认不等于不适用；记录义务也不等于立即实现所有相关功能。
- 按正常、正当使用设计。保留安全、隐私和运行方面已接受的要求；额外的限流、防滥用或最坏情况处理应有明确需求或实际证据，而非默认加入。
- 不为不可能发生的情况添加错误处理、兜底或校验。信任内部代码和框架的保证，只在系统边界（如用户输入、外部 API）做校验。
- 改动只包含必要的内容，不夹带无关清理；任务结束前移除为它创建的临时文件和调试代码。
- 修复已经核实的根因，不围绕未经验证的假设搭建绕行方案。
- 设计或审查公共 API 时，从调用方的角度评估可发现性、抗误用、错误处理、配置和演进。
- 注释解释当前的、不自明的理由、不变量、安全约束或外部系统的怪癖，不复述代码，也不记录变更历史。公共 API 文档描述契约。
- 不围绕假想套餐或配额预先收紧设计。以官方支持的宽松选项作为工作假设，注明什么观察会触发复核，不把假设写成实测事实；若具体限制决定已接受需求能否实现，在目标环境核实。

## 运营背景

- 不代项目假定经营主体、经营地点或上线市场。信息未定时保持设计与市场无关；已知地点不等于要求本地化或分析该市场的合规问题。
- 只有用户要求，或为判断、履行本次工作涉及的强制义务或已接受的约束确有必要时，才分析特定市场或运营安排。

## 兼容与迁移

- 默认不为假想调用方保留旧实现、别名、回退、双读写或格式转换。明确要求的兼容性，以及仍受支持的调用方、升级或恢复契约必须保留。
- 替换实现时，在授权范围内同步调用方、配置、派生契约、测试和文档，移除已退役路径；不借机扩大清理范围。
- 已确认尚无生产部署，且无升级、恢复或保留义务依赖历史时，可在获准修改迁移的任务中重写、合并或重新生成基线。出现生产部署配置、部署作业、生产环境文件、发布记录或用户说明时，重新核对前提。
- 已有生产部署时，保证在生产运行的迁移正确，不改写或合并已发布的迁移。本地库可丢弃不代表可以删除迁移历史。更新部署事实本身不改变现行迁移政策。

Prism’s running LAN databases are retained data. The [project data and compatibility policy](STATUS.md#data) controls changes that could lose state. Apply direct schema changes to disposable development data only when the migration and restore contracts allow it.

## 验证改动

- 优先运行能覆盖改动行为的最小范围检查。用户或项目要求全量回归，或更窄的检查不足以验证改动时，再运行全套。通过后，只有后续修改、失败或尚未解决的实质风险才需要重跑或扩大范围。
- 写通用且正确的实现，不写只能让检查通过的实现：不硬编码测试期望值，不为让检查通过而削弱测试、屏蔽真实错误或删除必要行为。测试本身有错，或要求不合理、不可行时，直接指出，不绕过。
- 优先在可观察的行为边界上复用已有覆盖。不写只复述实现或只为提高覆盖率的测试，除非该行为本身是契约。
- 修复缺陷时，若能在可观察的行为边界上复现，补一个修复前失败的回归测试。
- 预期行为有意改变时，同步更新对应测试；这不属于削弱测试。
- 测试结果须确定，不依赖执行顺序，也不依赖外部服务，除非被测对象就是该集成。
- 不添加或保留只为证明某次编辑、删除或清理已完成的测试，例如搜索源码文本，或检查某个路径、字段、符号已不存在；这类工作靠审查或任务结束前删除的临时检查确认。表达持久可观察契约的负向测试要保留，例如鉴权、拒绝非法输入、禁止的副作用和兼容性保证。

Use [the project test-ownership rules](docs/development-rules.md#test-ownership) to choose the regression layer, then run the [affected checks](#tests-checks-and-builds).

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

`./release.sh patch --dry-run` previews a release. Live `./release.sh patch` (or `minor`, `major`, or an explicit version) requires release authorization: it keeps `VERSION`, `backend/VERSION`, `frontend/VERSION`, and `frontend/package.json` aligned, verifies backend version metadata and the frontend build, commits, tags, and pushes the root release. The helper requires clean, current `main` and a forward version; it does not deploy an instance.

`.github/workflows/docker-images.yml` publishes only `ghcr.io/coachpo/prism` for `linux/arm64`. A `v*` tag requires green CI for its commit and moves `latest`; manual workflow dispatch does not move `latest`. `.github/workflows/cleanup.yml` only prunes untagged single-image container versions. See [Operations](docs/operations.md) for deployed-instance inspection, backup, upgrades, and recovery.

<!-- write-project-docs:shared-contributing:start -->
## Current Development Strategy

**Development tier: `MVP`**

Complete the smallest observable end-to-end loop around the confirmed core value, scope, non-goals, acceptance conditions, and exit conditions in [`docs/product-contract.md`](docs/product-contract.md). This tier permanently forgoes active investment in security, privacy, data, credential and key management, compatibility, audit/monitoring/SLO, and regulatory compliance requirements.

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

Implementations must comply with the project architecture facts in [`ARCHITECTURE.md`](ARCHITECTURE.md), the project- and technology-specific rules in [`docs/development-rules.md`](docs/development-rules.md), and the unified size and responsibility rules in [`docs/source-code-size-and-responsibility-rules.md`](docs/source-code-size-and-responsibility-rules.md).

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
