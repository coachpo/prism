# 测试精简交接文档（开发者执行版）

> **状态：已全部执行并合并至 main（merge d6b6be1d，2026-07-09）。本文为历史执行记录。**
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement batch-by-batch. Steps use checkbox (`- [ ]`) syntax.
>
> 决策依据与批次理由见 [TEST_REDUCTION_PLAN.md](TEST_REDUCTION_PLAN.md)；证据与行号出处见 [TEST_SUITE_REDUCTION.md](TEST_SUITE_REDUCTION.md)。本文假设执行者对仓库零上下文——照步骤做即可，**每一步都写了验证命令和预期输出**。行号为 2026-07-08 快照，执行时以符号名/文件名为准。

## 约束（每个批次隐含遵守）

- **C1 基线**：`codex/prism-core-simplification` 分支；生产实例 `http://192.168.1.222:8088` 有真实数据。本交接**只动测试与 CI 接线，不动任何生产代码、不动数据库**（唯一例外：批次 3 的 TD4 行为化测试可能需要读生产代码来定断言，但不修改它）。
- **C2 标准验证**（下文简称「标准验证」）：

  ```bash
  cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/... && go build ./cmd/prism-backend
  cd frontend && pnpm run build && pnpm run lint && pnpm exec vitest run && pnpm run test:lib && pnpm run test:server
  ```

- **C3 ⚠️ 三个 TestMain 陷阱**（删文件前必查）：contract 套件的 `TestMain` 与共享 harness 在 `backend/tests/contract/auth_control_plane_test.go`（:45,:123,:1143）；runtime 套件的在 `backend/tests/runtime/profile_scope_test.go`（:150）。**删除或大改这两个文件不属于本交接**（分别归 IMPLEMENTATION_PLAN 的 Task 8/Task 9，已含"先抽 harness"步骤）。本交接的任何步骤若发现要动它们——停下，确认没走错文件。
- **C4 glob 语义**：`frontend/package.json` 的 `test:lib` 是 glob（`tests/lib/*.test.mjs` + `tests/model-detail/*.test.mjs`）——**放进这两个目录的任何 `.test.mjs` 自动进 CI；删除文件即自动摘除**，无需改脚本。
- **C5 归属边界（本交接不删，留给对应任务）**：Task 12/R7 的前端实时测试已随功能退役删除，不再列为待办或保留项；`dashboard_routing_flow_layout_contract.test.mjs`（→ Task 13/R8）；`profile_selection_contract.test.mjs`、`profile_scope_header_contract.test.mjs`（→ Task 9/R4）；`tests/priority/outbox/email_outbox_priority_test.go`（→ Task 8/R3）；`tests/contract/s15_observability_contract_test.go:558-574`（→ Task 11/R6）；`model_dialog_i18n_contract.test.mjs` 的改写（→ Task 25/T8）。剩余项测的是仍然活着的功能，提前删会丢真守卫。
- **C6 删除 PR 必附「覆盖去向表」**：每删一个测试文件，PR 描述里一行：`文件 → 语义去向（哪个存活测试拥有该覆盖 / 为何无需覆盖）`。压缩 PR 必附「用例映射表」：压缩前后 `rg -c "func Test" <file>`（Go）或 `rg -c "^test\(|^  test\(" <file>`（TS）的数字对比 + 被合并用例的去向说明。
- **C7 提交信息**：删除批 `test: <批次内容>`；CI 接线 `ci: <内容>`；压缩批 `test: compress <目标文件> (<before>→<after> LOC)`。

---

## 批次 0：基线核实（半小时）

- [x] **0.1** 确认 CI 当前绿：`gh run list --branch codex/prism-core-simplification --limit 3`（或看 GitHub Actions 页）。红的先修再开工。
- [x] **0.2** `cd frontend && pnpm install --frozen-lockfile`——后续所有前端验证的前提。
- [x] **0.3** 复核 TD5（一条命令，预期已覆盖）：

  ```bash
  rg -n "loadbalance/strategies" backend/tests/contract/s11_management_contract_test.go | head -5
  ```

  预期：出现列表/详情/变更调用（已于 2026-07-08 核实存在）。若为空——停，把批次 4 的 4.3 改为"先移植 1 个 CRUD 用例再删"。
- [x] **0.4** 记录基线数字（写进第一个 PR 描述，供验收对比）：

  ```bash
  find backend -name '*_test.go' | xargs wc -l | tail -1
  find frontend/tests frontend/src -name '*.test.*' -o -name '*.spec.ts' | xargs wc -l | tail -1
  ```

---

## 批次 1：e2e 大清洗 + Playwright 进 CI（TD2/TD3）

**Files:**

- Delete: keeper 白名单之外的全部 spec（2026-07-08 快照时 32 个；IMPLEMENTATION_PLAN 的 R2/R4/R8/R9a 若已先行落地，部分已不存在——跳过即可；R7 项已先行删除）
- Modify: `frontend/tests/e2e/shared-chart-statistics.spec.ts`（压缩）、`frontend/tests/e2e/dashboard-aggregate-fixtures.ts`（接收 fixture）、`.github/workflows/ci.yml`（新 job）、`frontend/tests/e2e/AGENTS.md`
- Keep（恰好 5 个 keeper + 1 个 fixture 文件）: `auth-session-lifecycle.spec.ts`、`loadbalance-strategies-recovery.spec.ts`、`models-access-target-authoring.spec.ts`、`request-log-dedicated-audit-page.spec.ts`、`shared-chart-statistics.spec.ts`、`dashboard-aggregate-fixtures.ts`

- [x] **1.1** 删除 `frontend/tests/e2e/` 下除 5 个 keeper、`dashboard-aggregate-fixtures.ts`、`AGENTS.md` 之外的**全部** spec。**操作以 keeper 白名单为准**（例如 `find frontend/tests/e2e -name '*.spec.ts' | grep -v -E 'auth-session-lifecycle|loadbalance-strategies-recovery|models-access-target-authoring|request-log-dedicated-audit-page|shared-chart-statistics' | xargs git rm`），下方清单只是 2026-07-08 快照（32 个），供覆盖去向表对照——个别文件可能已被并行推进的 R 任务先删，属正常：

  ```text
  dashboard-aggregate-overview  dashboard-reporting-currency
  dashboard-routing-shell  loadbalance-strategy-defaults
  model-access-targets  model-detail-access-target-authoring  model-detail-connection-dialog-probe
  model-detail-request-logs-handoff  models-request-logs-reporting-currency
  pricing-template-policy-removal  profile-scope-bootstrap  profile-scope-route-headers
  protected-shell-sidebar  proxy-key-lifecycle  reporting-currency-provider
  request-log-audit-disabled-state  request-log-detail-copy  request-log-proxy-api-key-detail
  request-logs-filter-options-loading  request-logs-optional-zero  request-logs-target-model-columns
  request-logs-token-rate  request-logs-ttft  settings-audit-controls  settings-log-retention
  settings-reporting-currency-save  settings-startup-tab  settings-user-agent-client-rules-copy
  statistics-filtered-totals  statistics-proxy-api-key-label  statistics-token-rate  statistics-ttft
  ```

  （全部带 `.spec.ts` 后缀。）注意两组易混名：**保留** `models-access-target-authoring`、**删除** `model-access-targets` 与 `model-detail-access-target-authoring`。
- [x] **1.2** 覆盖去向表（C6，直接抄进 PR）：reporting-currency 四件套 → lib `costing_reporting_currency_contract` + `reporting_currency_contract`；TTFT/token-rate 四件套 → 单元格格式化归 seam 层，无浏览器义务；audit-disabled → keeper `request-log-dedicated-audit-page` 已含 disabled 分支；model-access-targets → lib `management_api_model_targets_contract`；settings-audit-controls → vitest `AuditConfigurationAPIFamilyCard.test.tsx`；startup-tab/profile-scope/routing-shell/probe → 功能本身在 R2/R4/R8/R9a 中移除（**同时在 IMPLEMENTATION_PLAN 对应 Task 的 e2e 删除步旁标注"已于测试精简批次 1 提前完成"**）；其余 → 政策规则 2（浏览器不断言单元格文本/i18n）。
- [x] **1.3** TD3 压缩 `shared-chart-statistics.spec.ts`（884 → ≤400 行）：删 `.sisyphus/evidence` 写残留（:12-22、:642 两处）；删 recharts 内部断言（:812 贝塞尔路径正则、:702-703 坐标轴刻度启发式）；~450 行内联 fixture 移入 `dashboard-aggregate-fixtures.ts` 并导入。保留：图表渲染、粒度切换、空态、tooltip 交互的行为断言。
- [x] **1.4** 本地全量跑一次 keeper：`cd frontend && pnpm exec playwright install chromium && pnpm run test:e2e`——预期 5 个 spec 全过。有挂的先修（它们可能从未在此环境跑过）。
- [x] **1.5** ci.yml 的 `frontend-seams` job 末尾（`Lint frontend` 步骤之后）加：

  ```yaml
      - name: Install Playwright chromium
        working-directory: frontend
        run: pnpm exec playwright install chromium --with-deps

      - name: Run frontend journey specs
        working-directory: frontend
        run: pnpm run test:e2e
  ```

  （5 个 spec 全量跑即可，无需过滤参数；`test:e2e` 已在 P0 被修正为裸 `playwright test`。）
- [x] **1.6** 更新 `frontend/tests/e2e/AGENTS.md`：写明"恰好 5 个 journey spec、封顶、加一个须删一个"（政策规则 2）。
- [x] **1.7** 验证：`ls frontend/tests/e2e/*.spec.ts | wc -l` → **5**；标准验证；推分支看 CI 绿（含新 job）。
- [x] **1.8** 提交：`test: prune e2e to 5 journey specs and wire them into CI`

---

## 批次 2：前端 lib/vitest 清理 + vitest 进 CI

- [x] **2.1** 删除 4 个文件（⚠️ 对照 C5——本批**不含** flow-layout/profile 那 3 个；R7 项已删除）：
  - `frontend/tests/lib/dashboard_contract.test.mjs`（107 行，13 个对源码的 `doesNotMatch` 正则钉——tsc 拥有此职责）
  - `frontend/tests/main/main_entrypoint_structure.test.mjs`（39 行，孤儿，从未被任何脚本引用）
  - `frontend/tests/loadbalance/ban_policy_schema_contract.test.mjs`（72 行，孤儿，与 vitest `banPolicySchemas.test.ts` 用例标题逐一相同）
  - `frontend/tests/loadbalance/loadbalance_strategy_form_state_contract.test.mjs`（184 行，孤儿）

  随后若 `frontend/tests/main/`、`frontend/tests/loadbalance/` 目录空了，连目录一起删。
- [x] **2.2** 局部瘦身（删测试不删文件）：`dashboard_bootstrap_contract.test.mjs` 删 5 个 `doesNotMatch` + :118 的源码正则（−~80 行，保留契约形状断言）；`management_contract.test.mjs` 删 3 个 removed-concept 测试（−~60 行）；`model_dialog_i18n_contract.test.mjs` 删 4 个 `doesNotMatch`（其余归 T8）。
- [x] **2.3** vitest 进 CI：ci.yml `frontend-seams` job 的 `Run frontend seam tests` 步骤**之前**加：

  ```yaml
      - name: Run frontend unit tests
        working-directory: frontend
        run: pnpm exec vitest run
  ```

- [x] **2.4** 验证：`pnpm run test:lib`（glob 自动收窄）+ `pnpm exec vitest run` 全绿；`rg -l "doesNotMatch" frontend/tests/lib` 只剩 `model_dialog_i18n_contract.test.mjs`（0 个更好，若 T8 已完成）。
- [x] **2.5** 提交：`test: drop source-regex lib tests and orphans, wire vitest into CI`

---

## 批次 3：后端元测试清除 + TD4 行为化（在 CI 的套件，逐步跑绿再往下）

- [x] **3.1 TD4 行为化先行**（先立新的、再拆旧的）。在 `backend/tests/priority/` 新增两个行为测试（各 ~50 行，模板：同目录现存的 admission/load 行为测试的 harness 用法）：
  - `scheduler_behavior_test.go`：通过 `platform/background` 调度器的注册表（`production.go:428-439` 的注册环所建）断言注册的 worker 名字集合 == 预期集合（当前：management side effects、log retention、feedback pipeline 等——以 `rg -n "WorkerName" backend/internal/platform` 的实际清单为准）。这替代 `scheduler` grep 文件的"归属"语义。
  - `after_commit_behavior_test.go`：开启事务 → 触发一次会入队副作用的写 → **回滚** → 断言 outbox 表无新行；再重复一次并**提交** → 断言有行。这替代 after-commit grep 的语义。表名与入队函数用 `rg -n "EnqueueTx" backend/internal/platform/managementsideeffects` 定位。
- [x] **3.2** 整删 6 个纯 grep 文件：`backend/tests/priority/` 下 `async`(78 行)、`cache`(77)、`failure`(106)、`scheduler`(125)、`integration`(86)、`auditstats`(138) 六个 `*_priority_test.go`（先 `rg -ln "strings.Contains" backend/tests/priority` 确认即这六个 + outbox<归R3> + 局部三处）。
- [x] **3.3** 局部删 grep：`priority/db` 的 :92-103（~50 行）、`priority/sideeffects` 的 :20-30（~25 行）、`priority/unit` 的源码钉剩余（~30 行；:111 那条归 T5，若 T5 未做则一并删并在 T5 任务旁注记）。保留这些文件中的行为部分（admission 80 行、load 87 行、unit 非钉部分 ~118 行、sideeffects 子测试 ~110 行、log_retention 20 行）。
- [x] **3.4** 删文档措辞测试：`backend/tests/contract/s2_shell_test.go` 的 `TestNormativeDocsParity`（:99 起 ~122 行）与 `TestServedDocsSurfaceRemoved`（:57，11 行）。**保留** `dockerfile_contract_test.go`（政策例外，部署产物契约）。
- [x] **3.5** 删 Postgres 内部行为测试：`tests/integration/partitioned_log_retention_test.go` 的 `TestLogPartitionToastDiagnostics` + `task12*` helper（:155-200 起 ~200 行）；`tests/integration/migrations_test.go:1364-1459` 的 TOAST/reloptions 断言（~200 行）。
- [x] **3.6** 删 phase7 的 SQL 文本 grep：`tests/integration/management_audit_stats_phase7_test.go` 的 `TestManagementAuditQueryUsesBoundedIndex`(:253-268) 与 `TestManagementAuditNoBroadCount`(:270-279)。
- [x] **3.7** 删重构时代残留：`tests/runtime/` 下 `phase0_baseline_bench`(331)、`phase0_bench`(294)、`phase1_bench`(148)、`phase3_bench`(19) 四个 bench 文件、phase2/phase3_streaming 内嵌的 Benchmark 函数（~110 行）、`runtime_phase0_query_proof_test.go`(520)。**保留** phase1_snapshot/phase2_local_state/phase3_hot_path 的不变量测试。
- [x] **3.8** 清空壳：删 `tests/runtime/responses_translation_streaming_test.go`（1 行包声明壳）；`tests/contract/model_vendor_helpers_test.go`（8 行桩）——⚠️ 先 `rg -n "$(rg -o 'func \w+' backend/tests/contract/model_vendor_helpers_test.go | cut -d' ' -f2)" backend/tests/contract` 确认无人调用再删，有人调用则内联；两份 `log_partition_helpers_test.go` 合一（98+47→~60，保留被两个套件引用的那份的位置）。
- [x] **3.9** s11 proves-not 三连折叠：`s11_management_contract_test.go` 的 :423/:455/:482 三个 `TestLoadbalanceStrategyRejects*` 折成一个 ~20 行 `t.Run` 表（保留三种已删策略类型的拒绝断言各一行）。
- [x] **3.10** 验证：`rg -ln "strings.Contains" backend/tests/priority` → 0（行为文件除外，人工确认剩余 Contains 是对响应体的断言而非源码）；`cd backend && go test ./tests/...` 全绿；`rg -c "func Benchmark" backend/tests -r` → 0。
- [x] **3.11** 提交：`test: replace source-grep meta-tests with behavioral equivalents, drop refactor-era residue`

---

## 批次 4：跨层去重——包内管理路由测试（先移植、后删除）

- [x] **4.1 移植**（去向 `backend/tests/contract/endpoint_contract_test.go`，仿其 :23-196 现有用例风格）：从 `backend/internal/httpapi/management/endpoints/routes_test.go` 移植两个独有断言——(a) 密钥掩码：创建端点后 GET 响应中 key 字段 == `********`（原 :47）；(b) 边界状态冻结（原 :140-143）。移植后跑 `go test ./tests/contract -run Endpoint` 确认绿。
- [x] **4.2** 删 `backend/internal/httpapi/management/endpoints/routes_test.go`（402 行）。
- [x] **4.3** 删 `backend/internal/httpapi/management/loadbalance/routes_test.go`（339 行；TD5 已核实 s11 覆盖 CRUD——见批次 0.3）。（IMPLEMENTATION_PLAN Task 21 原计划在此文件加 handler 用例——已同步改指 `tests/contract/s11_management_contract_test.go`。）
- [x] **4.4** `backend/internal/httpapi/management/settings/routes_test.go`：**只删路由/HTTP 测试部分（:136 起）**，保留 :34-135 的纯单元校验（那部分无 DB、正是 TD1 要进 CI 的形态）。
- [x] **4.5** 删 `backend/internal/httpapi/management/models/promotion_target_test.go` 与 `store_test.go` 的剩余部分（合计 ~450 行；契约孪生在 `model_contract_test.go:272,:338-491`）。⚠️ `store_test.go:494` 附近的 seed 若被别的测试文件引用，先移到公共 helper。
- [x] **4.6** `platform/managementjobs/` 与 `platform/logretention/` 的包内 DB 测试：`rg -ln "Postgres|pgxpool" backend/internal/platform/managementjobs backend/internal/platform/logretention --glob '*_test.go'` 枚举；其中**需要真 DB 的用例迁往 `tests/integration/`**（批次 7 之后有 template-DB 基建，成本低），纯逻辑用例留在包内。目标：`internal/` 下不再有任何自起 Docker/外连 DB 的测试（TD1 前置）。
- [x] **4.7** 验证：`go test ./internal/... ./cmd/...` 在**无 Docker、无 DATABASE_URL** 的环境下全绿（可 `docker ps` 确认没起容器）；`go test ./tests/contract` 绿。
- [x] **4.8** 提交：`test: dedupe in-package management route tests into contract suite`

---

## 批次 5：TD1 落地——包内测试进 CI

**前置：批次 4 完成（4.7 的无 Docker 验证过了）。**

- [x] **5.1** ci.yml `backend-regression` job 的 `Run backend regression suites` 步骤**之前**加（先跑快的）：

  ```yaml
      - name: Run backend unit tests
        working-directory: backend
        run: go test ./internal/... ./cmd/...
  ```

- [x] **5.2** 推分支，确认新步骤 <2 分钟且绿。此后 `runtime_test.go`（定价数学）与 `server_test.go:460`（路由契约第二守卫）首次真正守护提交。
- [x] **5.3** 提交：`ci: run backend in-package unit tests`

---

## 批次 6：大文件压缩（13 项，每项独立 PR，附 C6 用例映射表）

> 通用手法四件套：**基线构造器**（`base(overrides)` 替代粘贴的字面量墙）、**`t.Run` 表**（≥3 个同形用例）、**golden 文件**（形状断言）、**共享 harness helper**（重复 arrange）。每项压缩后跑该套件 + 标准验证。
> ⚠️ 6.3/6.4/6.5 建议排在 IMPLEMENTATION_PLAN 的 R6（Task 11）与 T5（Task 22）之后——那两个任务会删这些文件里的段落，先压缩会白干。

- [x] **6.1** `internal/httpapi/runtime/operation_translation_{request,stream,response}_test.go` + `response_translation_execution_metadata_test.go` + `gateway/provider/openai/translation_parity_test.go`（合计 2,961 行）→ 并入 `operation_translation_golden_test.go` 的 golden 机制（自更新开关 `PRISM_UPDATE_OPENAI_TRANSLATION_GOLDENS`，见 :20；规范化 JSON 比对 :211-222）：请求/响应形状加表行、流式加 `.sse` golden 转写；`TestBuildRequestPlan_*` 家族（request 文件 :427-784）折成 ~15 行选择表。目标 ~1,060 行。
- [x] **6.2** `tests/runtime/request_logs_contract_test.go`（2,722 行）→ 写 `wantPricedRow(mutate func(*row))` 基线构造器替代 6 处 28 行字面量墙（:651-677、:741-769、:899-927、:957-1034）；4 个组件定价测试并入既有表 `TestRuntimeRequestLogPreservesUnpricedPricingPathways`(:931-1082)；两个孪生 SQL 扫描加载器（:2019/:2064）按表名参数化。目标 ~1,400 行。
- [x] **6.3**（R6 后）`tests/contract/s15_observability_contract_test.go`（2,055 行）→ 种子构造器替代 30 字段单行字面量（:214,:379,:438,:492）；仪表盘/快照形状改 golden JSON；拆 :223,:391-396,:512-514 的 8 路 `||` 断言墙；删 :269-369 的 INSERT→SELECT 镜像测试（写路径归 tests/runtime）。目标 ~1,100 行。
- [x] **6.4**（T5 后）`tests/integration/management_audit_stats_phase7_test.go`（1,847 行）→ 与 s15 跨套件去重：phase7 独有用例（staleness/缓存模式 :1025-1217、keyset 分页、删除任务租约）迁入 contract 套件；删与 s15 重复的 dashboard 形状/stats-summary/audit 窗口用例（对照行号见 TEST_SUITE_REDUCTION §4）。目标 ~1,100 行且随批次 7 共享容器。
- [x] **6.5**（可与 6.4 同 PR）`tests/integration/migrations_test.go`（1,844 行）→ ⚠️ **先把 `newPostgresHarness`(:455-459) 挪到同包 `harness_test.go`（整包在用）**；然后用一份规范化 `pg_dump --schema-only` golden diff 替代 :553-1844 的逐列 DDL 镜像 helper；保留行为测试（脏库 :230、noop :282、stamped 升级 :314、backfill :156/:195）与已应用版本清单断言。目标 ~600 行。
- [x] **6.6** `tests/integration/startup_test.go`（1,612 行）→ `sync.Once` 编译一次二进制替代 6 次 `go run`/`go build`（:159,:164,:175,:183,:728）；:85-152 的 68 行标量种子墙改 golden 行转储。目标 ~900 行。
- [x] **6.7** `tests/runtime/proxy_selector_test.go`（1,353 行，核心路由矩阵，**只压不删**）→ `routeSpec` 构造器替代 ~30 处种子级联（模式见 :22-32,:120-127,:168-182）；:600-786 五个结构等同的策略测试折成 `{strategy, targetOrder, wantSequence}` 表；双 harness 块共享 `runBothPlanServices`。目标 ~950 行。
- [x] **6.8** `internal/httpapi/runtime/runtime_test.go`（2,243 行，定价数学主力，**只压不删**）→ `basePricedResult(overrides)` 替代 5 处 25 行期望墙（:1581-1603,:1626-1648,:1660-1682,:1714-1736）；3 个工厂块（:115-124,:152-161,:190-199）共享 helper；48 个平铺 Test 函数按域表化。目标 1,400–1,900 行。
- [x] **6.9** `tests/contract/s11_management_contract_test.go`（1,234 行）→ `putThenGetJSON` helper（`TestGlobalLogRetentionSettingsAndJobs`:286-378 用 92 行测 4 个整数）；**:260-284 的路由契约 parity 测试一字不动**。目标 ~850 行。
- [x] **6.10** `tests/contract/model_contract_test.go`（1,178 行）→ :26-270 的 245 行故事测试拆成 ~80 行 happy path（`postModel` helper）+ 10 用例校验表；:401-540 同法。目标 ~700 行。
- [x] **6.11** 契约套件启动去重：7 份 ~50 行启动拷贝（`endpoint_contract_test.go:242`、`model_contract_test.go:698`、`s11:938`、`s15:1405`、`connection_s10:340`、`profile_scope:359`<若 R4 已删则跳过>、`auth_control_plane:1204,:1272`）折成一个 `newContractHarnessFor(t, prefix, opts)`（放 `tests/contract/harness_test.go`——若 Task 8 已建此文件则并入）。净省 ~400 行。
- [x] **6.12** `tests/runtime/runtime_phase3_streaming_first_test.go`(393) 并入 `runtime_streaming_buffering_test.go`(623)：用量保持（:23 vs :81）与缓冲回退（:190 vs :233）重复用例合并，文件删除。净省 ~250 行。顺手把并入后的文件按政策规则 10 改名去 `phase` 前缀。
- [x] **6.13** `internal/platform/lifecycle/app_test.go`（662 行）→ 1 个可配置假服务替代 :84,:126,:169,:187 的 5 假件动物园。目标 ~540 行。另：幸存 `tests/lib/*.mjs` 迁入 vitest、退役 `loadTsModule.mjs` 的事项**推迟到 R8/T8 落地后**（届时幸存清单才稳定），在 `frontend/tests/AGENTS.md` 记一行待办即可。

---

## 批次 7：集成套件基建——41 容器 → 1 + template-DB

- [x] **7.1** 给 `backend/tests/integration` 建包级 `TestMain`（放 6.5 建立的 `harness_test.go`）：起**一个** Postgres 容器（模式照抄 contract 套件 `TestMain` 的容器管理，见 `auth_control_plane_test.go:123` 或其抽出后的 harness 文件），跑一次全量迁移建 `template1_prism` 模板库。
- [x] **7.2** 把逐测试的 `docker run`（`migrations_test.go:455-471` 的 `newPostgresHarness` 调用者，全套件 41 处）改为 `CREATE DATABASE test_<n> TEMPLATE template1_prism`——每测试一个隔离库、零容器启动成本。⚠️ 例外：`migrations_test.go` 中**测迁移过程本身**的用例（脏库/noop/stamped/backfill）需要空库或特定版本库，给它们保留"从空库跑迁移"的路径（同容器内 `CREATE DATABASE` 不带 TEMPLATE 即可）。
- [x] **7.3** `launcher_startup_contract_test.go` 的 docker compose 全栈测试**保留原样**（它是 start.sh/compose 部署契约的唯一守卫，价值在真实栈）。
- [x] **7.4** 验证：`cd backend && time go test ./tests/integration` ——容器启动从 41 次降到 1 次，本地耗时应显著下降（记录 before/after 到 PR）；`docker ps -a | grep -c postgres` 测试期间 ≤2。
- [x] **7.5** 提交：`test: single shared postgres + template-db cloning for integration suite`

---

## 批次 8：政策固化

- [x] **8.1** 把 [TEST_SUITE_REDUCTION.md](TEST_SUITE_REDUCTION.md) §6 的 10 条政策（含两条常设例外：路由契约 parity、Dockerfile 契约）浓缩写入 `backend/tests/AGENTS.md` 与 `frontend/tests/AGENTS.md` 的 CONVENTIONS 段；根 `AGENTS.md` 的 COMMANDS 段更新为含 vitest/e2e 的新验证命令集（即 C2）。
- [x] **8.2** IMPLEMENTATION_PLAN.md 附录 A 第 7 条更新为"测试精简已执行，见 TEST_REDUCTION_HANDOFF.md"。
- [x] **8.3** 提交：`docs: codify test policy into AGENTS conventions`

---

## 完工判定（Definition of Done）

1. `ls frontend/tests/e2e/*.spec.ts | wc -l` == 5，且 CI 含 Playwright job 并绿。
2. `go test ./internal/... ./cmd/...` 无 Docker 环境下绿，且在 CI 中执行。
3. `rg -ln "strings.Contains" backend/tests/priority` 中不再有对生产源码路径的 `os.ReadFile`（`rg -l "os.ReadFile" backend/tests --glob '*_test.go'` 复核，例外仅 dockerfile_contract 与 golden 文件读取）。
4. `rg -c "func Benchmark" backend -r` == 0。
5. 集成套件单容器（7.4 验证）。
6. 批次 0.4 与完工时的 LOC 对比 ≥ −21k（本交接范围）。
7. 两个 AGENTS.md 含测试政策；所有 PR 均附覆盖去向表/用例映射表。
