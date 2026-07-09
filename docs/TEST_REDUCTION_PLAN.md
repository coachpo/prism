# Prism 测试精简实施方案（决策与批次）

> 生成日期：2026-07-08 · 依据：[TEST_SUITE_REDUCTION.md](TEST_SUITE_REDUCTION.md)（审计与证据）。
> 执行细节见交接文档 [TEST_REDUCTION_HANDOFF.md](TEST_REDUCTION_HANDOFF.md)——开发者按交接文档逐批执行，本文只回答"为什么这么定"。

## 1. 五项决策（已裁决）

| # | 决策 | 裁决 | 理由 |
|---|------|------|------|
| TD1 | 包内测试进不进 CI | **进**。清掉包内 Docker 型测试后，backend job 增加 `go test ./internal/... ./cmd/...` | "不在 CI 的测试"本身就是技术债——定价数学主力（`runtime_test.go` 2,243 行）和路由契约守卫（`server_test.go:460`）目前不守护任何提交。落地后分层清晰：`internal/` = 纯进程内单元层（无 DB、秒级），`tests/` = DB 契约/集成层 |
| TD2 | Playwright 去向 | **5 个 journey keeper 进 CI（chromium 单浏览器、单 job），其余 34 个全删，不保留"仅本地"spec** | 本仓的教训就是"仅本地"套件会烂成 1.5 万行死重（e2e 从 CI 摘除后无人维护）。5 个 keeper 对应政策规则 2 的五条旅程：请求日志+审计页（`request-log-dedicated-audit-page`）、模型编排（`models-access-target-authoring`）、统计图表（`shared-chart-statistics`，压缩后）、故障转移恢复（`loadbalance-strategies-recovery`，产品核心）、认证壳（`auth-session-lifecycle`，登录在 R3 step 1 后存活） |
| TD3 | `shared-chart-statistics.spec.ts`（884 行）去留 | **压缩保留（目标 ≤400 行）**，充当统计图表 journey keeper | 它是统计核心唯一的真浏览器走查（recharts 无法在 jsdom 渲染）；删掉则五条旅程缺一条。压缩内容：删 `.sisyphus/evidence` 写残留（:12-22,:642）、删 recharts 内部断言（贝塞尔正则 :812、坐标轴启发式 :702-703）、~450 行内联 fixture 移入 `dashboard-aggregate-fixtures.ts` |
| TD4 | 两个有真实语义的源码 grep（scheduler 归属、after-commit） | **用行为测试重写（合计 ~100 行）后，所有源码 grep 元测试全删** | 这两条语义是仓库架构红线（副作用必须走 outbox/调度器、不得请求路径内联），值得保；但源码 `strings.Contains` 是错误的表达方式——重构即误报、行为退化照过。行为化后 priority/ 从 1,301 行缩到 ~530 行且全部测行为 |
| TD5 | 删包内 `loadbalance/routes_test.go` 前的契约覆盖核实 | **已核实：策略 CRUD 契约覆盖在 `tests/contract/s11_management_contract_test.go`**（`/api/loadbalance/strategies` 列表/详情/变更均有用例） | 包内拷贝（339 行，不在 CI）可直接删，无需移植 |

## 2. 目标测试架构（终态）

```
后端
├── internal/**/_test.go      纯进程内单元（定价/规划/翻译/流分类），无 DB，进 CI    [TD1]
├── tests/runtime             写路径契约（代理请求→日志/用量落库），共享 1 容器
├── tests/contract            读/管理面契约 + 路由契约 parity 守卫，共享 1 容器
├── tests/integration         迁移/启动/部署产物，1 容器 + template-DB 克隆（现在是 41 容器）
└── tests/priority            行为化后的调度/准入/租约核（~530 行，无源码 grep）

前端
├── src/**/*.test.tsx (vitest)   纯组件/schema 逻辑，进 CI                        [TD1]
├── tests/lib (node --test)      对后端契约 JSON 的漂移守卫（源码正则钉全删）
├── tests/server                 部署产物健康入口（保持不动）
└── tests/e2e                    恰好 5 个 journey spec，进 CI（chromium）        [TD2]
```

**每个行为只在一层被测**（政策规则 1）；**留下来的全部在 CI 执行**（规则 9）；政策全文 10 条见 [TEST_SUITE_REDUCTION.md](TEST_SUITE_REDUCTION.md) §6，随批次 8 写入 `backend/tests/AGENTS.md` 与 `frontend/tests/AGENTS.md` 固化。

政策常设例外（写入规则 7/8 注记）：路由契约 parity 双守卫（`s11:260` + `server_test.go:460`，TD1 落地后两份都在 CI）；`dockerfile_contract_test.go`（部署产物 non-root 契约，AGENTS 约定明文依赖它）。

## 3. 批次总览（细节见交接文档）

| 批次 | 内容 | 预期净变化 | 与 IMPLEMENTATION_PLAN 的关系 |
|------|------|-----------|------------------------------|
| 0 | 基线核实（CI 绿、TD5 复核、覆盖去向表模板） | 0 | 无 |
| 1 | e2e 大清洗：34 个 spec 删除、TD3 压缩、Playwright CI job | −~13.5k | 提前吸收 R2/R4/R7/R8/R9a 各自的 e2e 删除步（变为 no-op，已注记） |
| 2 | 前端 lib/vitest 清理 + vitest 进 CI | −~450 | R7/R8 的三个 lib 文件**不在本批**（归 Task 12/13） |
| 3 | 后端元测试清除 + TD4 行为化重写 | −~2.8k | s15:558 归 R6（Task 11 已注记）；outbox grep 归 R3 |
| 4 | 跨层去重：包内管理路由测试删除（先移植 2 个独有断言） | −~1.7k | 无 |
| 5 | TD1 落地：包内测试进 CI（`go test ./internal/... ./cmd/...`） | +1 行 CI | 依赖批次 4 完成（清 Docker 依赖） |
| 6 | 大文件压缩（golden 折叠、基线构造器、故事测试拆表等 13 项） | −~7k | s15/phase7/migrations 三项建议排在 R6 与 T5 之后，避免改了又删 |
| 7 | 集成套件基建：41 容器 → 1 + template-DB | 0 行，CI 分钟数最大收益 | 无 |
| 8 | 政策固化进 AGENTS.md | +~40 | 无 |

批次 0–5 可立即开工（P0 与 R1 已在当前分支落地）；批次内任务顺序有讲究（交接文档标注），批次之间 1/2/3/4 可并行。

## 4. 验收标准

| 指标 | 现状 | 目标（本方案范围） | 叠加 R/T 计划后 |
|------|------|------|------|
| 测试总行数 | ~78k | ~57k（−21k：删 ~14k + 压缩 ~7k） | ~45k |
| 不在 CI 的测试行数 | ~39k | **0**（删除或接入） | 0 |
| CI Docker 容器数/次 | 44 | ~4 | ~3 |
| 源码/文档文本 grep 型测试 | ~2k 行 | 0（例外：路由契约 parity、Dockerfile 契约） | 同左 |
| e2e spec 数 | 39（0 在 CI） | 5（全在 CI） | 5 |
| 新增测试守卫 | — | `./internal/...`、vitest、5 个 e2e journey 进 CI | 同左 |

每个删除 PR 必附「覆盖去向表」、每个压缩 PR 必附「用例映射表」（模板见交接文档 §约束），这是防止"删着删着把真覆盖删没了"的唯一审查抓手。
