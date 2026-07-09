# Prism 测试精简建议

> 生成日期：2026-07-07 · 基线：`codex/prism-core-simplification` 分支（P0 快赢与 R1 已落地）。
> 本文是 [DEVELOPMENT_DIRECTION.md](DEVELOPMENT_DIRECTION.md) 的测试专项续篇；删减动作与 [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) 交叉引用，已排期项不重复计数。全部结论附 `path:line` 证据。

## 0. 一句话结论

全仓约 **78k 行测试代码**中，**约 39k 行不在任何 CI 任务里执行**（后端包内 22.7k、Playwright 15.5k、vitest 816、孤儿 295）——一半的"测试资产"实际是只有编译检查的死重。在 CI 里跑的那一半，则背着 **44 个 Docker 容器、约 293 次建库+迁移+起应用**的基建成本，且大量行数花在三类低价值形态上：**跨层重复覆盖**（同一功能最多被 5 个套件测）、**元测试**（断言文档措辞、生产源码文本、Postgres/图表库内部行为，约 2,000 行）、**风格啰嗦**（无基线的 25 字段字面量墙、复制粘贴的 arrange 级联、每测试一个容器）。建议：在已排期删减（约 11.4k）之外**再删约 13k、压缩净省约 6–8k**，把存活的测试全部纳入 CI，并立 10 条书写政策防止回弹。

## 1. 测试地产全景

### 后端（CI 入口：`.github/workflows/ci.yml:30`）

| 套件 | 文件 | LOC | 在 CI？ | 基建成本 |
|---|---|---|---|---|
| `tests/contract` | 12 | 9,446 | ✅ | 共享 1 个 PG 容器（TestMain 在 `auth_control_plane_test.go:123`）；约 117 次完整起应用；**约 50 行的启动序列被复制粘贴 7 份（~470 行）** |
| `tests/integration` | 9 | 7,654 | ✅ | **41 个逐测试 Docker Postgres 容器**（`migrations_test.go:455-471`）+ 逐测试真实 `docker compose` 栈（`launcher_startup_contract_test.go:191,:266`）+ `startup_test.go` 内 6 次编译器调用 |
| `tests/runtime` | 29 | 16,947 | ✅ | 共享 1 个 PG 容器（TestMain 在 `profile_scope_test.go:150`）；**约 176 次完整起应用**；约 1,290 行共享 harness 被困在 `profile_scope_test.go` 里 |
| `tests/priority` | 13 | 1,301 | ✅ | 无容器；**约 56%（≈735 行）是对生产源码做 `strings.Contains`** |
| `internal/` 包内 `_test.go` | 78 | 22,120 | ❌ **不在任何 CI** | 本地跑时 6 个包各自起私有 Docker PG；含定价数学的主力测试 `runtime/runtime_test.go`（2,243 行）与 G4 路由契约守卫 `platform/http/server_test.go:460` |
| Benchmark | 19 个函数 | ~900 | ❌ 无任何 `-bench` 调用 | 只被编译、从不执行 |

### 前端（CI 入口：ci.yml:57-69）

| 套件 | 文件 | LOC | 在 CI？ |
|---|---|---|---|
| `tests/e2e`（Playwright） | 39 spec + fixture | 15,043 | ❌ **无任何 workflow 跑 Playwright**；且所有 spec 全量 mock（`page.route("**/*")`）+ 仅 Vite webServer（`playwright.config.ts:21`）——**永远抓不到前后端漂移** |
| `tests/lib` + `tests/model-detail`（node --test） | ~25 | ~4,700 | ✅（`test:lib` glob）；22/25 依赖脆弱的 `tests/helpers/loadTsModule.mjs` 动态转译 |
| `tests/loadbalance` + `tests/main` | 3 | 295 | ❌ 从未被任何脚本引用的**孤儿** |
| `src/` vitest（`pnpm run test`） | 12 | 816 | ❌ |
| `tests/server` | 1 | 136 | ✅ 唯一测真实部署产物的测试——**保留** |

**治理性发现：处置经济学被"是否在 CI"劈成两半。** 不在 CI 的 39k 行删除零风险（它们本来就不守护任何提交）；在 CI 的部分，问题主要是重复与基建浪费，而非数量。

## 2. "繁琐啰嗦"的三个来源（定性）

1. **跨层重复覆盖**：同一行为最多被 5 个套件重复测。极端例子——日志保留（log retention）被 5 个套件 7 个文件覆盖（`s11:286`、`s15:837-1318`、`phase7:36-203`、`integration/partitioned_log_retention` 941 行、`runtime_partitioned_logs`、`priority/log_retention`、外加一个源码 grep）；仪表盘/统计读取被 s15 与 phase7 各测一遍；端点/模型/策略 CRUD 在包内 `routes_test.go` 与 `tests/contract` 各一份**同等成本**的拷贝（包内那份还不在 CI）。
2. **元测试（不测行为）**：约 2,000 行在断言"文本长什么样"而非"系统做什么"——对生产源码 `strings.Contains`（priority 六个整文件 + `phase7:253-279` 把 SQL 当字符串 grep）、断言文档措辞（`s2_shell_test.go:99` 检查 API_SPEC/ARCHITECTURE 里的短语）、测 Postgres 自身（TOAST/reloptions，`partitioned_log_retention_test.go:155-200`、`migrations_test.go:1364-1459`）、用正则断言 recharts 的贝塞尔路径（`shared-chart-statistics.spec.ts:812`）、6 个前端 .mjs 用 `readFileSync`+`assert.match` 钉源码结构。这类测试**行为退化时照样通过、任何重构都会误报**。
3. **风格啰嗦**（12 个最大文件尸检的 Top-5 反模式，按行数成本）：

| # | 反模式 | 证据 | 估算行数 |
|---|---|---|---|
| 1 | Schema/第三方镜像测试——迁移 DDL 被逐列复述（`migrations_test.go:553-1844`）、源码 grep、Postgres/recharts 内部 | 见 §2.2 | ~1,800 |
| 2 | 无基线字面量墙——20–30 字段的期望结构体整段粘贴、每份只差 2–5 个字段（`request_logs_contract_test.go` 6×28 行、`runtime_test.go` 5×25 行、s15 单行 30 字段种子） | `request_logs_contract_test.go:651-1034`、`runtime_test.go:1581-1682` | ~1,600 |
| 3 | 复制粘贴的 arrange 级联——种子链 ~30 次（proxy_selector）、7 份契约启动拷贝（~470）、6+2 份包内 docker harness 拷贝（~700）、38/39 个 e2e spec 重复 mock 前导（中位数 ~290 行，最差 709） | `proxy_selector_test.go:22-32` 等 | 核心 ~1,300 + 全仓 ~3,200 |
| 4 | 逐测试重型资源供给——41 个容器、6 次编译、~40 处 15 行内联建库块 | `migrations_test.go:455-471`、`startup_test.go:159-183` | ~700 行但是 **CI 分钟数第一大户** |
| 5 | 重复/过期覆盖——s15 里 INSERT→SELECT 镜像（写路径归 tests/runtime 管）、给早已删除的策略做 proves-not（`s11:423-505`）、legacy 404 探测 | `s15:269-369` | ~600 |

## 3. 新增删除清单（已排期项不在内；按 LOC÷风险排序）

| # | 目标 | LOC | 为何可删 |
|---|---|---|---|
| 1 | **20 个低价值 e2e spec**（reporting-currency ×4、TTFT/token-rate ×4、audit-disabled、detail-copy、aggregate-overview、proxy-key label ×2、policy-removal、strategy-defaults、settings-audit-controls、model-access-targets、UA-rules-copy 等，单文件 93–625 行） | **~5,684** | Playwright 本就不在 CI，零守护损失；语义已由**在 CI 的** lib 测试（costing/reporting_currency/dashboard_bootstrap/observability_api/model_targets 契约）与 vitest（`AuditConfigurationAPIFamilyCard.test.tsx`）持有；浏览器里断言单元格文本/i18n 兜底本就是错层 |
| 2 | **5 个前端 .mjs**：`dashboard_routing_flow_layout_contract`(1,406，可抢救 ~80–400 行 graph/inspector/mobile 部分)、`dashboard_contract`(107)、`main_entrypoint_structure`(39)、`loadbalance/ban_policy_schema_contract`(72，与 vitest `banPolicySchemas.test.ts` 标题级重复)、`loadbalance_strategy_form_state_contract`(184) | **~1,808** | R7 的 websocket/realtime lib 契约测试已随 Task 12 删除；剩余 flow_layout 是 **R8 的 CI 级漏网伤亡**，后四个是源码正则钉/孤儿/重复 |
| 3 | **包内 Docker 版管理路由测试**（先移植 3 个未覆盖断言到 tests/contract）：`endpoints/routes_test.go`(402)、`loadbalance/routes_test.go`(339)、`settings/routes_test.go` 路由部分(~250，保留 :34-135 纯单元校验)、`models/` 剩余(~450)、managementjobs/logretention harness 合并(~250) | **~1,700** | 与 `tests/contract` 的孪生（`endpoint_contract_test.go:23-196`、`s11:152-166`、`model_contract_test.go:272,338-491`）**同成本同层**，但包内那份不在 CI。先移植：密钥掩码 `********`（`routes_test.go:47`）、边界状态冻结（:140-143）、strategy CRUD（见 §8 待核实项） |
| 4 | **重构时代的 phase 基准与证明**：phase0_baseline_bench(331)、phase0_bench(294)、phase1_bench(148)、phase3_bench(19)、phase2/3 内嵌 bench(~110)、`runtime_phase0_query_proof_test.go`(520) | **~1,400** | ~900 行 Benchmark 从未被执行（全仓无 `-bench`）；query-proof 是给一场已完结迁移出具的"证明"，有效断言已由 phase1/2/3 不变量测试接管 |
| 5 | **源码 grep 元测试**：priority 六个整文件（async 78、cache 77、failure 106、scheduler 125、integration 86、auditstats 138）+ 局部 grep（db ~50、sideeffects ~25、unit ~30）+ `s2:99 TestNormativeDocsParity`(~122) + `s15:558-574`(18) + `phase7:253-279` SQL 子串 | **~880** | 行为退化照过、重构必炸；priority/ 里值得保留的行为核约 430 行（admission 80、load 87、unit 非钉部分 ~118、sideeffects 子测试 ~110、log_retention 20）。scheduler 归属/after-commit 两个 grep 若想保语义，用行为等价测试重写（见 §8 决策） |
| 6 | **Postgres 内部行为测试**：`TestLogPartitionToastDiagnostics` + task12 helper（~200）、`migrations_test.go:1364-1459` TOAST/reloptions 断言（~200） | **~400** | 测的是 Postgres 不是 Prism |
| 7 | **空壳与残桩**：`responses_translation_streaming_test.go`（1 行包壳）、`model_vendor_helpers_test.go`（8 行返回 `1` 的桩）、两份重复的 `log_partition_helpers_test.go` 合一（98+47→~60）、`dockerfile_contract_test.go`(51，边缘——5 行 `USER` 正则可替) | **~150** | 可平凡替代 |
| 8 | **s11 proves-not 三连**（`:423/:455/:482` 拒绝早已删除的策略类型）→ 折成一个 ~20 行表 | ~60 净 | 每策略一行表项即可保留拒绝断言 |

**小计：新增可删约 12.9k 行**（其中约 8.3k 本就不在 CI）。

## 4. 合并压缩清单（保留覆盖、砍掉行数）

> ⚠️ 各行估算部分重叠（尤其 s15/phase7 两行 vs 它们的跨套件合并行），不可直接求和。

| 文件 | 手法 | 前 → 后 |
|---|---|---|
| `internal/httpapi/runtime/operation_translation_{request,stream,response}_test.go`(805/645/424) + `response_translation_execution_metadata_test.go`(495) + `openai/translation_parity_test.go`(592) | 并入现成 golden 机制（`operation_translation_golden_test.go` 仅 260 行、自更新开关 `PRISM_UPDATE_OPENAI_TRANSLATION_GOLDENS`:20、规范化 JSON 比对 :211-222）：加表行 + `.sse` golden 转写；`TestBuildRequestPlan_*` 家族（:427-784，8 行 setup 复制 ×N、只差 3 个值）折成 ~15 行选择表 | 2,961 → **~1,060** |
| `tests/runtime/request_logs_contract_test.go` | `wantPricedRow(mutate)` 基线构造器替代 6×28 行字面量墙（:651-677 等四处）；4 个组件定价测试并入既有表 `TestRuntimeRequestLogPreservesUnpricedPricingPathways`(:931-1082)；孪生 SQL 扫描加载器按表参数化（:2019 vs :2064）；推广好模式 `loadRequestFixture`(:54) | 2,722 → **~1,400** |
| `tests/contract/s15_observability_contract_test.go` | 带默认值的种子构造器替代 30 字段单行字面量（:214,:379,:438,:492）；仪表盘/快照形状改 golden JSON；拆 8 路 `\|\|` 断言墙（:223,:391-396,:512-514）；**删 INSERT→SELECT 镜像测试**（:269-369，写路径归 tests/runtime） | 2,055 → **~1,100** |
| `tests/integration/management_audit_stats_phase7_test.go` + **s15↔phase7 跨套件去重** | 包级 TestMain + 单容器 + **template database**（`CREATE DATABASE x TEMPLATE ...`）替代 ~40 处 15 行内联建库（:36-52 等）；phase7 独有用例（staleness/缓存模式 :1025-1217、keyset/EXPLAIN、删除任务租约）并入契约套件；删与 s15 重复的 dashboard 形状（:298 vs s15:419,:433,:590）、stats-summary（:683,:709 vs s15:399,:642）、audit 窗口（:1254 vs s15:1073） | 1,847 → **~1,100**，另净杀跨套件重复 ~700–900 行，省最多 17 个容器 |
| `tests/integration/migrations_test.go` | 用一份规范化 `pg_dump --schema-only` **golden diff** 替代 ~1,300 行逐列 DDL 镜像 helper（:553-1844，如 :710-745）；保留行为测试（脏库 :230、noop :282、stamped 升级 :314、backfill :156/:195）。⚠️ 先把 `newPostgresHarness`(:455-459) 挪到 `harness_test.go`——整包在用它 | 1,844 → **~600** |
| `tests/integration/startup_test.go` | 二进制每包编译**一次**（`sync.Once`）替代 6 次 `go run`/`go build`（:159-183,:728）；68 行标量种子墙改 golden 行转储；共享容器 | 1,612 → **~900** |
| `tests/runtime/proxy_selector_test.go`（核心路由矩阵，**保留**） | `routeSpec` 构造器替代 ~30 处 8–10 行种子级联；5 个结构等同的策略测试体（:600-786）折成 `{strategy, targetOrder, wantSequence}` 表；双 harness 块共享 `runBothPlanServices` | 1,353 → **~950** |
| `internal/httpapi/runtime/runtime_test.go`（定价数学主力） | `basePricedResult(overrides)` 替代 5×25 行期望墙（:1581-1736）；3 个工厂块共享 helper；48 个平铺函数表化。**并把 `go test ./internal/...` 加进 CI——否则这根定价支柱什么都不守护** | 2,243 → **~1,400–1,900** |
| `tests/contract/s11_management_contract_test.go` | `putThenGetJSON` helper（`TestGlobalLogRetentionSettingsAndJobs`:286-378 用 92 行测 4 个整数）；拒绝策略表化；**G4 parity 测试 :260-284 一字不动** | 1,234 → **~850** |
| `tests/contract/model_contract_test.go` | 245 行故事测试 `TestModelCRUD`(:26-270) 拆成 ~80 行 happy path（`postModel` helper）+ 10 行校验表；target-rejection(:401-540) 同法 | 1,178 → **~700** |
| 契约启动去重 | 7 份 ~50 行启动拷贝（endpoint:242、model:698、s11:938、s15:1405、s10:340、profile:359、auth:1204/:1272）折成一个 `newContractHarnessFor(t, prefix, opts)` | ~470 → ~80 |
| `runtime_phase3_streaming_first_test.go` → 并入 `runtime_streaming_buffering_test.go` | 用量保持/缓冲回退重复用例合并（:23 vs :81；:190 vs :233） | −~250 |
| `platform/lifecycle/app_test.go` | 1 个可配置假服务替代 5 假件动物园（:84-187，~200 行） | 662 → **~540** |
| e2e 幸存者前导 | 共享 bootstrap fixture：38/39 个 spec 重复 mock auth+`profiles/bootstrap`、27 个私有 `createProfile()`、前导中位数 ~290 行 | 12 个幸存者共 **−~2,000–2,500** |
| `loadTsModule.mjs` 退役 | 幸存 .mjs 契约测试迁入 vitest（TS/alias/mock 原生支持）；`node --test` 只留 server 起停测试 | 净 ~90 行 + 消除整类脆弱性 |
| 集成套件供给方式 | 41 个逐测试 `docker run` → 1 套件容器 + template DB（contract/runtime 已是此模式）；`launcher_startup_contract_test.go` 的 compose 全栈跑法决定去 PR 门禁还是 nightly | 0 行，**CI 分钟数最大单项收益** |

## 5. 重叠矩阵（同一功能 N 份拷贝 → 留哪份）

| 功能 | 拷贝数 | 保留 |
|---|---|---|
| 日志保留 | 5 套件 / 7 文件 | 运行时写路径 + **一个**契约读套件；s11/s15/phase7 的任务生命周期三重奏是纯重复 |
| 仪表盘/统计/审计读取 | s15 + phase7 | 契约套件；phase7 独有用例并入后整删 |
| 端点 CRUD | 包内 vs contract | **contract**（在 CI，同成本）；先移植掩码/边界断言 |
| 设置审计路由 | 包内 vs s11 | **contract**；包内保留纯单元校验 :34-135 |
| 模型 target 拒绝 | 包内 vs contract | **contract** |
| 策略 CRUD | 包内 vs s15 | 先核实 s15 是否真覆盖 CRUD（现在只见 current-state/events :1156-1300）再折 |
| 能力矩阵 | 包内单测 vs `operation_route_matrix_translation_test.go` | **反例——留便宜的包内单测**（全仓唯一金字塔方向正确的地方），harness 矩阵留作冒烟 |
| 流式 | streaming_buffering vs phase3_streaming_first | 合并 |
| 生成参数 | 包内解析 vs 运行时持久化 | 互补，都留 |
| 路由矩阵正/负 | runtime vs integration 负例 | 都留 |
| 路由契约 JSON parity | s11:260（CI）+ `server_test.go:460`（不在 CI） | 双守卫是计划 G4 有意为之——但注意**只有 s11 那份实际执行**；CI 决策（§8）落定后统一 |
| 前端 reporting-currency | 4 个 e2e（1,384 行）+ 2 个 lib | lib |
| 前端 TTFT/输出速率单元格 | 4 个 e2e（1,352 行） | 全删 |
| 前端审计状态 | 3 个 e2e（1,495 行，含重复的 disabled 测试） | 只留 `request-log-dedicated-audit-page` |
| 前端 ban policy schema | .mjs 孤儿 vs vitest（标题一模一样） | vitest |

## 6. 测试书写政策（10 条，防回弹）

1. **三层各司其职，一个行为只测一层**：(a) 进程内单元测试管定价/规划/流分类（`internal/`，不碰 DB）；(b) 每个 API 面**一个** DB 契约套件（`tests/runtime` 管写路径、`tests/contract` 管读/管理面）；(c) vitest/lib 管纯前端逻辑。禁止 INSERT→SELECT 镜像。
2. **Playwright 封顶 ~5 个旅程 spec**（请求日志、审计页、模型编排、统计图表、认证壳）；加新 spec 必须删旧的；浏览器里**永不**断言表格单元格文本或 i18n 兜底——那归 seam 测试管。
3. **Harness 预算：第一个 act 之前 ≤10 行**。超了就上带默认值的构造器（`seedRoute(spec)`、`seedEvent(overrides)`）；每包一个共享 PG 容器（TestMain + template-DB 克隆）；测试函数内**禁止** `docker run`/`go build`/`go run`；e2e 前导 mock 超 50 行直接打回。
4. **期望值超过 8 个字段必须用基线+覆写**——基线构造器逐用例 mutate 或 golden 文件，绝不再粘贴第二份 25 字段字面量。
5. **形状用 golden、行为用内联断言**：`loadRequestFixture` 与 `operation_translation_golden` 是本仓正统模式；规范化 `pg_dump` diff 替代逐列 DDL 断言；内联断言只写本测试真正关心的 ≤5 个字段、一行一断、禁止 8 路 `||` 墙。
6. **≥3 个用例共享 act+assert 形状即强制 `t.Run` 表**；每资源最多一个叙事型"故事测试"；所有错误探测进表。
7. **测产品，不测平台**：不测 Postgres 内部（TOAST/reloptions）、不 grep Go/TS 源码文本、不断言 recharts/xyflow 渲染输出、不手工复算聚合。一个测试若只可能因依赖升级而失败——删。
8. **proves-not 测试随删除 PR 过期**（不存在性用 PR 描述里的 grep 输出证明）。常设例外：路由契约 parity 守卫（它已含路由级 absence）。
9. **留下来的必须进 CI**；进不了 CI 的要么写明理由要么删。接线一律 glob、禁止手写清单（9 孤儿事件的教训）。补上：`go test ./internal/...` 进 CI（包内 Docker 测试清掉后它很快）、vitest 进 CI；Playwright 要么给一个冒烟 job 要么按规则 2 当本地工具箱。
10. **比率与命名防回弹**：功能 PR 的测试增行 ≤ 产品增行（现状 49%，目标 ≤35%）；禁止计划编号命名（`task9*`、`phase7*`、`s15*` → 领域名 `retention*`、`observability*`）；新共享 helper 必须在同 PR 删掉它替代的复制粘贴。

## 7. 对实施计划的修正（已同步回 IMPLEMENTATION_PLAN.md）

审计发现原计划 6 处测试相关的遗漏/错误，其中两处按原文执行会**弄断 CI**：

1. **R4（Task 9）范围错误，CI 级**：`profile_scope_test.go` 不能直接"3,571 → <200"——它里面住着整个 runtime 套件的 `TestMain`(:150)、~1,290 行共享 harness（:43-165,:2184-3428）和 ~1,860 行仍然有效的负载均衡/封禁/租约测试（:502-2183）。正确顺序：先抽 harness → `runtime_harness_test.go`（保住 TestMain + `startSharedPostgresHarness` + docker helper :3525-3542），LB 测试迁往 `proxy_selector_test.go`，然后才把 ~420 行真正的 scope 测试缩到 <200。另：`tests/contract/profile_scope_test.go`(481 行) 随 R4 死亡，原计划未列。
2. **R8（Task 13）漏网，CI 级**：`dashboard_routing_flow_layout_contract.test.mjs`（1,406 行）在 CI 的 `test:lib` 里，≥11/18 个测试加载 Task 13 要删的模块——R8 落地当天 CI 变红。需同 PR 删除（可抢救 graph/inspector/mobile 部分 ~80–400 行）。
3. **R3（Task 8）同类陷阱**：contract 套件的 `TestMain` 与共享 harness 住在 `auth_control_plane_test.go`（:45,:123,:1143）——裁剪该文件前先把 harness 抽到独立文件。
4. **R6（Task 11）漏一个元测试**：`s15:558-574 TestManagementMetricsEndpointRemovedAfterOTLP` 逐字读 `management_branch.go`/`db/pools.go`/`db/telemetry.go` 源码——R6 变体 1 落地即挂。
5. **R7（Task 12）已清**：websocket/realtime 前端 lib 契约测试已随退役任务删除，当前无 R7 lib 测试漏删项。
6. **R9a（Task 14）漏两个后端测试文件**：`tests/runtime/runtime_phase4_health_check_test.go`(329，直打 `/connections/{id}/health` :55,:94) 与 `tests/contract/connection_s10_contract_test.go` 的 health 部分（:51-120,:340，~160 行）——路由删掉后无法编译。

## 8. 需要拍板的决策

> 2026-07-08 已全部裁决，结论与理由见 [TEST_REDUCTION_PLAN.md](TEST_REDUCTION_PLAN.md) §1，执行细节见 [TEST_REDUCTION_HANDOFF.md](TEST_REDUCTION_HANDOFF.md)；下表保留原始选项供追溯。

| # | 决策 | 选项与建议 |
|---|---|---|
| TD1 | **包内测试进不进 CI？**（决定 22k 行包内测试的全部去留经济学） | 建议：清掉 6 个包内 Docker harness 后把 `go test ./internal/...` 加进 CI（很快，纯进程内）；否则"没进 tests/ 的一律视为可弃" |
| TD2 | **Playwright 的 5 个幸存者进 CI 还是当本地冒烟工具？** | 两报告分歧：进 CI（方向文档 §T4 原意）vs 本地工具箱 + 至多一个冒烟 job。两案封顶都是 ~5 个 spec。建议：先本地工具箱，等哪次真实回归漏网再升 CI |
| TD3 | **`shared-chart-statistics.spec.ts`(884) 去留** | 一方观点：它是统计核心唯一的真浏览器走查（recharts 无法在 jsdom 渲染）→ 压到 ~400 行保留；另一方：删。共识部分：先删 `.sisyphus/evidence` 写残留（:12-22,:642）与 recharts 内部断言（:702-812） |
| TD4 | **scheduler 归属/after-commit 两个源码 grep 的语义要不要保** | 保 → 花 ~100 行写行为等价测试；不保 → 随 §3-5 整删 |
| TD5 | **策略 CRUD 契约覆盖核实**（§3-3 前置） | 删 `loadbalance/routes_test.go` 前先确认 s15 或其他契约测试覆盖了 strategy CRUD；没有则移植 1 个用例再删 |

## 9. 数字汇总（验收参考）

| 类别 | 行数 |
|---|---|
| 现状测试总量 | ~78k（后端 tests/ 35.3k + 包内 22.1k + 前端 ~20.6k） |
| 已排期删减（IMPLEMENTATION_PLAN，§8 交叉引用） | ~11.4k |
| 本文新增删除（§3） | ~12.9k |
| 本文压缩净省（§4，去重后保守估） | ~6–8k |
| **目标态** | **~45–48k（约 -40%），且全部存活测试在 CI 中执行**；CI 容器数 44 → ~4，起应用次数 ~293 → 大幅下降（template-DB 克隆） |
