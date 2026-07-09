# Prism 精简与核心增强实施计划

> **状态：已全部执行并合并至 main（merge d6b6be1d，2026-07-09）。本文为历史执行记录。**
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 落地 [DEVELOPMENT_DIRECTION.md](DEVELOPMENT_DIRECTION.md) 的三类建议——移除/冻结外围功能（R1–R9）、增强核心链路（E1–E5）、清理技术债（T1–T8），把仓库收敛到"代理运行时 → 请求日志 → 统计与费用"这条主价值链上。

**架构方针：** 删除优先、冻结次之（多档案只冻结不挖列）、增强复用现有模式（outbox、快照聚合、既有图表组件）。每个任务独立成 PR、独立可验证、可被单独否决。

**技术栈：** Go 1.26（chi、pgx）、React 19 + TypeScript + Vite、TanStack Router、PostgreSQL 16、Playwright/vitest/node --test。

**本计划的证据来源：** 所有文件路径与行号锚点均经 2026-07-07 对 `main`（`c42b3984`）工作树逐一核实。行号会随先行任务漂移——执行时以符号名/内容锚点为准，行号仅作初始定位。

---

## 全局约束（每个任务隐含遵守）

- **G1 现网实例约束：** 生产实例运行在 `http://192.168.1.222:8088`（v0.4.7），数据库有真实数据。**迁移只进不退**：禁止 squash 或改写已应用的 `000001`–`000007`；新迁移从 `000008` 起按合入顺序取下一个空闲号（方向文档 §T1 中"合并 000002/000007"的想法**作废**）。
- **G2 启动配置 schema 只加不减：** 现网 `config.json` 包含 `mail`、`telemetry`、`stateTransfer.bundleEncryptionKey`、`database.pools.realtime` 等字段，且 bootstrap 解析**拒绝未知字段**。任何功能移除都**保留对应字段的解析**（parsed-but-unused，加 `// ponytail:` 注释标记），只删除消费行为。删字段 = 现网重启失败。
- **G3 i18n 机制：** `frontend/src/i18n/messages/en.ts` 同时承载 `Messages` 类型块（约 1300–1900 行）与英文值块（约 3200 行起）；`zh-CN.ts:1` 从它导入类型。**任何 key 增删都是 3 处编辑**：en.ts 类型 + en.ts 值 + zh-CN.ts 值。删 key 时 en.ts 先行，否则 `pnpm run build`（tsc）报错。
- **G4 路由契约：** 管理路由的增删必须与 `backend/internal/platform/http/management_route_contract.json` 同一提交修改；守卫测试为 `backend/internal/platform/http/server_test.go:460` 与 `backend/tests/contract/s11_management_contract_test.go:261`（两者运行时重读该 JSON）。
- **G5 文档同 PR：** 每个任务同 PR 更新 `docs/API_SPEC.md`（及涉及的 ARCHITECTURE/DATA_MODEL）与被改动包的 AGENTS.md。废弃的旧隐藏计划目录引用已清理，一切计划/文档写 `docs/`。
- **G6 标准验证（下文各任务以「标准验证」指代）：**

  ```bash
  cd backend && go test ./tests/contract ./tests/integration ./tests/runtime ./tests/priority/... && go build ./cmd/prism-backend
  cd frontend && pnpm run build && pnpm run lint && pnpm run test && pnpm run test:lib && pnpm run test:server
  ```

- **G7 易混淆的保留物（删除任务的红线）：**
  - `runtime_telemetry` 数据库连接池 lane（`config.go`、`production.go:140-158,409`）与 `backend/internal/httpapi/runtime/telemetry_outbox.go` 是**请求日志写入路径**，不是 OTel——R6 不得触碰。
  - `backend/internal/platform/email/outbox/`（邮件专用）≠ `backend/internal/platform/managementsideeffects/outbox.go` + `runtime_side_effects.go`（运行时侧效应）——R3 只删前者。
  - 代理 API 密钥全链路（`auth/routes.go:92` runtimeMiddleware、`store.go:1209` verifyProxyAPIKey、CRUD `routes.go:572-672`、`proxy_key_usage_writer.go`）与会话登录链（login/logout/refresh/session/throttle）在 R3 中**必须存活**。
  - `frontend/src/pages/settings/sections/authentication/OperatorEmailCard.tsx` 名字带 Email 但实为**用户名/密码编辑器**——保留。
  - `frontend/src/pages/request-logs/streamTelemetry.ts` 是 TTFT 展示，与 OTel 无关——保留。
  - `management_branch.go:34` 挂载的平台 `/health` 存活探针与 R9a 的连接健康探测无关——保留。

## 决策门（对应任务开工前需 owner 拍板）

| # | 决策 | 门控任务 | 默认建议 |
|---|------|---------|---------|
| D1 | 是否有 OTLP Collector 在收数据？ | R6 | 无 → 变体 1 全删；有 → 变体 2 只留 http/protobuf |
| D2 | WebSocket 退役还是留作告警通道？ | R7、E1 | 退役（E1 已设计为仅依赖 REST 轮询 + webhook，不受影响） |
| D3 | `/observe?tab=routing` 拓扑图是否在用？ | R8 | 保留标签页，仅删 xyflow 渲染（懒方案） |
| D4 | 登录是否彻底移除（step 2）？ | 本计划仅含 step 1 | step 1 保守裁剪；step 2 另行计划 |
| D5 | 图片路由确认无流量？ | R5 | 开工前跑 R5 的门控 SQL |
| D6 | 英文语言包是否为开源需求？ | T8 | 不需要 → 执行反转；需要 → 跳过 T8 |

## 阶段与顺序

| 阶段 | 任务 | 硬依赖 |
|------|------|--------|
| P0 半天快赢 | 1(T7) 2(E4a) 3(T1) 4(T3) 5(T4) | 无，可并行 |
| P1 大减法 | 6(R1) → 7(R2) → 8(R3) → 9(R4) 严格顺序（共享接线文件）；10(R5)/12(R7)/13(R8)/14–16(R9) 相互独立；11(R6) 在 R2 后 | R1→R2→R3→R4 |
| P2 核心增强 | 17(E4) → 18(E2) 顺序（共享解析函数）；19(E3)/20(E5) 独立；21(E1) 最后（含迁移+契约） | E1 在 R7 决策后 |
| P3 结构债 | 22(T5)；23(T2) 在 T3、R3 后；24(T6) 在 R1、R9a 后；25(T8) 决策门 D6 | 见各任务 |

**迁移号分配（按建议合入序）：** R3=`000008_drop_dead_auth_tables`；T5=`000009_stats_write_coherence` + `000010_drop_management_stat_rollups`；E1=`000011_alert_webhook_outbox`。实际以合入时下一个空闲号为准，名字不变。

---

# 阶段 P0：半天快赢

### Task 1: T7 — 文档修复（废弃目录引用 + ARCHITECTURE 图）

**Files:**
- Modify: `AGENTS.md`（根，:5,:16,:33,:52,:75,:90,:120 共 7 处废弃目录引用）
- Modify: `docs/AGENTS.md`（:4,:23,:24,:41,:58 共 5 处）
- Modify: `docs/ARCHITECTURE.md:5-22`（错乱的 ASCII 图）

- [x] **Step 1：** 删除/改写根 `AGENTS.md` 与 `docs/AGENTS.md` 的全部 12 处废弃目录引用。改写口径：活动计划与长期文档一律放 `docs/`；不要创建旧隐藏计划目录（owner 已确认该目录废弃）。
- [x] **Step 2：** 将 `docs/ARCHITECTURE.md:5-22` 整块 ASCII 图替换为一行文字（保留 :24 的脚注）：

  ```
  Client (5173) → Prism (Management APIs + Proxy Engine → PostgreSQL) → Providers (OpenAI / Anthropic / Gemini)
  ```

  同文件 :64-65 提到 `BrowserRouter` 与 legacy redirects 的两行归 T2/T3 的 PR 管，此处不动。
- [x] **Step 3：** 验证：`rg -n "\\.[o]mo" AGENTS.md docs/` → 0 命中。
- [x] **Step 4：** 提交：`git commit -m "docs: remove stale .o""mo references and fix architecture diagram"`

### Task 2: E4a — 接通死参数 `to_time`

**Files:**
- Modify: `backend/internal/httpapi/management/stats/service.go:536-566`（`parseRequestLogListParams`）
- Modify: `docs/API_SPEC.md`（请求日志列表端点参数表补 `to_time` 行；其他端点已文档化该参数，见 :1671,:1874 等）

**Interfaces:** WHERE 构建器已支持——`backend/internal/domain/stats/request_logs.go:365-368` 已有 `if params.ToTime != nil { ... created_at <= $n }`；结构体字段 `ToTime *time.Time` 已在 `domain/stats/types.go:15`。只缺解析。

- [x] **Step 1：** 先写失败测试。用 `rg -l "status_family" backend/tests backend/internal/domain/stats` 定位现有请求日志列表参数测试文件，按其表驱动模式加一个用例：造两条 `created_at` 相差 1 小时的日志，请求 `?from_time=<t0>&to_time=<t0+30m>`，断言只返回窗口内那条。运行该测试，预期 FAIL（当前 `to_time` 被忽略，两条都返回）。
- [x] **Step 2：** 在 `parseRequestLogListParams` 的 `from_time` 解析块（:537-540）之后，仿照同文件 `parseStatsSummaryParams` 中已有的写法补两行：

  ```go
  toTime, err := parseOptionalTime(r, "to_time")
  if err != nil { return statsdomain.RequestLogListParams{}, err }
  ```

  并在 :566 的返回字面量中加 `ToTime: toTime,`。
- [x] **Step 3：** 测试转绿；跑标准验证的 backend 部分。
- [x] **Step 4：** curl 冒烟：`curl "http://192.168.1.222:8088/api/stats/requests?from_time=...&to_time=..."` 确认窗口外行被排除（此前 `to_time` 静默无效）。
- [x] **Step 5：** 提交：`git commit -m "fix: honor to_time filter in request-log list params"`

### Task 3: T1 — context-routing 硬删除残留清理

**Files:**
- Delete: `backend/tests/contract/unified_removed_concepts_test.go`（383 行整文件；`rg -l removedConcept backend/` 确认只此一处）
- Modify: `backend/tests/runtime/proxy_selector_test.go`、`backend/tests/runtime/runtime_streaming_buffering_test.go`、`backend/tests/runtime/request_logs_contract_test.go`、`backend/tests/contract/model_contract_test.go`、`backend/internal/httpapi/management/models/store_test.go`、`backend/internal/httpapi/management/configbundle/*_test.go`（若 R1 未先行）、`frontend/package.json:19`

- [x] **Step 1：** 删除 28 处空壳 stub 调用（stub 是 no-op，删调用行为零行为变化）：
  - `setRuntimeHarnessConnectionContextCapabilities` 26 处：`proxy_selector_test.go` :127,:128,:183,:184,:501,:850,:851,:986,:1068,:1069,:1070,:1071,:1276,:1277,:1313,:1314,:1337,:1338,:1361,:1362,:1385,:1386；`runtime_streaming_buffering_test.go` :606,:607,:622,:623。
  - `enableRuntimeHarnessFacadeModel` 2 处：`proxy_selector_test.go` :978,:1000。
  - 然后删两个 stub 定义：`proxy_selector_test.go:1091`（enable…）与 `:1097`（set…）。
- [x] **Step 2：** 重命名 5 个 `*AfterContextRoutingRemoval` 测试（去后缀即可）：`proxy_selector_test.go` :18,:106,:167,:309,:325。
- [x] **Step 3：** 清散落的 absence 断言：
  - `request_logs_contract_test.go:247-251` 的 absent 列表中**只删 `"context_routing"`**（其余三项是字段位置断言，保留）。
  - `model_contract_test.go:338-360` `TestModelContextCapabilitiesRejectModelOwnedFields` 整测删除。
  - `models/store_test.go:34-48` 两个 `RejectsPromotionTarget*` 测试整删。
  - `configbundle_contract_test.go:656-666,:689-693` 与 `configbundle/import_test.go:174-186` 的 absence 断言：若 R1 已合入则已消失；否则此处删除。
  - **保留** `migrations_test.go` :61,:86,:89,:668 的 `assertContextCapabilityAndFacadeColumnsAbsent`——它验证迁移 000007 对预置 schema 的效果，是正当的迁移行为测试，不是 proves-not。
- [x] **Step 4：** `frontend/package.json:19` 的 `test:e2e` 整段 node 包装脚本（为已删除的 `context-capability-authoring` 测试族改写 `--grep` 参数）替换为一行：

  ```json
  "test:e2e": "playwright test"
  ```

- [x] **Step 5：** 验证：`rg -n "setRuntimeHarnessConnectionContextCapabilities|enableRuntimeHarnessFacadeModel|AfterContextRoutingRemoval|removedConcept" backend/` → 0；`rg -n "context-capability-authoring" frontend/` → 0；标准验证 backend 部分；`cd frontend && pnpm run test:e2e -- --list` 能列出 specs。
- [x] **Step 6：** 提交：`git commit -m "test: remove context-routing hard-delete residue"`

### Task 4: T3 — 删除 11 条 legacy 重定向路由

**Files:**
- Modify: `frontend/src/app/router/appRouter.tsx`（:24 import、:48、:136-143、:237-241、:320-330、:347-357）
- Modify: `frontend/src/app/router/rewriteRoutes.ts`（:94-106、:108-112、:132-144、:146、:156-158、:160-167）
- Modify: `frontend/src/test/route-helpers.test.ts`（:5-6 import、:31-37 测试）、`frontend/src/app/index`（barrel 再导出）
- Modify: `docs/ARCHITECTURE.md:65`（去掉 "and legacy redirects" 短语）

- [x] **Step 1（防断根路由的陷阱，先做）：** `indexRoute`（`appRouter.tsx:237-241`）当前经 `LegacyRedirectRoute` 借 `legacyRouteRedirects["/"]` 落到 `/observe`。先把 `indexRoute` 的 component 改为直接 `() => <Navigate to="/observe" replace />`，再动其他。
- [x] **Step 2：** 删除 `appRouter.tsx:320-330` 的 11 个 `legacy*Route` 常量、`:347-357` 路由树里对应 11 条、`:136-143` 的 `LegacyRedirectRoute`、`:24` import 中的 `getLegacyRedirectPath`；`:48` `PUBLIC_AUTH_PATHS` 收窄为仅 3 条 `/auth/*`。
- [x] **Step 3：** 删除 `rewriteRoutes.ts` 中 `rewriteCompatibilityRoutePaths`（:94-106，含 :108-112 的展开）、`legacyRouteRedirects`（:132-144）、`LegacyRoutePath` 类型（:146）、`buildLegacyRequestAuditRedirect`（:156-158）、`getLegacyRedirectPath`（:160-167）；同步清 `src/app/index` barrel 再导出与 `route-helpers.test.ts:31-37` 的 legacy 测试及 :5-6 的 import。
- [x] **Step 4：** 验证：`rg -n "legacyRouteRedirects|getLegacyRedirectPath|buildLegacyRequestAuditRedirect|rewriteCompatibilityRoutePaths|LegacyRedirectRoute" frontend/` → 0；标准验证 frontend 部分；手测 `/` 落 `/observe`、`/dashboard` 404（预期行为——无用户无兼容层）。
- [x] **Step 5：** 提交：`git commit -m "refactor: drop legacy route redirects"`

### Task 5: T4 — test:lib glob 化 + 测试地产快赢

**Files:**
- Modify: `frontend/package.json:16`
- Delete: `frontend/tests/e2e/task-6-auth-profile-smoke.spec.ts`、`task-8-models-feature.spec.ts`、`task-9-model-detail-reorder.spec.ts`、`task-10-endpoints-pricing.spec.ts`、`task-11-ban-policies.spec.ts`、`task-17-ui-semantics.spec.ts`
- Modify: `frontend/tests/lib/AGENTS.md`（"Scripted test:lib allowlist" 段落改为 glob 说明）

- [x] **Step 1：** `package.json:16` 的 15 文件手工清单替换为 glob（Node ≥24，`node --test` 自 v21 支持 glob）：

  ```json
  "test:lib": "node --test \"tests/lib/*.test.mjs\" \"tests/model-detail/*.test.mjs\""
  ```

  glob 自动接入此前 9 个孤儿测试；其中 `profile_selection_contract.test.mjs` 在 R4 删、`analytics_websocket_contract.test.mjs` 在 R7 删，其余 7 个（costing/reporting-currency、log-retention、management、pricing 规范化、request-log audit/filter state）都覆盖核心面，保留。
- [x] **Step 2：** 删除 6 个计划编号 e2e spec（`task-*.spec.ts`，为已完结计划服务）。各 R 包配套的 e2e spec 在对应任务中删，不在此处。
- [x] **Step 3：** `pnpm install && pnpm run test:lib` ——这是 9 个孤儿的**首次真实运行**（此前 checkout 缺 `node_modules/typescript` 无法验证）；有真失败的当场修或删，同 PR 处理。
- [x] **Step 4：** 标准验证 frontend 部分；提交：`git commit -m "test: glob test:lib and prune plan-numbered e2e specs"`

---

# 阶段 P1：大减法（R1→R2→R3→R4 严格顺序）

### Task 6: R1 — 配置包导出/导入整体移除（≈5k LOC，最低风险的大删除）

**Files:**
- Delete（后端整目录 13 文件）: `backend/internal/httpapi/management/configbundle/`
- Delete（后端测试）: `backend/tests/contract/configbundle_contract_test.go`、`configbundle_s13_contract_test.go`
- Delete（前端）: `frontend/src/pages/settings/useConfigBackupData.ts`、`frontend/src/pages/settings/sections/BackupSection.tsx`、`frontend/src/lib/configImportValidation.ts`、`frontend/src/lib/configImportValidationReferences.ts`（删前 `rg -l configImportValidationReferences frontend/src` 确认唯一消费者是前者）、`frontend/tests/e2e/settings-config-export.spec.ts`、`settings-config-import.spec.ts`、`frontend/tests/lib/config_import_validation_contract.test.mjs`、`config-import-pricing-template-normalization.test.mjs`
- Modify: 接线与契约见下

**Interfaces（必须存活）：** `platform/config` 的 `ConfigBundleEncryptionKey` 字段解析（G2：现网 `config.json` 可能含 `stateTransfer.bundleEncryptionKey`）——变为 parsed-but-unused，加 `// ponytail: parsed for live config.json compat; feature removed`。

- [x] **Step 1：** 删除上列文件；`git rm -r backend/internal/httpapi/management/configbundle`。
- [x] **Step 2：** 后端接线（以 `go build ./...` 编译器为最终权威）：
  - `management_branch.go`：:12 import、:36 `deps.ConfigBundleService` 实参、:47 形参、:58-60 mount 块。
  - `dependencies.go`：:11 import、:36 `ConfigBundleService` 字段。
  - `production.go`：:18 import、:196 结构体字段、:367-372 构造/注册块，及 `services.configBundle` 的所有拷贝点（文件内搜 `configBundle`）。
  - `server_test.go`：:21 import、:403 构造实参；`management_body_limits_test.go`：:15、:96 及 bundle 路由用例。
  - `management_body_limits.go:50-51` 的 `ConfigBundleRequestBodyLimitBytes` 分支；`bodylimits/body_limits.go:15` 常量。
  - 措辞修订（代码保留）：`providerauth/doc.go:5,9`、`domain/modelrouting/doc.go:5` 中提及 configbundle 的注释。
- [x] **Step 3：** 前端接线：`useSettingsPageData.ts` :12/:28/:80 三处、`SettingsProfileTab.tsx` :5/:35-53、`settingsPageHelpers.ts:21` 的 `{ id: "backup" }`、`lib/api/observability.ts:163-179` 的四个 bundle 客户端方法、`lib/types/config-audit-settings.ts` 只删 bundle 四类型（audit-settings 类型保留）、`package.json` 删 `test:config` 脚本（:18）——若 Task 5 已 glob 化 test:lib 则无清单可改，直接 `git rm` 两个 lib 测试文件即可。
- [x] **Step 4：** 路由契约：`management_route_contract.json:54-57` 删 4 条配置包导出/导入路由。同提交，G4 的守卫测试自愈。
- [x] **Step 5：** i18n（G3，en.ts 类型+值、zh-CN 值三处齐删）：`backup`（en :362/:2290，zh :366）、`settingsBackup`（en :881/:2815，zh :885）、`settingsBackupData` + `settingsBackupValidation`（en :930/:941 + 值块，zh :934/:945）。**注意** `backupCapable`/`backupReady`（en :757-758/:2688-2689）属另一命名空间，先 `rg -l 'backupCapable|backupReady' frontend/src` 确认消费者再决定。
- [x] **Step 6：** 文档同 PR：`docs/API_SPEC.md`（:778-960 四个端点段 + :15-16 scoping 列表）、`docs/WORKFLOWS.md:230-249,273`、`docs/ARCHITECTURE.md:223,235`、`docs/DATA_MODEL.md`（:3,:326,:328,:381-382,:449,:709,:1122-1124 的 "version: 3 bundle" 表述）、`docs/PRD.md:226`、根 `README.md:26,270`、`frontend/README.md:16`，以及涉及的各 AGENTS.md（management、httpapi、backend、根、frontend lib/api/settings/sections、tests、docs）。README 处补一句替代方案：灾备用 `pg_dump`。
- [x] **Step 7：** 验证：

  ```bash
  rg -il configbundle backend/ frontend/src frontend/tests docs/API_SPEC.md docs/DATA_MODEL.md docs/ARCHITECTURE.md README.md   # 0 命中（DEVELOPMENT_DIRECTION.md 历史除外）
  rg -l "config/profile/(export|import)" backend frontend docs README.md                                                        # 0 命中
  ```

  标准验证全绿；契约 JSON 从 62 行降到 58。
- [x] **Step 8：** 提交：`git commit -m "feat!: remove config bundle export/import"`

### Task 7: R2 — 设置页启动标签 + bootstrapconfig API 移除

**前置：R1 已合入**（共享 `management_branch.go`/`dependencies.go`/`production.go`/`server_test.go`/`management_body_limits*`）。

**Files:**
- Delete: `backend/internal/httpapi/management/bootstrapconfig/`（3 文件整目录）；`backend/tests/contract/bootstrap_config_contract_test.go`；`backend/tests/integration/bootstrap_config_test.go`；`backend/internal/platform/config/bootstrap_management_test.go`；`frontend/src/features/settings/startup/`（8 文件整目录）；`frontend/src/lib/types/bootstrap-config.ts`；`frontend/tests/e2e/settings-startup-tab.spec.ts`（已于测试精简批次 1 提前完成）；`frontend/tests/lib/bootstrap_config_contract.test.mjs`
- Modify: 见下（核心是 `platform/config/` 的 KEEP/DELETE 切分）

**Interfaces（KEEP，运行时内核）：** `config.go` 全部 Settings 解析；`bootstrap.go` 的加载路径——`NewBootstrapConfigManager`(:443)、`Load`(:447)、`LoadFromEnv`(:459)、`LoadOrSeed`(:467)、`LoadOrSeedFromEnv`(:492)、`LoadBootstrapConfigDocument`(:500)、`Parse`(:611)、`WriteAtomically`(:619)、`WriteAtomicallyIfAbsent`(:641)、`seedPayloadFromDefaults`(:1722)、`BootstrapConfigSnapshot`；`bootstrap_apply.go` 的分类核心——`BootstrapConfigHotApplyRuntime` 接口(:39)、字段注册表(:140-293)、`BootstrapConfigFieldDiff`；整个 `platform/http/hot_bootstrap_runtime.go`（它是全部管理服务的 CORS/admission provider；其 `Publish()`(:64) 失去唯一调用者——留 `// ponytail:` 注释，后续再修剪）。

- [x] **Step 1：** 删除上列文件。
- [x] **Step 2：** 从 `bootstrap.go` 删除仅服务已删 API 的符号（**以 `go build` 编译器裁决**）：类型 `BootstrapConfigResponse`(:72)、`PlannedChanges`(:90)、`ApplyResult`(:95)、`ResponseOptions`(:103)、`UpdateRequest`(:249)、`PreparedUpdate`(:257)、`MissingConfirmationsError`(:287-291)；函数 `BuildBootstrapConfigResponse`(:505)、`PrepareBootstrapConfigUpdate`(:534)、`ValidateBootstrapConfigUpdate`(:583)、`WriteBootstrapConfigUpdate`(:587)、`SaveBootstrapConfigUpdate`(:600)、`validateBootstrapConfigExpectations`(:739)、`missingBootstrapConfigConfirmations`(:941)；`ConflictError`/`SecretOperationError`（先确认无其他使用者）；`bootstrap_apply.go` 的 `PlannedChangesFromDiff`(:301)、`ApplyResultFromDiff`(:305)；`bootstrap_apply_test.go` 删这两个 helper 的用例、保留分类/diff 测试。
- [x] **Step 3：** 后端接线：`management_branch.go` :11/:36/:47/:55-57；`dependencies.go` :10/:35/:82 起构造块；`production.go` :17/:122-140 及 :124-125 的 `LoadedRevision/LoadedDocumentETag` 管道；`cmd/prism-backend/main.go` :40-41/:126-127/:179-180 的 revision/etag 交接（`loadBootstrapConfigDocumentWithRepair`(:184)、`reseedBootstrapConfig`(:209) **保留**）；`server_test.go` :20/:349/:369/:402；`management_body_limits.go:47-48` bootstrap 分支 + `bodylimits/body_limits.go:14` 常量 + 对应测试用例。
- [x] **Step 4：** 前端接线：`SettingsPage.tsx` :12 import、约 :58 的 TabsTrigger、:73-75 TabsContent；`settingsPageHelpers.ts:10-14` 的 `SETTINGS_TABS` 删 `startup`；`useSettingsPageSectionState.ts` :30,:35,:39,:44,:55,:102-105 全部 startup 分支；`rewriteRoutes.ts:57` 的 `z.enum(["profile","global","startup"])` 删 `"startup"`；`lib/api/observability.ts:147-160` 客户端组；`lib/types.ts:10` 的 `export * from "./types/bootstrap-config"`。
- [x] **Step 5：** 路由契约：**无需改 JSON**（`/api/config/bootstrap*` 是全局路由，契约文件中 0 条目；唯一路由清单触点是 `server_test.go:369`，已在 Step 3 删）。
- [x] **Step 6：** i18n：`startupTab`（en :368/:2296，zh :372）、`settingsStartup` 整命名空间（en :375/:2303，zh :379）。
- [x] **Step 7：** 文档：`docs/API_SPEC.md`（:29 GET、:206 validate、:213 PUT 三段 + :15 全局路由列表）、`docs/PRD.md:226`、`docs/WORKFLOWS.md:30,217,220,225`、`docs/ARCHITECTURE.md:114,224`、根 `README.md:217,219,223,258,270` 改写为「编辑 `config.json` + 重启」——**明确写出：R2 之后外部编辑配置一律需重启，不再有热应用路径**（README:270 现在承诺的相反语义必须撤掉）；各 AGENTS.md（platform/config、platform/http、management、frontend settings/lib/api/lib/types、backend、根、docs、frontend/tests）。
- [x] **Step 8：** 验证：

  ```bash
  rg -l "managementbootstrapconfig|features/settings/startup|config/bootstrap" backend frontend/src frontend/tests docs README.md   # 0（DEVELOPMENT_DIRECTION.md 除外）
  rg -n "startup" frontend/src/pages/settings/settingsPageHelpers.ts frontend/src/app/router/rewriteRoutes.ts                        # 0
  ```

  标准验证全绿；`startup_test.go`/`launcher_startup_contract_test.go` 仍绿证明文件加载/播种/修复内核无恙。
- [x] **Step 9：** 提交：`git commit -m "feat!: remove startup settings tab and bootstrap config API"`

### Task 8: R3 — 认证链瘦身 step 1（删 WebAuthn 表 + 密码重置/恢复邮箱 + 邮件栈；保登录与代理密钥）

**前置：R2 已合入。关键事实：WebAuthn 零 Go 代码零组件**——只有 2 张表、死 i18n key、DATA_MODEL 章节。

**Files:**
- Delete: `backend/internal/platform/email/`（整树 4 文件）；`backend/tests/priority/outbox/email_outbox_priority_test.go`；`backend/internal/httpapi/management/auth/email_outbox_phase6_test.go`；`frontend/src/pages/ForgotPasswordPage.tsx`、`ResetPasswordPage.tsx`；`frontend/src/pages/settings/sections/authentication/RecoveryEmailCard.tsx`
- Create: `backend/migrations/000008_drop_dead_auth_tables.sql`
- Modify: `auth/` 包 6 个部分编辑文件 + 接线，见下

**Interfaces（MUST-SURVIVE，G7）：** 代理密钥全链路、会话登录链（`handleLogin`:250/`handleLogout`:287/`handleRefresh`:322/`handleGetSession`:349/`handleGetAuthStatus`:210/`handleGetPublicBootstrap`:219、throttle `store.go:234-385`、refresh 轮换 :461-676、`managementMiddleware`:56）、`tokens.go`/`cookies.go`/`runtime_cache.go`/`telemetry.go`/`realtime.go`（realtime 归 R7 裁决）/`request_tokens.go` 整文件、存活表 `app_auth_settings`（email 列**保留不删**，只停止读取）/`login_throttle_ledger`/`refresh_tokens`/`proxy_api_keys`、`OperatorEmailCard.tsx`（实为用户名/密码编辑器）。

- [x] **Step 0（预检）：** `rg -n 'signInWithPasskey|browserNoPasskeys' frontend/src --glob '!**/i18n/**'` → 预期 0（确认 passkey key 无组件消费，纯死 key）。非 0 则先处理消费点。
- [x] **Step 1（后端行为裁剪）：**
  - `auth/routes.go`：删 `publicManagementPaths` 中 :30-31 两条 password-reset 路径；删 :20 outbox import；整删 `handlePasswordResetRequest`:367、`handlePasswordResetConfirm`:440、`handleEmailVerificationRequest`:497、`enqueueAuthEmail`:542、`handleEmailVerificationConfirm`:552。
  - `auth/service.go`：删 `Mailer` 接口(:20-23)、`Options.Mailer/EmailOutbox` 字段、`Service.emailOutbox` 字段与赋值；`MountManagementRoutes` 删 4 条挂载（password-reset request/confirm、email-verification request/confirm :257-258）。
  - `auth/store.go`：整删 `beginEmailVerification`:757、`confirmEmailVerification`:789、`createPasswordResetChallenge`:835、`loadLatestPasswordResetChallenge`:876、`consumePasswordResetChallenge`:903、`buildEmailVerificationResponse`:1251、`validateEmail`:1330、`passwordResetChallengeRow` 类型；`scanAppAuthSettings`:209 与 `buildAuthSettingsResponse`:1238 停止读/回 email 四字段。**先跟踪** `handlePutAuthSettings`:471/`updateAuthSettings`:677 是否分支于 email-verification 状态再裁响应字段。
  - `auth/types.go:38-66`：删 5 个 email/reset 类型；`authSettingsResponse`(:22-31) 裁 4 个 email 字段。
  - `auth/runtime_config.go`：裁 reset-code TTL 字段及其快照接线。
- [x] **Step 2（平台接线）：** `production.go`：删 :279-281 `outbox.NewStore` 构造、`authServices.emailOutbox` 字段(:180)、`NewService` 调用里的 `EmailOutbox:`、:422-431 worker 注册、:32 import。`hot_bootstrap_runtime.go`：删 `Mailer()`(:92-93) 与 `HotMailSnapshot` 机制(:193-211)。
- [x] **Step 3（G2 裁决——mail 配置保留解析、删除行为）：** **不删** `config.go` 的 `MailConfig`/`MailSMTPConfig` 结构与 `bootstrap.go` 的 mail 字段解析（现网 `config.json` 含 `"mail":{"enabled":false}`，删 schema 即 brick 启动）。只删：SMTP 启动校验 fail-fast 逻辑、`Mailer` 构造消费点；结构体处加 `// ponytail: mail config parsed for live config.json compat; delivery removed`。`config_test.go:58-59` 的断言按保留后的实际行为调整。
- [x] **Step 4（前端）：** `LoginPage.tsx:116` 删 forgot-password 链接块；`appRouter.tsx` 删 :43-44 懒加载、`PUBLIC_AUTH_PATHS` 收为 `["/auth/login"]`（T3 已删 legacy 项）、`PublicForgotPasswordRoute`/`PublicResetPasswordRoute` 组件与路由定义及路由树条目；`lib/api/authSettings.ts` 删 `requestPasswordReset`/`confirmPasswordReset`/email-verification 调用（保 :21-30 与 :62-79）；`lib/types/auth.ts` 裁类型；`AuthenticationSetupGrid.tsx` 去 `RecoveryEmailCard`。
- [x] **Step 5（迁移，同 PR、代码裁剪之后）：** 新建 `backend/migrations/000008_drop_dead_auth_tables.sql`：

  ```sql
  DROP TABLE IF EXISTS webauthn_challenges;
  DROP TABLE IF EXISTS webauthn_credentials;
  DROP TABLE IF EXISTS password_reset_challenges;
  DROP TABLE IF EXISTS email_outbox;
  ```

  FK 安全已核实：四表的 FK 全部**向外**指向存活的 `app_auth_settings`，无表引用它们，无需 CASCADE。`migrations_test.go` 的已应用清单加 000008。**顺序红线：迁移不得先于停止写入 `email_outbox` 的代码合入。**
- [x] **Step 6（测试裁剪）：** ⚠️ contract 套件的 `TestMain` 与共享 harness 住在 `auth_control_plane_test.go`（:45,:123,:1143）——**先把 harness 抽到 `tests/contract/harness_test.go`**，再对该文件（2,000 行）删 password-reset/email-verification/mail 用例，保留 login/logout/refresh/throttle/proxy-key；`scheduler_ownership_test.go` 删 email-outbox worker 归属断言。
- [x] **Step 7（i18n + 文档）：** 按 G3 删 key 组：forgot/reset（en 类型 :8-11,:20-23、值 :1916-1929）、passkey/WebAuthn 全部（含 `settingsPasskeysData` 整块 :820）、email-verification（:766-767）、recoveryEmail（:784-787），zh-CN 镜像。文档：`API_SPEC.md` 删 mail 段 :80-101,:171-198,:227 与热应用注册表中 `auth.reset_code_ttl_seconds`/`mail.*` 行、删 password-reset/email-verification 端点段；`DATA_MODEL.md` 删四表章节；README mail 段；相关 AGENTS.md。**路由契约无需改**（`/api/auth/*` 不在契约 JSON；现存的 auth PUT 与 3 条 proxy-key 行全部存活）。
- [x] **Step 8：** 验证：

  ```bash
  rg -i 'webauthn|passkey' backend frontend --glob '!**/migrations/**' --glob '!docs/DEVELOPMENT_DIRECTION.md'   # 0
  rg -i 'password.?reset|email.?verification|recovery.?email' backend/internal frontend/src                      # 0
  rg 'platform/email' backend                                                                                     # 0
  ```

  标准验证全绿；现网冒烟：登录（若启用）与代理密钥鉴权照常。
- [x] **Step 9：** 提交：`git commit -m "feat!: slim auth chain — drop mail stack, password reset, webauthn tables"`

### Task 9: R4 — 多档案冻结（钉死 Default id=1，保留全部 profile_id 列）

**前置：R3 已合入。策略：冻结不挖列**——所有 `profile_id` 列/FK/索引原样保留，解冻路径就是两处 `ponytail:` 注释。

**Files:**
- Delete: `backend/internal/httpapi/management/profiles/`（整包）；前端 `ProfileSwitcher.tsx`、`ProfileDialogs.tsx`、`useProfileSwitcherState.ts`、`useProfileDialogState.ts`、`navigationProfileConfig.ts`、`context/ProfileContext.tsx`、`context/profile/` 整目录、`frontend/tests/lib/profile_selection_contract.test.mjs`、`frontend/tests/e2e/profile-scope-bootstrap.spec.ts`（已于测试精简批次 1 提前完成）、`profile-scope-route-headers.spec.ts`（已于测试精简批次 1 提前完成）
- Modify: 钉点 + 接线见下；`backend/tests/runtime/profile_scope_test.go`（**先抽 harness 再裁剪**，见 Step 4——不可直接替换全文）
- Delete: `backend/tests/contract/profile_scope_test.go`（481 行，`/api/profiles` CRUD/activate 契约 :29-172——路由随本任务消失；其"scope header 接受但钉死"的价值并入 Step 4 的新回归）
- **KEEP：** `backend/internal/profiledomain/` 整包（钉点所在 + `startup/profiles.go` 靠它保证 Default 存在）；`frontend/tests/lib/profile_scope_header_contract.test.mjs`（配合前端"继续发 header"的懒方案，原样存活）

- [x] **Step 0（预检，风险 #4）：** `rg -n 'bootstrap' frontend/src/context/profile/bootstrap.ts` 查看 `api.profiles.bootstrap()` 返回物——若只有档案列表+active id，前端替换即硬编码 `{id: 1}`；若喂了别的壳层启动数据，先安排替代来源再动手。
- [x] **Step 1（THE 钉点）：** `backend/internal/profiledomain/scope.go:11` 的 `ResolveEffectiveProfile` **签名不变**，函数体替换为忽略 `rawHeader`、`return LoadNonDeletedProfile(ctx, exec, 1)`（错误映射沿用现有 not-found）。签名不变 ⇒ 全部 12 个调用点零改动。注释：`// ponytail: pinned to Default profile id=1; unfreeze by restoring header parsing`。
- [x] **Step 2：** 后端接线：`management_branch.go:47` 去 `profilesService` 形参、删 :76-78 mount 与 import；`production.go` 删构造与实参；`runtime_cache_invalidation.go` 删 `invalidates_active_profile` 死分支。
- [x] **Step 3：** 前端懒方案：`lib/api/core.ts:13-14` 删 `setApiProfileId`，`currentProfileId` 改 const `= 1`（:77 的 `headers["X-Profile-Id"]` 继续无条件发 `1`，后端反正忽略），注释 `// ponytail: profile pinned to Default(1)`；`lib/api/management.ts` 删 `api.profiles` 客户端段；`appRouter.tsx:18` 去 `ProfileProvider` 包装；`AppSidebar.tsx`/`useAppLayoutState.ts`/`useShellNavigation.ts` 去 ProfileSwitcher 渲染与状态钩子（锚点用 `rg -n 'ProfileSwitcher|useProfileSwitcherState|ProfileDialogs' frontend/src/components/layout`）。收尾横扫：`rg -l 'ProfileContext|useProfile|setApiProfileId|activateProfile' frontend/src` 每一处都必须被编辑或删除。
- [x] **Step 4（profile_scope_test.go：先抽 harness、再迁移、最后裁剪——⚠️ 直接替换全文会弄断整个 runtime 套件）：** 该文件里住着 runtime 套件的 `TestMain`(:150)、~1,290 行共享 harness（:43-165,:2184-3428）与 ~1,860 行仍有效的负载均衡/封禁/租约测试（:502-2183,:3172-3253）。顺序：(a) harness 连同 `TestMain`、`startSharedPostgresHarness`、docker helper(:3525-3542) 抽到新文件 `runtime_harness_test.go`；(b) LB/封禁/租约测试迁往 `proxy_selector_test.go` 或新领域文件；(c) 把 ~420 行真正的 scope 测试（:166-501）缩成一个钉死回归：对任一 profile-scoped 管理路由分别带 `X-Profile-Id: 999` 与不带 header 请求，断言都成功且读写落在 `profile_id = 1`。**这个测试的存在就是防止有人把冻结"修"回去。**（Step 7 的 `wc -l` 验证目标相应指新的 scope 测试文件，非套件 harness 文件。）
- [x] **Step 5：** 路由契约：删第 6 行 `/api/profiles/{profile_id}/activate`（唯一 `invalidates_active_profile` 条目）；所有 `"profile_scoped": true` 标志**原样保留**（查询仍按 profile 1 作用域）。检查两个守卫测试有无显式 `/api/profiles` 期望。
- [x] **Step 6：** i18n：删 `profiles:` 块（en 类型 :1020、值 :2961，zh 镜像）。文档：`API_SPEC.md` 删 `/api/profiles*` 端点段、`X-Profile-Id` 契约改写为「接受但忽略；恒为 Default(1)」；`DATA_MODEL.md` §2.1 保留但注明冻结（删 :277 "最多 10 个"）；`ARCHITECTURE.md` 作用域段；`frontend/src/lib/api/AGENTS.md` :9,:18-19,:30-31,:47 改写。
- [x] **Step 7：** 验证：

  ```bash
  rg 'management/profiles' backend                                   # 0
  rg 'ProfileSwitcher|ProfileContext|setApiProfileId' frontend/src   # 0
  rg -c 'X-Profile-Id' frontend/src/lib/api/core.ts                  # 恰好 1
  wc -l backend/tests/runtime/profile_scope_test.go                  # < 200
  curl -s -H 'X-Profile-Id: 999' http://192.168.1.222:8088/api/models | head   # 200 + Default 数据（冻结前是 404）
  ```

  标准验证全绿。
- [x] **Step 8：** 提交：`git commit -m "feat!: freeze multi-profile to pinned Default (id=1)"`

### Task 10: R5 — 图片 generations/edits 路由移除（决策门 D5）

- [x] **Step 0（门控 SQL，现网执行）：** `SELECT count(*) FROM request_logs WHERE operation_name LIKE 'openai.images%';` → 仅当 0 才继续。
- [x] **Step 1：** 先做唯一非机械步——`operation_media_hooks.go` 的包装器塌缩：删 :11-12 hook 集合常量、`operationMediaRequestKind`/`operationMediaHooks` 类型、`operationMediaHooksByCollectionID`(:33-47)、`mediaHooksForOperation`、三个 `...ViaAdapter` 函数；把通用包装器（`extractModelFromBodyForOperation`、`rewriteModelInBodyForOperation`、`auditRequestBodyForOperation`、`multipartBoundary`）塌缩为各自 fallback（`extractModelFromBody(rawBody)` 等）或内联到调用点（调用点：`rg -n "ForOperation\(|multipartBoundary\(" backend/internal/httpapi/runtime`）。**此步必须先于删除 `provider/openai/images.go`**（其 `RedactImageRequestAuditBody`/`MultipartBoundaryForRuntime` 正被这些包装器调用）。
- [x] **Step 2：** 整删：`openai_image_adapter_bridge.go`、`operation_media_hooks_test.go`、`gateway/provider/openai/images.go`、`tests/runtime/testdata/route_matrix_upstream/openai-images-{generations,edits}.json`。
- [x] **Step 3：** 目录条目与 hook 表：`operations.go:52-53` 两条、`operation_request_hooks.go:32-38` 两条、`operation_response_hooks.go:59-68` 两条（`operationResponseKindMedia`/`proxyNonEventResponseAndCaptureWithoutUsage` 若变死码一并删）、`gateway_core_bridge.go:42-45` 两个 case。
- [x] **Step 4（接口收缩，单独 commit、放最后）：** `gateway/provider/adapter.go` 删 `MediaRequest`(:123) 与接口方法 `HandleMedia`(:223)；`default_adapter.go:52` 与 `openai/adapter.go:191-192` 的实现/分支——接口改动波及全部 adapter 编译，与实现删除同 commit。
- [x] **Step 5：** 测试部分编辑：`operations_test.go:228-243`、`operation_hook_residency_test.go:75-87`、`gateway_typed_hooks_bridge_test.go:23-56`（围绕 image 形状，整删或改指文本操作）、`operation_response_hooks_test.go:55-57,110-117`、`tests/runtime/operation_route_matrix_test.go:157-185` + `:377+` 整个 `TestOpenAIImageEditsMultipartForwarding`、`request_generation_params_contract_test.go:118-133` + `:340-358` helper、`body_limits_test.go`、`adapter_conformance_test.go:51-53`、`service_ingress_test.go`/`runtime_test.go`（grep `images`）。
- [x] **Step 6：** 文档：`API_SPEC.md:1249-1250,:1367-1377,:1456`；`gateway/CONTRACTS.md`、`gateway/provider/AGENTS.md`、`httpapi/runtime/AGENTS.md` grep `images` 清理。无迁移、无 i18n、无契约 JSON（`/v1/*` 不在管理契约内）。README:13 特性列表删两条 images 路由。
- [x] **Step 7：** 验证：`rg -in "images\.generations|images\.edits|IsImageOperation|MediaRequest|operationMediaHooks" backend --glob '!docs/**'` → 0；标准验证；`curl -X POST http://192.168.1.222:8088/v1/images/generations` → 404/operation-not-found。
- [x] **Step 8（可选深挖，独立 commit）：** `gateway/routing/` 的图片预约管道（`planner.go:24,:33,:36,:40,:204-217`、`reservation_manager.go:113,:128,:169`、`planner_test.go:220-277`）与 `gateway/core/envelope.go:23-24` + `classification.go:54` 形状常量。
- [x] 提交：`git commit -m "feat!: remove image generations/edits operations"`

### Task 11: R6 — OTel 遥测（决策门 D1；前置 R2）

**G7 红线重申：`runtime_telemetry` 池 lane、`telemetry_outbox.go`、`streamTelemetry.ts` 一律不动。**

- [x] **Step 0：** 按 D1 选变体。变体 2（保 http/protobuf 删 gRPC）：只改 `providers.go`（:18-21 imports、:30 grpc credentials、:171-192 GRPC case 改报错、删 `newGRPCTraceExporter`/`newGRPCMetricExporter`）+ `config.go:36` 删 GRPC 常量 + `go.mod` 删 2 行 + `go mod tidy` + `API_SPEC.md:202` 注明仅 `http/protobuf`，验证 `rg "otlpmetricgrpc|otlptracegrpc" backend` → 0，完事。以下步骤为变体 1（全删）。
- [x] **Step 1：** 整删：`platform/telemetry/`（5 文件）、`platform/asyncmetrics/`（2 文件）、`platform/db/telemetry.go`、`pgxutil/telemetry.go`、`platform/http/telemetry.go`、`httpapi/runtime/runtime_tracing.go`、`management/auth/telemetry.go`。
- [x] **Step 2：** 中间件摘除：`management_branch.go:43` 的 ingress 包装、`runtime_branch.go:22,:31` 两处包装。
- [x] **Step 3：** span 调用剥离（删 span 行、保 ctx 流）：`runtime/service.go:344,:350,:496`；`runtime/observability.go:844,:861,:883,:907,:950,:972`；`runtime/runtime.go:644,:671,:1000,:1358`；核查 `gateway/core/context.go`。
- [x] **Step 4：** asyncmetrics 调用剥离（10 文件）：`managementjobs/jobs.go`、`logretention/maintenance.go`、`auth/proxy_key_usage_writer.go`、`runtime/feedback_pipeline.go`、`runtime/runtime_side_effects.go`、`managementsideeffects/outbox.go`、`runtime/telemetry_outbox.go`、`background/scheduler.go`（`platform/email/*` 两处若 R3 已删则不存在）。
- [x] **Step 5：** 进程装配：`main.go` :17,:33,:91-98,:129,:134,:154-161 的 providers 构建/关停舞步；`lifecycle/app.go` :30,:41,:55,:133-135 与 `production.go` :44,:53,:80,:87,:513-514 的 `TelemetryShutdown` 钩子。**保留** `production.go:140-158,:401,:409` 池 lane。
- [x] **Step 6（G2 裁决）：** `config.go` 的 `TelemetryConfig` 及子结构、`bootstrap.go` 的 telemetry 解析**保留**（纯字符串配置，无 OTel import；现网 config.json 含该节）——加 `// ponytail: telemetry config parsed for live config.json compat; exporters removed`。仅当校验逻辑引用已删导出器构造时改为宽松通过。
- [x] **Step 7：** `go.mod` 删 9 个直接 OTel 模块 + `go mod tidy`（间接依赖 :33-35 自清）。测试修复：`tests/integration/management_audit_stats_phase7_test.go`、`tests/runtime/request_logs_contract_test.go` 的 telemetry import；**整删 `tests/contract/s15_observability_contract_test.go:558-574` 的 `TestManagementMetricsEndpointRemovedAfterOTLP`**（它逐字读 `management_branch.go`/`db/pools.go`/`db/telemetry.go` 源码文本，变体 1 落地即挂）。前端 `StartupTelemetrySection.tsx` 已随 R2 消失，确认即可。
- [x] **Step 8：** 文档：`API_SPEC.md:202` telemetry 重启字段行、`ARCHITECTURE.md` OTel 段、`platform/AGENTS.md`、`runtime/AGENTS.md`、README:21 OTel 特性段改写。验证：`rg -in "opentelemetry|asyncmetrics|startRuntimeSpan" backend --glob '!docs/**'` → 0；`grep -c opentelemetry backend/go.mod` → 0；标准验证。
- [x] 提交：`git commit -m "feat!: remove OTel telemetry path"`（或变体 2：`build: drop gRPC OTLP exporter pair`）

### Task 12: R7 — Realtime WebSocket 退役（决策门 D2；顺序：前端 → 后端 → lane/nginx）

**此任务锁定 E1 通道 = webhook-via-outbox，并使 E4 的"实时 tail"项作废。**

- [x] **Step 1（前端轮询替换，先行——让客户端先停止拨号）：**
  - `pages/dashboard/useDashboardRealtime.ts` 重写：**保持公开返回形状**（`refreshDashboard`、`isRefreshing`、`recentNewIds`、`clearRecentRequestHighlight`、`metricsHighlighted`；删 `connectionState`/`isSyncing`），内部把 `useRealtimeData({channel:"dashboard"...})` 换成 30s `setInterval`（hook 内——`hooks/AGENTS.md:35` 禁止组件内 interval），调用既有 `useDashboardBootstrapData.ts` 的 `fetchDashboardData({ silent: true })`（:85-86 打 `api.stats.dashboard()` + `dashboardRecentActivity({limit:12})`），沿用 `shouldApplyDashboardSnapshotRevision`/`mergeDashboardRecentActivityItem` 调和——这正是 `pages/dashboard/AGENTS.md:56` 规定的重连 REST 调和路径。
  - `pages/statistics/useUsageStatisticsPageData.ts`：删 :20-21 imports、:114 `buildRealtimeSnapshotSource`、:300-310,:392-410 realtime 接线；换 30s interval 调既有 `api.stats.usageSnapshot({ preset: state.selectedTimeRange })`（:582）喂同一 `acceptSnapshotSource`。
  - 整删：`lib/websocket.ts`、`lib/websocket/` 目录、`hooks/useRealtimeData.ts`、`components/WebSocketStatusIndicator.tsx`（先 `rg -l WebSocketStatusIndicator frontend/src` 清渲染点）、`pages/statistics/useUsageStatisticsRealtimeData.ts`。
- [x] **Step 2（后端）：** 整删 `httpapi/realtime/`（10 文件）、`management/auth/realtime.go`、`tests/runtime/realtime_test.go`；接线：`management_branch.go` :21/:36/:47/:79-80、`dependencies.go` :20/:43、`auth/service.go` :52-53,:178-193 撤销监听注册表及发布点（grep `publishRealtimeAuthRevocation`）、`production.go:409` 去 `DashboardUpdates`/`AnalyticsUpdates` 与两个 publisher 构造，再顺着 `rg -n "DashboardUpdates|AnalyticsUpdates" backend/internal` 删 runtime 侧 Options 字段与发布点（**编译扇出最大的一步**）。
- [x] **Step 3（lane + nginx，G2 适用）：** `db/pools.go` 删 `Realtime` lane（:27,:54,:60,:69,:101,:131,:142,:203）与 `PostgresLaneRealtime`；`config.go` 的 `Realtime DatabasePoolBudget` 字段**保留解析**、忽略取值（现网 config.json 可能含 `database.pools.realtime`）——ponytail 注释。`docker/nginx.conf.template:64-73` 删 `/api/realtime/ws` 块。
- [x] **Step 4：** 测试与文档：`tests/priority/db/db_lane_isolation_test.go` 等 realtime lane 引用；`frontend/tests/lib/analytics_websocket_contract.test.mjs`、**`websocket_contract.test.mjs`（557 行）、`dashboard_realtime_reconnect_contract.test.mjs`（230 行）** 删（三者都在 glob 后的 test:lib 即 CI 里，漏删后两个则本任务落地即红）；e2e `analytics-websocket-native.spec.ts`、`launcher-same-origin-realtime.spec.ts` 删。i18n：`rg "messages\." frontend/src/components/WebSocketStatusIndicator.tsx frontend/src/hooks/useRealtimeData.ts`（删除前枚举）得出的 key 从 en/zh 删。文档：`API_SPEC.md` :15、:202 realtime 池行、:1649、§8 整章（:2621-2725+）；`ARCHITECTURE.md`；`pages/dashboard/AGENTS.md:54-56` 改写；hooks/lib/components/statistics/src 各 AGENTS。README:96 nginx 段的 `/api/realtime/ws` 提法。
- [x] **Step 5：** 验证：`rg -in "realtime|websocket" backend/internal frontend/src --glob '!**/AGENTS.md'` → 0；标准验证；手测仪表盘 Network 面板每 ~30s 一次 `GET /api/stats/dashboard`、无 `/api/realtime/ws` 尝试、近期活动按 `request_log_id` 去重正常。
- [x] 提交：`git commit -m "feat!: retire realtime websocket in favor of REST polling"`

### Task 13: R8 — 路由拓扑图降级为纯列表（决策门 D3；懒方案）

**保留 routing 标签页与全部数据管道，只删 xyflow 桌面渲染，无条件渲染既有 `RoutingDiagramMobileList`。**

- [x] **Step 1：** 整删 `frontend/src/pages/dashboard/routing-diagram/` 下 7 个 flow 文件：`RoutingDiagramFlow.tsx`、`RoutingDiagramFlowEdge.tsx`、`RoutingDiagramFlowNode.tsx`、`routingDiagramFlowState.ts`、`routingDiagramFlowLayout.ts`、`routingDiagramFlowEdgeStyle.ts`、`routingDiagramLayout.ts`。**保留**：`routingDiagramContracts.ts`、`RoutingDiagramMobileList.tsx`、`RoutingDiagramLegend.tsx`、`RoutingDiagramVisualizationShell.tsx`、`RoutingDiagramInspectorContent.tsx`、`routingDiagramPresentationUtils.ts`。
- [x] **Step 2：** `RoutingDiagramCard.tsx` 删桌面分支（:218 `data-testid="routing-diagram-desktop-pending"` 区域）与桌面/移动开关，恒走列表路径；barrel `routingDiagram.ts` 去已删模块再导出。**核查** `RoutingDiagramVisualizationShell` 的视口分支能单列降级。
- [x] **Step 3：** `main.tsx:7` 删 `import "@xyflow/react/dist/style.css"`；`package.json:29` 删 `"@xyflow/react"`。数据管道（`useDashboardBootstrapData`/`useDashboardPageData` 的 `routingDiagramData`、`DashboardPage.tsx:124-126` props）不动。
- [x] **Step 4：** i18n：仅删被已删文件独占引用的 key（en 有 97 处 `routing` 命中、zh 40 处——对幸存引用做差集；`RoutingDiagramShell.tsx:31-73` 与 MobileList 用的列表/空态 key 保留）。e2e `dashboard-routing-shell.spec.ts` 删（已于测试精简批次 1 提前完成）；**⚠️ CI 级：`frontend/tests/lib/dashboard_routing_flow_layout_contract.test.mjs`（1,406 行）在 CI 的 test:lib 里且 ≥11/18 个测试加载 Step 1 删除的模块——必须同 PR 删除**（graph/inspector/mobile 相关 ~80–400 行可拆出保留）。AGENTS：`routing-diagram/AGENTS.md`、`pages/dashboard/AGENTS.md:57` 改写为"纯列表"。
- [x] **Step 5：** 验证：`rg -n "xyflow" frontend` → 0；标准验证 frontend；手测 `/observe?tab=routing` 渲染分区表、点节点仍开 inspector。
- [x] 提交：`git commit -m "feat: replace routing diagram flow rendering with plain list"`

### Task 14: R9a — 连接健康探测路由移除

- [x] **Step 1：** 整删 `management/connections/health.go`、`health_test.go`、`frontend/src/pages/model-detail/useConnectionHealthChecks.ts`、e2e `model-detail-connection-dialog-probe.spec.ts`（已于测试精简批次 1 提前完成）、`frontend/tests/model-detail/connection_probe_behavior_contract.test.mjs`；**另删两个直打该路由的后端测试**（路由删除后无法编译）：`backend/tests/runtime/runtime_phase4_health_check_test.go`（329 行，:55,:94）与 `backend/tests/contract/connection_s10_contract_test.go` 的 health 部分（:51-120,:340，~160 行）。
- [x] **Step 2：** `connections/service.go` 删 4 条挂载及 handler：:98（health-check-preview legacy 404）、:103（`POST .../connections/{connection_id}/health`）、:104、:114（legacy reject）；删 Service 结构体的 `persistedHealthChecks` singleflight 字段。
- [x] **Step 3：** 契约 JSON :17 删 health 行（grep `health` 确认含 legacy 行）；前端 `lib/api/management.ts:375` 方法 + :10 类型 import、`lib/types/routing.ts` 的 `HealthCheckResponse`、`useModelDetailDataSupport.ts` apply helpers、`ConnectionDialog.tsx` 按钮/props、`ModelDetailFeaturePage.tsx` + `useModelDetailFeatureData.ts` 接线（prop 名先 `rg -n "HealthCheckResponse" frontend/src` 枚举）；i18n 删 ConnectionDialog 的 health-check 文案 key；`API_SPEC.md` 对应段。README:24 成功率徽章句不动（那是请求数据驱动的，非探测）。
- [x] **Step 4：** 验证：`rg -in "healthcheck" backend/internal/httpapi/management/connections frontend/src` → 0（平台 `/health` 存活探针无关，保留）；标准验证。
- [x] 提交：`git commit -m "feat!: remove connection health probe route"`

### Task 15: R9b — @dnd-kit 换上移/下移按钮

- [x] **Step 1：** `pages/endpoints/useEndpointReorder.ts` 重写：删 :12-13 imports、sensors、拖拽状态、`arrayMove`；**API 语义不变**——`api.endpoints.movePosition(Number(id), toIndex)` + 乐观 `setEndpoints`/`setSharedEndpoints(revision, ...)` + 失败回滚（沿用现 `handleDragEnd` 内代码）。暴露 `moveUp(id)`/`moveDown(id)`（`toIndex = index ∓ 1`）；保留 `canReorder`（`endpoints.length > 1 && !reorderInFlight && !filtersActive`）。**后端零改动。**
- [x] **Step 2：** `EndpointCard.tsx` :2-3 删 dnd imports、:205-230 删 `useSortable`/drag-handle props，换两个图标按钮（首/末禁用，样式循 `frontend/DESIGN.md`）；`features/endpoints/EndpointsFeaturePage.tsx:1-2` 删 `DndContext`/`DragOverlay`/`SortableContext` 包装；`package.json:22-24` 删三个 `@dnd-kit/*`。
- [x] **Step 3：** i18n **新增** key（G3 三处）：`endpoints.moveUp`/`endpoints.moveDown` aria 标签；既有 `endpointsData.reorderedFailed` 保留。
- [x] **Step 4：** 验证：`rg "dnd-kit" frontend` → 0；标准验证 frontend；手测移动一个端点，确认 movePosition POST 发出、刷新后顺序保持。
- [x] 提交：`git commit -m "feat: replace endpoint drag reorder with move buttons"`

### Task 16: R9c — 过程性文档归档

- [x] **Step 1（硬依赖先解）：** `backend/tests/integration/dashboard_contract_docs_test.go` :13,:27,:36 读取并断言 SMOKE_TEST_PLAN.md 内容——同 PR 删除该测试（或改写为不依赖文档）。
- [x] **Step 2：** `git rm docs/SMOKE_TEST_PLAN.md docs/TEST_CASE_GENERATION_METHODOLOGY.md`（或 `git mv` 至 `docs/archive/`）；改 `README.md:145,147` 链接、根 `AGENTS.md:87` 清单、`docs/AGENTS.md` :15,:17,:22,:40。
- [x] **Step 3：** 验证：`rg -n "SMOKE_TEST_PLAN|TEST_CASE_GENERATION" . --glob '!docs/archive/**' --glob '!docs/DEVELOPMENT_DIRECTION.md'` → 0；`cd backend && go test ./tests/integration` 通过。
- [x] 提交：`git commit -m "docs: archive process docs and drop doc-content test"`

---

# 阶段 P2：核心增强

### Task 17: E4 — 请求日志页修缮（默认 24h、2xx/精确码/错误文本、CSV 导出）

**前置：Task 2 已接通 `to_time`。与 Task 18 共享 `parseRequestLogListParams` 与 WHERE 构建器——本任务先行。**

**Files:**
- Modify（后端）: `domain/stats/types.go:8-21`、`domain/stats/request_logs.go:353-360`、`management/stats/service.go:536-566`
- Modify（前端）: `pages/request-logs/queryParams.ts`、`rewriteRoutes.ts:41-44`、`appRouter.tsx:365-371`、`FiltersBar.constants.ts:40-50`、`FiltersBarPrimaryFilters.tsx`、`useRequestLogsPageData.ts`、`RequestLogsTable.tsx`、新建 CSV helper、`src/test/route-helpers.test.ts`、`pages/statistics/usageStatisticsStorage.ts`

**Interfaces（Produces，Task 18 依赖）：** `RequestLogListParams` 新增 `StatusCode *int`、`ErrorText *string`；查询参数 `status_code=`（精确匹配）、`error_text=`（`error_detail ILIKE`，参数化、实参预包 `%…%`）。

- [x] **Step 1（失败测试）：** 在 Task 2 定位的同一后端测试文件加三个用例：`status_family=2xx` 只回 2xx；`status_code=429` 精确；`error_text=timeout` 命中 `error_detail` 含 timeout 的行。预期 FAIL。
- [x] **Step 2（后端实现）：** `request_logs.go:353-360` 的 switch 加 `case "2xx": status_code BETWEEN 200 AND 299`；`types.go` 加两字段；`parseRequestLogListParams` 解析 `status_code`（strconv.Atoi）与 `error_text`（非空透传）。测试转绿。
- [x] **Step 3（默认 24h，3 锚点 + 测试）：** `queryParams.ts:12-17` `DEFAULTS.time_range: "1h"` → `"24h"`；`rewriteRoutes.ts:44` `catch("1h")` → `catch("24h")`；`appRouter.tsx:365-371` `isDefaultSearchValue` 的 `"1h"` → `"24h"`；`route-helpers.test.ts` 默认断言更新；`usageStatisticsStorage.ts` 的统计页默认 preset 一并翻到 24h。`// ponytail: fixed 24h default, no per-user persistence for request logs — localStorage only if 24h still annoys.`
- [x] **Step 4（前端过滤 UI）：** `queryParams.ts:4-8` `STATUS_FAMILY_OPTIONS` 加 `"2xx"`、`STATUS_ALIAS_OPTIONS` 加 `"success"`、:20-29 双向映射表 `success↔2xx` **两张表同时扩**（别名与 family 不同步 = URL 失联）；zod `rewriteRoutes.ts:41-42` 两个 enum 同扩；`FiltersBar.constants.ts` `getStatusFamilyLabel` 加 2xx；`FiltersBarPrimaryFilters.tsx` 加状态码与错误文本输入框；`queryParams.ts` + `useRequestLogsPageData.ts` 状态管道接到新 API 参数。
- [x] **Step 5（CSV 导出，纯客户端）：** 照抄 `pages/statistics/UsageStatisticsContent.tsx:12-19` 的 `downloadSnapshotJson` Blob 模式写 `downloadRequestLogsCsv(rows)`（列取自 `pages/request-logs/columns.tsx`），按钮放 `RequestLogsTable.tsx` 表头区。`// ponytail: exports current page only (≤500 rows); add a backend streaming endpoint if full-range export is ever needed.`
- [x] **Step 6（i18n + 文档）：** 新 key：`requestLogs.twoHundredsOnly`（列于既有 `fourHundredsOnly`/`fiveHundredsOnly` 旁）、`statusCodeFilterLabel`、`errorTextFilterLabel`、`exportCsv`（`last24Hours` 已存在）。`API_SPEC.md` 请求日志参数表加 `status_code`/`error_text`/2xx；`pages/request-logs/AGENTS.md`。无路由契约改动。
- [x] **Step 7：** 验证：三条 curl（`?status_family=2xx`、`?status_code=429`、`?error_text=timeout`）各自收窄；浏览器新开 `/observe/requests` 直接可见 24h 数据且 URL 干净（24h 作为默认被省略）；CSV 行数 = 可见列表行数；标准验证。
- [x] **Step 8：** 提交：`git commit -m "feat: request-log filters (2xx/status-code/error-text), 24h default, CSV export"`

### Task 18: E2 — 未定价请求下钻（前置：Task 17）

**Files:**
- Modify（后端）: `domain/stats/types.go`、`management/stats/service.go`（`parseRequestLogListParams`）、`domain/stats/request_logs.go` WHERE 构建器
- Modify（前端）: `pages/dashboard/DashboardMetricsGrid.tsx:21-22,:81,:88`、`rewriteRoutes.ts:30-44`、`pages/request-logs/queryParams.ts`、`FiltersBarPrimaryFilters.tsx`、`useRequestLogsPageData.ts`

**Interfaces：** 行数据已带 `priced_flag`/`unpriced_reason`（列表 SELECT `request_logs.go:153` 已含两列，`request_logs.go:193-194` 已返回）；原因枚举（校验 + 前端选项）来自 `runtime_pricing.go`：`PRICING_DISABLED`(:45-47)、`MISSING_TOKEN_USAGE`/`STREAM_USAGE_UNAVAILABLE`(:127-134)、`MISSING_PRICE_DATA`(:136-153)。

- [x] **Step 0（预检）：** `rg -n "unpricedBreakdown" frontend/src` ——`statistics.unpricedBreakdown` key 已存在（en 类型 :1893/值 :3853，zh :1897）且 `UsageBreakdownSection.tsx` 已渲染未定价文案；确认已有展示范围，剩余工作大概率只是仪表盘计数变链接 + 原因过滤，不是新挂件。
- [x] **Step 1（失败测试）：** 后端加用例：`?priced=false` 只回未定价行；`?priced=notabool` → 400；`?unpriced_reason=MISSING_PRICE_DATA` 精确过滤；非法 reason → 400。
- [x] **Step 2（后端实现）：** `types.go` `RequestLogListParams` 加 `PricedFlag *bool`、`UnpricedReason *string`；`parseRequestLogListParams` 解析 `priced=true|false`（非空 `strconv.ParseBool`）与 `unpriced_reason`（对上面四值枚举校验，越界 400）；WHERE 构建器（:340-380 从句块）追加 `priced_flag = $n` 与 `unpriced_reason = $n`。测试转绿。
- [x] **Step 3（仪表盘链接）：** `DashboardMetricsGrid.tsx:21-22` 处 `unpricedRequestCount > 0` 的明细文案改为 TanStack 链接指向 `/observe/requests?priced=false&time_range=30d`（:81/:88 的 `showPricingTemplatesLink` 逻辑保留）。
- [x] **Step 4（过滤 UI）：** zod：`requestLogSearchSchema` 加 `priced: z.enum(["all","true","false"]).catch("all")`、`unpriced_reason: searchStringSchema.catch("")`；`queryParams.ts` 状态字段/DEFAULTS/`parsePageState`/`stateToParams` 补齐；`FiltersBarPrimaryFilters.tsx` 加定价状态选择器与原因下拉（仅 `priced=false` 时启用）。
- [x] **Step 5（i18n + 文档）：** 新 key：`requestLogs.pricedFilterLabel`/`pricedOnly`/`unpricedOnly`/`unpricedReasonLabel` + 四个原因标签（`reasonPricingDisabled`/`reasonMissingTokenUsage`/`reasonStreamUsageUnavailable`/`reasonMissingPriceData`）。`API_SPEC.md` 参数表；`pages/request-logs/AGENTS.md`。无契约改动（扩展既有 `GET /api/stats/requests` 参数）。
- [x] **Step 6：** 验证：`curl '.../api/stats/requests?priced=false&time_range=30d' | jq '.items | length'` → 现网应为 4（对上仪表盘"4 个未定价"）；`?priced=notabool` → 400；点击仪表盘未定价文案直落已过滤列表；标准验证。
- [x] **Step 7：** 提交：`git commit -m "feat: unpriced request drill-down filters and dashboard link"`

### Task 19: E3 — 价格表 JSON 导入（独立任务）

**Files:**
- Modify: `management/connections/pricing_templates.go`（新 handler）、挂载点（`rg -n '"/pricing-templates"' backend/internal/httpapi/management/connections/` 定位）、`management_route_contract.json`（新行）
- Modify（前端）: `/route/pricing` 页加导入对话框、模型详情连接列表加"缺模板"徽章
- Test: `management/connections/routes_test.go`（沿用既有 pricing-template CRUD 用例模式）

**Interfaces（Produces）：** `POST /api/pricing-templates/import`

```json
Request:  { "mode": "upsert_by_name" | "create_only",
            "templates": [ { "name": "gpt-4o", "pricing_unit": "PER_1M", "pricing_currency_code": "USD",
                             "input_price": "2.5", "output_price": "10", "cached_input_price": "1.25",
                             "cache_creation_price": "0", "reasoning_price": "0", "description": "..." } ] }
Response: { "created": 3, "updated": 2, "skipped": ["name-a"], "errors": [ { "index": 4, "name": "bad", "detail": "invalid input_price" } ] }
```

- [x] **Step 1（失败测试）：** `routes_test.go` 加用例：合法两行导入 → created=2；重跑同载荷（upsert_by_name）→ updated=2 且 created=0（幂等）；坏行 → 整体 400（全或无，单事务）；未知字段 → 400。
- [x] **Step 2（实现）：** 新 `handleImportPricingTemplates`——**逐字复用**既有规范化函数：`buildCreatedPricingTemplate`(:227)、`normalizePricingTemplateName`(:341)、`normalizePricingUnit`(:356)、`normalizePricingCurrencyCode`(:374)、`normalizePricingDecimalString`(:389)；JSON 解码用 `DisallowUnknownFields`（同 `management/loadbalance/routes.go` 的 `decodeStrategyRequest` 模式）；先整组解析、逐行规范化、按 profile 内 name 匹配 upsert、单事务提交。无 schema 变更（`pricing_templates` 表既有列全够用，PER_1M 不变）。`// ponytail: no litellm auto-fetch/mapping — user curates the JSON file; add a converter script only if hand-mapping hurts.`
- [x] **Step 3（契约，正确性关键位）：** 契约 JSON 加行（镜像第 23 行 `/api/pricing-templates` POST）：`{"route_pattern":"/api/pricing-templates/import","methods":["POST"],"profile_scoped":true,...,"invalidates_planning":true}`。**漏掉 `invalidates_planning:true` = 运行时定价快照变陈旧**，这是本任务唯一的正确性红线。
- [x] **Step 4（前端）：** `/route/pricing` 页（`appRouter.tsx:302-308`）加上传/粘贴对话框：客户端 `JSON.parse` + POST + 结果摘要 toast。"缺模板"徽章零后端改动——连接列表响应已带 `pricing_template_id`（`connections/types.go:145`）与 `PricingTemplate` 摘要(:223-227)，`pricing_template_id === null` 即渲染徽章（模型详情连接行；`ConnectionDialog.tsx` 已有「未定价（不跟踪成本）」文案 key `unpricedNoCostTracking` 可参照，样式循 `frontend/DESIGN.md`）。
- [x] **Step 5（i18n + 文档）：** 新 key：`pricing.importTitle`/`importDescription`/`importButton`/`importModeUpsert`/`importModeCreateOnly`/`importResultSummary`(fn)/`importInvalidJson`/`connectionMissingTemplateBadge`。`API_SPEC.md` 新端点段；`connections/AGENTS.md`。
- [x] **Step 6：** 验证：curl 导入 → 计数正确；重跑 → `updated` 非 `created`；s11 契约测试与路由 parity 绿；标准验证；手测粘贴导入 10 模板价目单。
- [x] **Step 7：** 提交：`git commit -m "feat: pricing template JSON import with upsert-by-name"`

### Task 20: E5 — 延迟趋势图（p50/p95 over time；独立任务）

**Files:**
- Modify（后端）: `domain/stats/types.go`（:374-406 旁新增三类型 + :483 响应字段）、`domain/stats/snapshot.go`（:704-770 旁新增 series 构建器、:520-563 填充点）
- Modify（前端）: `pages/statistics/sections/UsageTrendsSection.tsx`（`data-testid="usage-trends-grid"` 网格内新挂一图）、`lib/types/usage-statistics.ts`

**Interfaces（Produces）：** `GET /api/stats/usage-snapshot` 响应扩展（无新路由）：

```go
type UsageLatencyTrendPoint struct { BucketStart time.Time `json:"bucket_start"`; P50MS *int `json:"p50_ms"`; P95MS *int `json:"p95_ms"` }
type UsageLatencyTrendSeries struct { Key string `json:"key"`; Label string `json:"label"`; Points []UsageLatencyTrendPoint `json:"points"` }
type UsageLatencyTrends struct { Hourly []UsageLatencyTrendSeries `json:"hourly"`; Daily []UsageLatencyTrendSeries `json:"daily"` }
// 响应结构体（types.go:483 RequestTrends 旁）加：LatencyTrends UsageLatencyTrends `json:"latency_trends"`
```

- [x] **Step 0（预检）：** `rg -n "type snapshotEvent" -A 25 backend/internal/domain/stats/snapshot.go` 确认 `snapshotEvent` 是否携带 response-time（TTFT 已确认在——:940 从 `group.ttftValues` 算 p50/p95）；若无则在快照事件装载查询里加 `response_time_ms` 列（列在 `request_logs` 上，SELECT 见 `request_logs.go:153`）。
- [x] **Step 1（失败测试）：** `rg -l "usage-snapshot|buildRequestTrendSeries" backend/tests backend/internal/domain/stats` 定位既有快照测试，加用例：造 3 条已知 `response_time_ms` 的事件落同一小时桶，断言 `latency_trends.hourly` 对应桶的 `p50_ms`/`p95_ms` 等于手算值，且桶起点与 `request_trends` 一一对齐。
- [x] **Step 2（实现）：** 仿 `buildRequestTrendSeries`（`snapshot.go:704-770`，用 `bucketRange`/`bucketFloor`/`bucketMinutes`，`common.go:412-460`）写：

  ```go
  func buildLatencyTrendSeries(events []snapshotEvent, startAt *time.Time, endAt time.Time, granularity string) []UsageLatencyTrendSeries
  ```

  每桶收集 `response_time_ms`，用既有 `percentileContInt`（`common.go:153`）出 p50/p95。现有即时 p95（`aggregates.go:145-147,:212-214`）原样保留。`// ponytail: Go-side percentile over loaded events, same as existing trends — move to SQL percentile_cont only when T5 fixes the load-all-events pattern wholesale.`
- [x] **Step 3（前端）：** 走双系列复用路线——`charts/UsageTrendChart.tsx` 是单值/点的 recharts AreaChart，把 p50、p95 作为两个 series entry（`key:"p50"`/`key:"p95"`）喂进既有组件，**零新图表代码**；挂到 `UsageTrendsSection.tsx` 网格（既有两个 `UsageTrendChart` 实例就是逐行拷贝模板，含 `onSetChartGranularity` 粒度接线）；类型补 `lib/types/usage-statistics.ts`。**画图前查阅 dataviz 技能**（配色/空态规范）。
- [x] **Step 4（i18n + 文档）：** 新 key（statistics 命名空间，`requestTrendsTitle` 旁）：`latencyTrendsTitle`/`latencyOverTime`/`p50Label`/`p95Label`/`noLatencyData`。`API_SPEC.md` usage-snapshot 响应加 `latency_trends`；`domain/stats/AGENTS.md`、`pages/statistics/AGENTS.md`。
- [x] **Step 5：** 验证：`curl '.../api/stats/usage-snapshot?...' | jq '.latency_trends.hourly[0].points[0]'` → `{bucket_start,p50_ms,p95_ms}` 且桶与 `request_trends` 对齐；分析页渲染新图、空窗显示空态无 NaN；标准验证。
- [x] **Step 6：** 提交：`git commit -m "feat: latency p50/p95 trend series in usage snapshot"`

### Task 21: E1 — 故障转移 incidents 面板 + webhook 告警（本阶段压轴；R7 决策后）

**Files:**
- Modify: `domain/loadbalance/service.go`（`ListEvents`:166 旁新增 `ListIncidents`；`RuntimeCurrentStateProvider`:105-109 加方法）、`domain/loadbalance/runtime_state.go`、`domain/loadbalance/runtime_events.go`（:106,:114,:125 三个 Insert 返回 eventType）、`httpapi/management/loadbalance/observability.go`（:59-64 旁新 handler）、`httpapi/management/loadbalance/service.go:82-95`（挂载）、`httpapi/runtime/feedback_pipeline.go:205-217`（入队点）、`platform/config/config.go` + `bootstrap.go`（`alerting.webhookUrl` 热应用字段）、`production.go`（worker 注册，:428-439 注册环）、`pages/dashboard/DashboardOverviewTab.tsx`（横幅）、`useDashboardPageData.ts`（取数）
- Create: `backend/internal/platform/alerting/`（webhook outbox worker）、`backend/migrations/000011_alert_webhook_outbox.sql`
- Test: 复制 `email_outbox_priority_test.go` 为 `alert_webhook_priority_test.go`（**注意：R3 已删原文件——从 git 历史 `git show <R3^>:backend/tests/priority/outbox/email_outbox_priority_test.go` 取模板**）；handler 用例进 `tests/contract/s11_management_contract_test.go`（包内 `loadbalance/routes_test.go` 已由测试精简交接批次 4 删除）

**Interfaces（Produces）：**
- `GET /api/loadbalance/incidents`（profile-scoped 只读）：

  ```json
  { "active_bans":   [ { "connection_id": 3, "ban_mode": "temporary", "banned_until_at": "...",
                         "cumulative_retry_attempts": 7, "next_retry_at": "..." } ],
    "recent_events": [ /* 与 /api/loadbalance/events 条目同形 */ ],
    "generated_at":  "..." }
  ```

- Webhook POST 载荷：`{"event_type":"banned|unbanned|recovered","connection_id":3,"endpoint_id":1,"model_id":"gpt-...","banned_until_at":"...","occurred_at":"..."}`
- Go 接口：

  ```go
  // domain/loadbalance/service.go — ListEvents 同款执行器，去 model_id 限定
  func ListIncidents(ctx context.Context, exec queryExecutor, provider RuntimeCurrentStateProvider,
      profileID int, limit int, sinceHours int, referenceNow time.Time) (IncidentListResponse, error)
  // RuntimeCurrentStateProvider 新增
  SnapshotActiveBans(profileID int, referenceNow time.Time) []CurrentStateItem
  // platform/alerting
  const WorkerName = background.WorkerName("alert_webhook_worker")
  func NewStore(options Options) *Store   // Options{Pool *pgxpool.Pool, Scheduler *background.Scheduler, WebhookURLProvider ..., Now func() time.Time}
  func (s *Store) EnqueueTx(ctx context.Context, tx pgx.Tx, payload IncidentPayload) error
  ```

- [x] **Step 1（incidents 读端点，先测后写）：** handler 用例：种入 `banned`/`recovered` 事件 + 一条内存 ban 态，`GET /api/loadbalance/incidents` 断言两段都回。实现：`ListIncidents` 的近期事件 SQL = `ListEvents` 的 SQL 去掉 `model_id` 从句、加 `AND event_type IN ('banned','unbanned','recovered','retry_exhausted') AND created_at >= $2`；活跃 ban 走 `SnapshotActiveBans`（在 `runtime_state.go` 实现：遍历持有的连接态，留 `BannedUntilAt` 未来或 `ban_mode=until_reset` 的）。**先 `rg "SnapshotCurrentState" backend` 枚举接口实现者与测试假件**——接口加方法处处要补。挂载 `router.Get("/incidents", s.handleListIncidents)`（service.go:82-95 的 `/loadbalance` 块）。
- [x] **Step 2（契约 + 文档）：** 契约 JSON 加只读行（镜像现第 36 行 events 条目）：`{"route_pattern":"/api/loadbalance/incidents","methods":["GET"],"profile_scoped":true,...}`；`API_SPEC.md` 端点段。
- [x] **Step 3（迁移）：** `000011_alert_webhook_outbox.sql`——新表 `alert_webhook_outbox`，列从 `email_outbox`（000001:184-204）拷贝骨架（`status IN ('queued','sending','sent','dead')`、`attempt_count`、`next_attempt_at`、`locked_by/locked_until`、`idempotency_key`），去 `recipient_email/template/email_secret_ciphertext`，加 `event_type text NOT NULL`、`payload_json jsonb NOT NULL`。`migrations_test.go` 清单加号。`DATA_MODEL.md` 新表章节。
- [x] **Step 4（outbox worker）：** 新包 `platform/alerting/`，从邮件 outbox 模板拷贝 claim/lease/retry/backoff/dead-letter 骨架（模板取自 git 历史，见 Files 注；在世代码可参 `managementsideeffects/outbox.go`）；`WebhookURLProvider` 仿原 `MailerProvider` 模式——发送时经 provider 取 URL（配合热应用）。`// ponytail: single URL, plain POST, no channel framework — add templating only if a second sink appears.` `production.go` :428-439 注册环加一项。
- [x] **Step 5（入队点，事务原子）：** `runtime_events.go` 的 `InsertRuntimeFailureEvent`(:106)/`InsertRuntimeUnbannedEvent`(:114)/`InsertRuntimeRecoveryEvent`(:125) 改为返回落库的 `eventType string`（`banned` vs `retry_scheduled` 的判定在 `buildRuntimeFailureEventPayload`，:161-184）；`feedback_pipeline.go:205-217` 的 `persist()` 内，eventType ∈ {banned, unbanned, recovered} 时同事务 `alertStore.EnqueueTx(ctx, tx, ...)`——outbox 行与事件原子提交。此处本就在调度器任务里（`handleScheduledFeedback`:168），**不在 HTTP 请求路径**，符合仓库"请求路径无内联副作用"红线。返回值变更波及 `runtime_local_state_test.go`——同步修。
- [x] **Step 6（配置字段）：** `config.go` Settings 加 `Alerting AlertingConfig`（`type AlertingConfig struct { WebhookURL string }`）+ `loadCanonicalDefaultSettings`(:252-282) 默认空；`bootstrap.go` :146-163 的 `hotApplyBootstrapField(...)` 清单加 `bootstrapFieldAlertingWebhookURL`（**热应用**）——未分类字段启动硬失败（:74-76），别漏；校验：空=关闭，非空必须 `http(s)://`。
- [x] **Step 7（前端横幅）：** `DashboardOverviewTab.tsx` 在 `<DashboardMetricsGrid .../>` 上方插 `IncidentBannerCard`（`flex flex-col gap-[var(--density-page-gap)]` 容器首子）；取数并入 `useDashboardPageData.ts` 既有 REST 聚合；仅当 `active_bans.length > 0` 或 24h 内有 `banned`/`retry_exhausted` 事件时渲染；样式循 `frontend/DESIGN.md`。i18n 新 key：`dashboard.incidentBannerTitle`/`incidentActiveBans`(fn)/`incidentRecentFailovers`(fn)/`incidentViewEvents`。
- [x] **Step 8：** 验证：`curl -s '.../api/loadbalance/incidents' | jq .active_bans`（空数组也算通）；`go test ./tests/contract -run S11` 契约绿；`alerting.webhookUrl` 指向 `nc -l 9999`，压死一个上游触发 ban，观察 POST 到达且 `SELECT status FROM alert_webhook_outbox` 走到 `sent`；仪表盘出横幅；标准验证。
- [x] **Step 9：** 提交：`git commit -m "feat: failover incidents endpoint, dashboard banner, webhook alerting via durable outbox"`

---

# 阶段 P3：结构性技术债

### Task 22: T5 — 统计写读一致性（写侧修正 + 删三个读侧补丁 + 掉落死表）

**方向文档锚点修正：** 内存聚合在 `aggregates.go`（`GetStatsSummary`:105、`GetSpending`:511、`UnpricedBreakdown`:536/:553），不在 `rollups.go`（现仅 152 行且生产无调用者）。

- [x] **Step 1（写侧修正）：** 三个读侧规范化器——`request_logs.go:716-733`（`normalizeRequestLogListSpendState`/`DetailSpendState`）、`snapshot.go` 约 :610（`normalizeUsageEventPricingCoherence`）、`snapshot.go:643-685`（`normalizeObservedSpendCoherence`/`FXCoherence`）——它们修补的写入端在 `runtime/observability.go:1903`（request_logs 的 64 参 INSERT）与 `:1981`（usage_request_events 的 53 参 INSERT），取值源头是 `runtime_pricing.go`。**把同一套一致性规则（逻辑照搬勿重写）收敛为一个 helper，作用于定价结果、在两个 INSERT 之前统一应用**；然后删除三个读侧补丁。
- [x] **Step 2（历史数据修正迁移）：** `000009_stats_write_coherence.sql`——对 `request_logs`/`usage_request_events` 应用同规则的 UPDATE（否则读补丁必须为历史行保留）。**现网红线：先 `pg_dump`，再用 SELECT 干跑数影响行数。** 写侧修正与读补丁删除**必须同 PR**（只做一半 = 漂移重现）。
- [x] **Step 3（掉落死表，可独立先行）：** 证据：`management_stat_buckets`/`management_stat_refresh_state`（000001:597-608）唯一读写者是 `rollups.go`（`LoadDashboardRollupStats`:50、`RefreshDashboardStatsRollup`:101、:130），生产调用者为零（调度器 `managementjobs/jobs.go:127-132` 只注册 log-retention；仪表盘走 `GetDashboardStatsSummary`，`aggregates.go:166`←`snapshot.go:368,372`）。现网预检 `SELECT count(*) FROM management_stat_buckets;`（预期 0 或纯陈旧行）后：`000010_drop_management_stat_rollups.sql` DROP 两表；整删 `rollups.go`；删 `management_audit_stats_phase7_test.go` :289,:1172,:1175,:1212,:1227 相关段与两处钉死断言（`auditstats_priority_test.go:88,:93`、`unit_priority_contract_test.go:111`）。
- [x] **Step 4（记录不修）：** `request_logs.go:120-158` 每次列表全量 `COUNT(*)` + 全量端点/模型/UA 规则重载——加注释 `// ponytail: 全量 COUNT，日志量上万后换估算或 keyset 分页`，不写代码。
- [x] **Step 5：** 验证：两组 rg 归零（`management_stat_buckets|...|LoadDashboardRollupStats` 于 backend/ 除 migrations 历史；`normalizeObserved*|normalizeUsageEvent*|normalizeRequestLogListSpendState` 于 domain/stats）；标准验证；对 dev 库经运行时路径插入一条"定价但无成本"行，确认 API 返回 `priced=false/MISSING_PRICE_DATA` 且**无读侧补丁参与**。
- [x] **Step 6：** 提交：`git commit -m "fix!: enforce spend coherence at write time, drop dead stat rollup tables"`

### Task 23: T2 — 双路由器统一（前置：T3 已在 P0 落地；建议 R3 后——文件 5/9 直接消失）

**Files:** 23 处 react-router-dom 引用逐一迁移（若 R3 已删 Forgot/Reset 页则为 21 处），最后 `package.json:38` 删依赖。

| # | 文件 | 用到 | 替换 |
|---|------|------|------|
| 1 | `src/App.tsx:3` | `BrowserRouter` | 删；`bootstrapMode` 改由 `window.location.pathname` 计算 |
| 2 | `src/app/router/appRouter.tsx:2` | `CompatNavigate`(:101,:122)、`Routes`/`Route`(:129-132)、`useLocation`(:80) | TanStack `Navigate`（:4 已导入）；特例见下；`window.location` |
| 3 | `src/pages/DashboardPage.tsx:3` | `useNavigate` | TanStack `useNavigate` |
| 4 | `src/pages/model-detail/useConnectionFocus.ts:3` | 类型 `SetURLSearchParams` | 本地类型别名 `(next: URLSearchParams, opts?: {replace?: boolean}) => void` |
| 5 | `src/pages/ForgotPasswordPage.tsx:2` | `useNavigate` | R3 已删则跳过 |
| 6 | `src/pages/dashboard/useDashboardPageState.ts:2` | `useSearchParams` | `useSearch({ from: "/observe" })` + `navigate({ search })`（observeSearchSchema 已有） |
| 7 | `src/components/layout/page.tsx:2` | `Outlet` | children prop（appRouter :107 已传）或 TanStack `Outlet` |
| 8 | `src/pages/statistics/sections/UsageBreakdownSection.tsx:3` | `Link` | TanStack `Link` |
| 9 | `src/pages/ResetPasswordPage.tsx:2` | `useNavigate` | R3 已删则跳过 |
| 10 | `src/components/layout/app-layout/useShellNavigation.ts:2` | `matchPath`(:87)、`useLocation` | `useRouterState().location` + 对 `SHELL_ROUTE_METADATA` 的小型参数模式匹配（或 `useMatches`） |
| 11 | `src/features/models/ModelsTable.tsx:3` | `useNavigate` | TanStack |
| 12 | `src/components/layout/app-layout/AppSidebar.tsx:1` | `NavLink` | TanStack `Link` + `activeProps` |
| 13 | `src/components/layout/app-layout/useAppLayoutState.ts:2` | `useNavigate` | TanStack |
| 14 | `src/pages/settings/useAuthenticationSettingsData.ts:2` | 类型 `NavigateFunction` | 窄化回调类型 `(to: string) => void` |
| 15 | `src/components/SpendTrustIndicator.tsx:1` | `Link` | TanStack `Link` |
| 16 | `src/features/models/detail/useModelDetailFeatureData.ts:2` | `createSearchParams` 及类型 | 原生 `new URLSearchParams(...)` + 本地类型 |
| 17 | `src/components/layout/app-layout/SiteHeader.tsx:2` | `Link` | TanStack `Link` |
| 18 | `src/features/models/detail/ModelDetailFeaturePage.tsx:2` | `createSearchParams` 及类型 | 原生 + 本地类型 |
| 19 | `src/pages/settings/useSettingsPageData.ts:2` | `useNavigate` | TanStack |
| 20 | `src/pages/request-logs/RequestLogDetailSheet.tsx:1` | `Link` | TanStack `Link` |
| 21 | `src/pages/request-logs/useRequestLogPageState.ts:2` | `useSearchParams` | `useSearch({ from: "/observe/requests" })`（requestLogSearchSchema 已有） |
| 22 | `src/pages/request-logs/RequestLogAuditPage.tsx:2` | `Link`、`useParams`、`useSearchParams` | 路由 props（`useTanStackParams`/`useTanStackSearch`，requestAuditSearchSchema 已有）+ TanStack `Link` |
| 23 | `src/pages/LoginPage.tsx:2` | `Navigate`、`useLocation`、`useNavigate` | TanStack 对应物；`location.state.from` 回跳流改 TanStack navigate `state` 或 `redirect` search 参数 |

- [x] **Step 1（特例 LegacyRequestAuditCompat）：** `appRouter.tsx:127-134` 的嵌套 `Routes` 只为在 TanStack 路由 `/observe/requests/$requestId/audit`(:314-319) 内借 react-router 提参。删除它：`ProtectedRequestAuditRoute`(:228-234) 改用 `useTanStackParams({ from: "/observe/requests/$requestId/audit" })` 取 `requestId` 作 prop 传入 `RequestLogAuditPage`（须与 #22 同 PR）。第二个 `/request-logs/...` 模式已随 T3 死亡。
- [x] **Step 2：** 按表逐文件迁移；`App.tsx` 收尾——`RoutedAuthProvider`（`appRouter.tsx:79-84`）改用 `window.location.pathname` 后，`BrowserRouter` 包装即可摘除：

  ```tsx
  <QueryClientProvider client={queryClient}>
    <RoutedAuthProvider>
      <RouterProvider router={router} />
    </RoutedAuthProvider>
  </QueryClientProvider>
  ```

- [x] **Step 3：** `package.json:38` 删 `"react-router-dom"`；`pnpm install`。**回归重点**：请求日志页 search 参数往返——序列化由 `appRouter.tsx:360-388` 的 `parsePlainSearch`/`stringifyPlainSearch` 独家拥有，改造的 hook 里**不得**再造序列化。
- [x] **Step 4：** 文档：`ARCHITECTURE.md:64` 去 BrowserRouter 提法；相关前端 AGENTS。验证：`rg -l "react-router-dom" frontend/src frontend/package.json` → 0；标准验证；手测 `/observe`、`/models/$id?tab=…`、`/observe/requests?request_id=…`、审计页深链。
- [x] **Step 5：** 提交：`git commit -m "refactor!: unify on TanStack Router, drop react-router-dom"`

### Task 24: T6 — 两个小包正名（前置：R1、R9a 已删两个引用点）

- [x] **Step 1：** access-target glossary constants/helpers now live in `domain/modelrouting`; stored `target_type = "connection"` remains unchanged, and the obsolete package directory is gone.
- [x] **Step 2：** provider API-family/auth helpers now live under `backend/internal/providerauth`; imports, package declarations, and tests were renamed together.
- [x] **Step 3：** old package-name references are absent from backend, docs, and root AGENTS surfaces; focused backend verification ran.
- [x] **Step 4：** Task commit created.

### Task 25: T8 — i18n 反转为 zh-CN 单语（决策门 D6；建议在 R2 与各页面删除之后——两文件已被动瘦身）

- [x] **Step 1：** 类型推导翻转：`zh-CN.ts` 去掉 `import type { Messages } from "./en"`，保持纯对象字面量，加 `export type Messages = typeof zhCNMessages;`（字面量推导把叶子定型为 `string`；**不要 `as const`**——那会把每条消息定成字面量类型）。
- [x] **Step 2：** 新建 `src/i18n/messages/index.ts` 再导出 `Messages`，把 9 个 `@/i18n/messages/en` 导入点改为一行 diff：`streamTelemetry.ts:1`、`useShellNavigation.ts:4`、`UsageControlsBar.tsx:4`、`staticMessages.ts:2-3`（含值导入）、`LocaleProvider.tsx:8`（含值导入）、`locale-context.ts:3`（`startupFieldMetadata.ts`、`navigationProfileConfig.ts` 已随 R2/R4 消失）。
- [x] **Step 3：** 删除 `en.ts` 整文件（−3,866 行）；locale 管道塌缩：`format.ts:1` `type Locale = "zh-CN"`、`:3` `DEFAULT_LOCALE`、`:6-26` `normalizeLocale`/`resolveInitialLocale` 塌为常量；`LocaleProvider.tsx` 的 `MESSAGES_BY_LOCALE` 去 en 项；`staticMessages.ts` 直接 `return zhCNMessages;`；`rg setLocale` 清切换 UI。
- [x] **Step 4：** `tests/lib/model_dialog_i18n_contract.test.mjs` :18-19,:27-28,:36-37 重写为仅 zh-CN。约定文档翻转：`frontend/AGENTS.md`/`frontend/src/i18n/AGENTS.md`——"en.ts 是类型源"改为"zh-CN.ts 是唯一语言与类型源"；**本计划 G3 约束自此失效，新 key 只需 zh-CN 一处。**
- [x] **Step 5：** 验证：`rg -n "messages/en|enMessages" frontend/` → 0；`pnpm run build`（tsc 是真正的门——所有 `Messages` 消费者对新类型源复检）+ 标准验证 frontend。
- [x] **Step 6：** 提交：`git commit -m "refactor!: invert i18n to zh-CN single locale"`

---

## 附录 A：与方向文档的差异裁决（执行者必读）

1. **迁移 squash 作废**（G1）：000002/000007 是已应用历史，永久保留；`migrations_test.go` 的版本清单断言随之永久有效。
2. **`rollups.go` 锚点漂移**：方向文档引的 `rollups.go:898-948`/`:549-555` 实际在 `aggregates.go`（p95 :145-147/:212-214，UnpricedBreakdown :536-556）；`rollups.go` 本体是生产死码（Task 22 删）。
3. **配置 schema 只加不减**（G2）：R3 的 mail、R6 的 telemetry、R7 的 realtime 池预算、R1 的 bundle key——四处解析全部保留，只删行为。
4. **`request_logs_contract_test.go` 在 `tests/runtime/` 不在 `tests/contract/`**；其 absence 循环 :247-251 只有 `"context_routing"` 一项是残留。
5. **E1 的 outbox 模板**在 R3 中被删——实现 E1 时从 git 历史取 `email_outbox` 模板，或参照在世的 `managementsideeffects/outbox.go`。
6. **e2e 计 47 个 spec**；9 个孤儿 lib 测试清单已核实为准确，但其通过性在 `pnpm install` 前不可知（Task 5 的 Step 3 是首次真实运行）。
7. 测试精简已执行，见 TEST_REDUCTION_HANDOFF.md

## 附录 B：删除总量预期（验收参考）

| 批次 | 预期净删 |
|------|---------|
| P0 | ~700 行（测试残留 + legacy 路由 + 文档） |
| R1+R2 | ~10k 行（后端 6.5k + 前端 3.5k，含测试） |
| R3+R4 | ~6k 行 + 4 张表 + 3,571 行测试换 <200 行 |
| R5–R9 | ~5–7k 行 + 2 个前端依赖组 + 9 个 Go 模块（视 D1/D2/D3） |
| P3 | T2 删一个路由器依赖；T5 删 2 表 1 文件；T8 −3.9k 行（视 D6） |
