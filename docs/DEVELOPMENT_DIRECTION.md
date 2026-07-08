# Prism 开发方向建议

> 生成日期：2026-07-07 · 基于 main @ c42b3984 的全仓调研 + 线上实例（http://192.168.1.222:8088，v0.4.7）实地观察。
> 本文是长期参考文档；落地时的具体执行计划请另行拆分，不要把过程性笔记回写到本文。

## 0. 一句话结论

Prism 的核心价值链是：**代理运行时（多上游转发 + 负载均衡/故障转移）→ 请求日志 → 用量统计与费用估算**。这条链只占后端约 36k/87k LOC；其余约一半代码在维护单人局域网部署中从未启用的外围能力（认证/WebAuthn/SMTP、配置包导入导出、多配置档案、启动配置 API 等）。建议方向：**先做减法（约 15k–20k LOC 可删或冻结），把省下的维护带宽投入故障转移可见性与计价体验这两个核心增强，同时清掉几处结构性技术债（双路由器、删除残留、统计读写不一致）**。

## 1. 调研依据

**代码侧**（并行深度审计，全部结论附文件路径）：

- 后端 `internal/` ~86.8k LOC，`backend/tests/` 另有 ~36.9k；全仓 Go 代码约 49% 是测试。
- 前端 `src/` ~52.8k LOC，`tests/` ~23k（其中 Playwright e2e ~17.5k **不在 CI 中**，commit `7d0386e2` 移除了最后一个 e2e job）。
- 管理 API 13 个子包共 ~24.6k LOC（`backend/internal/httpapi/management/`，挂载于 `platform/http/management_branch.go:33-89`，62 个路由模式）。

**线上侧**（192.168.1.222:8088 实地观察）：

- 身份验证已禁用；仅一个 "Default" 配置档案；界面用中文。
- 3 个端点（Sub-CPA-B / Free-CPA-A / DeepSeek）→ 11 个终端目标 → 6 个模型；10 个价格模板在实际维护（含缓存输入/推理价格字段）。
- 流量极低：24h 4 个请求、30 天 ~17 个请求、支出 $1.09；仅 OpenAI 家族流量；**存在 4 个"未定价请求"**。
- OpenAI 家族的审计 + 正文捕获是**开启**状态——审计功能在真实使用中，不能按"外围"处理。
- 体验痛点：请求日志页与分析页默认"最近 1 小时"，低流量下打开即空列表，每次都要手动放宽时间范围。

## 2. 建议移除或冻结的功能（按"省下的维护成本 ÷ 移除难度"排序）

### R1. 配置包导出/导入（configbundle）——最容易的大删除

- **现状**：后端 `management/configbundle/` 12 个文件 ~4.3k LOC（含 `crypto.go` 密钥加密、`preview_tokens.go` 预览令牌、`import.go` 1,338 行图校验），前端 `lib/configImportValidation.ts`、`pages/settings/useConfigBackupData.ts` 及备份 UI，合计全栈 **~5k LOC**、4 条路由。
- **原因**：单人部署的"备份"用 `pg_dump` 一条命令即可替代；带密钥导出、脱敏包、预览令牌、版本化 bundle 图校验都是为多环境迁移场景设计的，从未被日常使用。它是自包含叶子包，对运行时零依赖——删除风险最低、收益最大。
- **怎么做**：删 `management/configbundle/`，从 `management_branch.go` 摘除挂载；删前端备份区块与 `configImportValidation.ts`；删 `settings-config-export/import.spec.ts` 与 `package.json` 里的 `test:config` 脚本；在 README 写一行"备份 = `pg_dump` + `config.json` 文件拷贝"。

### R2. 设置页瘦身：启动（Startup）标签页 + bootstrapconfig API

- **现状**：`/system/settings` 是全站最大前端页面（~8.8k LOC / 55 文件），超过请求日志 + 模型页之和；其中仅 `costing/`（532 LOC，计价货币/FX）与时区是核心。启动标签页 `features/settings/startup/` 独占 2,930 LOC，后端对应 `management/bootstrapconfig/` 2,222 LOC——它们做的事是**在网页里编辑磁盘上那个 `config.json`**（含热应用分类、脱敏元数据、危险变更确认令牌）。
- **原因**：单人部署直接改文件 + 重启即可；为"网页改启动配置"维护一整套 revision/etag/确认令牌/热应用能力报告，收益与复杂度严重倒挂。
- **怎么做**：删启动标签页与 `bootstrapconfig/`（`platform/config/` 的文件解析/热应用内核保留，运行时仍需要）；设置页保留：计费与货币、时区、审计控制、日志保留。`docs/API_SPEC.md` 同步移除 `/api/config/bootstrap`。
- **注意**：审计与隐私区块（含请求头屏蔽、UA 客户端规则）**保留**——线上在用，见 §4。

### R3. 认证链瘦身（保留代理 API 密钥，删掉从未启用的部分）

- **现状**：`management/auth/` 3,629 LOC / 16 文件，独占 **7 张表**（`app_auth_settings`、`login_throttle_ledger`、`refresh_tokens`、`password_reset_challenges`、`webauthn_challenges`、`webauthn_credentials`、`proxy_api_keys`），含**完整 WebAuthn/通行密钥实现**（`000001_initial_schema.sql:1311,1344`）；配套邮件栈 `platform/email/` + `outbox/` ~1.6k LOC + `email_outbox` 表（唯一消费者就是密码重置/恢复邮箱验证）；前端 `/auth/*` 三个页面。线上**认证整体处于禁用状态**。
- **原因**：这是"给公网 SaaS 准备的账号体系"装在了一个 README 明言"不要暴露公网、请套反代"的局域网工具上。真正被 coding agent 用到的只有**代理 API 密钥**（`proxy_api_keys` + 运行时校验 + 用量归因）。
- **怎么做**（保守分两步）：
  1. 先删无争议部分：WebAuthn 全部、密码重置/恢复邮箱流程、`platform/email/` + `outbox/` + `email_outbox`/`password_reset_challenges` 表、bootstrap 里的 `mail` 配置块与 SMTP 启动校验、前端 forgot/reset 页面。
  2. 操作员登录（用户名密码 + session/JWT + 登录限流）可再观察：若确认永远靠反代兜底，连同 login/refresh/throttle 一起删，前端去掉 AuthProvider 公共/受保护分叉；若想留个开关，只保留最简 cookie session。
- **连带收益**：`backend/tests/contract/auth_control_plane_test.go`（2,000 LOC）大部分随之退役。

### R4. 多配置档案（profiles）：冻结而非根除

- **现状**：`management/profiles/` 650 + `internal/profiledomain/` 417 LOC，前端侧栏档案切换器 + `X-Profile-Id` 头；**每张业务表都带 `profile_id` 列**（`000001_initial_schema.sql:85,137,323,631,943`…）。线上只有一个 Default 档案。最大单个后端测试文件 `backend/tests/runtime/profile_scope_test.go`（3,571 LOC）就是在测多档案作用域。
- **原因**：单人单档案下，整个"选中档案 vs 活跃档案"的概念税（UI、header、每个查询的作用域解析）没有回报。但 `profile_id` 列已渗透全部存储层，把维度本身拆掉要动每个 store 和迁移——不划算。
- **怎么做**：删档案 CRUD 路由与前端切换器，后端固定解析为 Default（id=1），保留列与索引不动（`// ponytail: profile_id 列保留，多租户需求出现时再解冻`）；`profile_scope_test.go` 缩减为"默认档案恒定"一个用例。

### R5. 图片生成/编辑路由（决策点：确认无此需求后删）

- **现状**：图片生成/编辑运行时路由已移除；OpenAI 保留 models、Chat Completions、Responses、Responses input_tokens、Responses compact。
- **原因**：coding agent 场景用不到图片生成；multipart/媒体钩子是运行时里独有的一条特殊路径，删掉后运行时钩子族更均质。
- **执行结果**：已确认历史使用为零并删除图片运行时路径、媒体钩子、provider 适配和对应测试。**注意**：`/v1/responses/input_tokens` 与 `/v1/responses/compact` 是给 coding agent 的令牌计数/压缩服务的，与 opencode 用法直接相关——**保留**。

### R6. OTel 遥测（决策点：是否真有 Collector 在收）

- **现状**：`platform/telemetry/`（395）+ `asyncmetrics/`（330）+ `db/telemetry.go` + 管理入口中间件 + `runtime_tracing.go` ≈ ~1k LOC，薄但撒得广；**go.mod 16 个直接依赖里 9 个是 OTel 模块**，且 gRPC/HTTP 两套 OTLP 导出器并存。
- **原因**：面向 Prometheus/Grafana/Tempo 运维栈的路径，对单人部署价值存疑。产品内的统计（`/api/stats/*`）完全不依赖它。
- **怎么做**：如果你没有在跑 Collector/Alloy → 整条移除（telemetry 启动配置段一起删）。如果偶尔用 → 至少删掉不用的那套 OTLP 传输（gRPC 或 HTTP 二选一），直接依赖数可从 16 降到 ~9。

### R7. 实时 WebSocket（决策点：与告警增强 E1 绑定，二选一）

- **现状**：R7 已选择方案 A，实时推送路由、独立 DB 连接 lane、后端 auth 接线、Nginx 推送规则、前端推送客户端与订阅 hook 已退役。仪表盘使用等价的 REST 拉取。
- **原因**：日均 4 个请求的场景，30 秒轮询与推送不可区分。但它也是 E1（故障转移告警）现成的推送通道。
- **怎么做**：一次性决策，不要维持现状——
  - **方案 A（推荐，更懒）**：退役 websocket，仪表盘改轮询；E1 的"人不在页面上也要知道"用 webhook 外推解决（见 E1），页面内横幅靠轮询。
  - **方案 B**：保留 websocket 并让 E1 复用它推 incident 事件，但接受这 ~3k LOC 与专用 DB lane 的长期成本。

### R8. 路由拓扑图（决策点：如果你不看它，删）

- **现状**：`pages/dashboard/routing-diagram/` ~15 文件（两个 ~550 LOC 的布局文件），是 `@xyflow/react` 依赖的唯一消费者。
- **原因**：6 模型 × 3 端点的拓扑装在脑子里就够了；图下方那份"模型/终端目标/端点 + 24h 成功数"列表信息量相同。
- **怎么做**：删路由标签页与 `@xyflow/react`，把健康列表保留成普通表格（它已经存在，几乎零工作量）。

### R9. 小件打包删

| 项 | 位置 | 处理 |
|---|---|---|
| 连接健康探测 | `management/connections/health.go` + 1 路由 | 删（探测结果不进任何决策路径） |
| `@dnd-kit/*` ×3 | 端点排序拖拽 | 换上/下移按钮，删 3 个依赖 |
| 11 条 legacy 重定向路由 | `appRouter.tsx:320-357`、`rewriteRoutes.ts` | 直接删（仓库约定明言"无用户不留兼容层"，root AGENTS.md:108） |
| `SMOKE_TEST_PLAN.md`、`TEST_CASE_GENERATION_METHODOLOGY.md` | docs/ | 归档或删（agent 过程文档，非产品参考） |

### 明确**不建议**删的（调研中排除的嫌疑对象）

- **审计日志**：线上开着 OpenAI 正文捕获，是排查 agent 请求的手段，属于核心观察链一环。
- **UA 客户端规则（configrules）**：请求日志页"客户端"列/筛选的数据来源（`client_rule_id` 正则匹配 `caller_user_agent`，`request_logs.go:373-376`）——正是"哪个 coding agent 发的"这个日常问题的答案。
- **价格模板 + FX 映射 + 报告货币**：费用估算的数据底座，10 个模板在真实维护中。
- **Ban Policy 负载均衡**：产品核心价值本身。
- **`/v1/responses/input_tokens`、`/v1/responses/compact`**：为 coding agent 的计数/压缩工作流而存在。

## 3. 建议增强的功能（核心闭环，按价值排序）

### E1. 故障转移可见性 + 告警（对"高可用网关"定位最大的缺口）

- **现状**：`loadbalance_events` 完整记录了 retry_scheduled / retry_exhausted / banned / unbanned / recovered / admission_rejected 及策略快照（`000001_initial_schema.sql:321-349`），但读取接口只有**按模型**的视图（`management/loadbalance/observability.go:20,60-64`）；仪表盘拓扑只带健康状态/近期成功率（`domain/stats/dashboard_topology_graph.go:37-45`）；**没有任何通知路径**。今天"某个上游挂了、流量切走了"只能靠事后翻日志发现。
- **怎么做**：
  1. 新增一个跨模型的 incidents 读取端点：当前活跃 ban + 最近 N 条 loadbalance 事件（表和数据已齐，只缺一个不按模型过滤的查询）。
  2. 仪表盘顶部放"事件横幅/事件卡"：有活跃 ban 或 24h 内 exhausted/banned 事件时高亮。
  3. **webhook 外推**：ban/unban/recovered 时 POST 到用户配置的 URL（Bark/Telegram/飞书皆可接）。实现放在现有的 side-effect outbox 工人上（禁止请求路径内联，遵守既有约定），配置进启动 JSON 一个 `alerting.webhookUrl` 字段即可，不要做成通知渠道框架。

### E2. 未定价请求下钻（直接解释线上那"4 个未定价"）

- **现状**：行数据已带 priced/unpriced 与原因字段（`domain/stats/request_logs.go:193-194`），聚合侧已有 `UnpricedBreakdown`（`rollups.go:549-555`），原因枚举齐全：`PRICING_DISABLED`（连接没挂价格模板，`runtime_pricing.go:45-47`）、`MISSING_TOKEN_USAGE`、`STREAM_USAGE_UNAVAILABLE`（流被中断时常见——coding agent 恰好爱中断流，`runtime_pricing.go:127-134`）、`MISSING_PRICE_DATA`（含缺 FX，`runtime_pricing.go:136-153`）。**但请求日志列表的查询参数解析不支持按计价状态过滤**（`management/stats/service.go:536-566`），仪表盘的"N 个未定价"也不可点击。
- **怎么做**：参数解析器加 `priced=true|false`（可选 `unpriced_reason=`）；前端把"4 个未定价请求"做成带过滤的跳转链接；支出 KPI 旁展示已有的 UnpricedBreakdown。全是接线工作，无新数据。

### E3. 价格目录/预设（降低费用估算的维护成本与覆盖缺口）

- **现状**：没有内置价格目录——每个模型×连接都要手工建 `pricing_templates` 行（仅 PER_1M 模式，`000001_initial_schema.sql:780-795`）再挂到连接上。10 个模板都是手敲的；新模型上线 = 又一轮手工录入；漏挂即产生未定价请求。
- **怎么做**：
  1. 最低成本：支持**JSON 价格表导入**（一个管理端点 + 一段校验），你可以维护一份自己的价格清单文件，或直接吃社区维护的价格数据（如 litellm 的 model_prices JSON）做一次映射。
  2. 配套：在模型/连接列表给"未挂价格模板的连接"一个显式警示徽标。
  3. 不建议做在线自动拉取官方价格的定时任务——你的上游是转售商，价格本来就非官方。

### E4. 请求日志页体验补全（你的第一页面，papercut 逐个清）

- **修 bug**：`to_time` 出现在参数结构与 WHERE 构建里（`request_logs.go:365-368`）但**从未被查询解析器读取**（`service.go:536-566`）——一个死过滤器，接上或删掉。
- **默认时间范围**：默认"最近 1 小时"在低流量下打开即空。改默认 24h，或记住上次选择（localStorage 一行事）。分析页同理。
- **过滤补全**：状态族目前只有 4xx/5xx，补 2xx/成功、精确状态码、错误文本搜索。
- **导出**：当前列表查询结果加 CSV/JSON 导出（分析页已有"导出快照 JSON"先例）。
- **实时尾随**：R7 已选方案 A，因此页面内实时 tail 不做；需要离页通知时走 E1 webhook-via-outbox。

### E5. 延迟趋势图

- **现状**：p95 只有时点值、且是 Go 侧现算的（`rollups.go:145-147,212-214`）；`response_time_ms`/`ttft_ms` 列早已在 `request_logs` 上。
- **怎么做**：分析页加一张 p50/p95 随时间曲线（复用现有 recharts 与分桶查询模式）。对"今天上游是不是变慢了"这个日常问题，比平均值有用得多。

## 4. 技术债清理清单（按优先级）

### T1. context-routing/overflow 硬删除残留（最大簇，纯清扫）

- 空壳 stub `setRuntimeHarnessConnectionContextCapabilities` / `enableRuntimeHarnessFacadeModel`（`backend/tests/runtime/proxy_selector_test.go:1097`）被 ~30 处调用；5 个测试名还叫 `*AfterContextRoutingRemoval`。
- 383 行的"证明不存在"测试文件 `backend/tests/contract/unified_removed_concepts_test.go`——违反仓库自己的规则（root AGENTS.md:109"普通删除验证用人工确认，不留 proves-not 测试"），加上散落各处的 absence 断言（`migrations_test.go:61,86,89,668` 等）。
- 自我抵消的迁移对 000002/000007（加了又删）——无用户，可直接 squash 进 000001。
- `frontend/package.json` 的 `test:e2e` 包了一段 node 脚本，专门为一个**已删除的测试家族**（`context-capability-authoring`）改写 `--grep` 参数——整段换回裸 `playwright test`。

### T2. 双路由器统一

- `react-router-dom` 与 `@tanstack/react-router` **同时挂载**（`App.tsx:13-15`）；TanStack 已拥有完整路由树 + zod search schema（`appRouter.tsx:236-397`），react-router 残存于 ~23 个文件（`useNavigate`×7、`Link`×5、`useSearchParams`×3、`useLocation`×3、`CompatNavigate` 及嵌套 `Routes`）。
- 机械迁移量 1–2 天，删依赖后每次页面加载少 ~40kB min+gz；先做 T3 会更顺（legacy 路由删除同时移除 `matchPath`/`Navigate` 用法）。

### T3. 11 条 legacy 重定向路由删除

- `appRouter.tsx:320-357` + `rewriteRoutes.ts:94-106,132-144` + `getLegacyRedirectPath` + `src/test/route-helpers.test.ts` 对应用例。无用户 = 无人依赖旧 URL。

### T4. 测试地基修整

- `frontend/tests/lib/` 23 个文件中 **9 个没接进 `test:lib`**（已核实清单：`analytics_websocket_contract`、`costing_reporting_currency_contract`、`log_retention_api_contract`、`management_contract`、`pricing-template-form-state-normalization`、`profile_selection_contract`、`reporting_currency_contract`、`request_log_audit_state_contract`、`request_log_filter_state_contract`）——逐个决定接入或删除，并把 `test:lib` 从手写文件枚举改成 glob。
- Playwright e2e ~17.5k LOC 不在 CI：按 §2 的删减自动缩水后，把存活的核心流（请求日志、模型配置）挑 3–5 条进 CI，其余删；顺带清掉 plan 编号命名（`task-6/8/9/10/11/17-*.spec.ts`）。
- 后端 ~49% 测试占比集中在将被冻结/删除的功能上（profile_scope 3,571 行、auth 控制面 2,000 行），随功能一起退役。

### T5. 统计读写不一致（核心链路上的隐性债）

- 三处**读时修补**写入端不一致的"支出一致性"归一化逻辑（`domain/stats/request_logs.go:716-733`、`snapshot.go:643-685`）——修正写入端，删掉补丁。
- `management_stat_buckets` 预聚合表建了但**服务路径没用它**：现状是把命中的 `usage_request_events` 全量载入内存、Go 里聚合（`rollups.go:898-948`、`GetSpending`:511、`GetStatsSummary`:105）。要么接线要么删表。日均 4 请求无感，流量上来是悬崖。
- 请求日志列表每次都全量重载端点/模型/UA 规则并跑 `COUNT(*)`（`request_logs.go:123-158`）——同上，先记账不动，`// ponytail: 全量 COUNT，日志量上万后换估算或 keyset 分页`。

### T6. 命名与小件

- `backend/internal/targetcompat/glossary.go`（29 行）：`connection`↔`terminal_target` 改名改了一半，选定一个词收尾删掉。
- `backend/internal/providercompat/`（~540 LOC）：是**在用的活逻辑**（`ResolveAuthProfile`，`runtime/planning_snapshot.go:129,195` 调用），名字却像兼容垫片——改名（如 `providerauth`），不要误删。
- God files 顺带拆：`platform/config/bootstrap.go` 3,038、`runtime/observability.go` 2,320、`i18n/messages/en.ts` 3,866（前端最大文件）。不专项立项，谁动谁拆。

### T7. 文档修正

- root `AGENTS.md:52,75,90` 与 `docs/AGENTS.md` 多处引用废弃的计划/证据目录——**该目录不存在**，应改写为 `docs/` 下的计划/文档口径并清理旧引用。
- `ARCHITECTURE.md` 的 ASCII 架构图已错位，修复或删图留文字。
- 其余文档新鲜度尚可（`API_SPEC.md` 2,817 行随 HEAD 更新）。§2 删除项落地时同步删对应章节即可。

### T8. i18n 策略确认

- ~6.1k LOC（前端源码 ~11%），每个文案 = en/zh-CN 两处修改；`en.ts` 同时是 `Messages` 类型源（`zh-CN.ts:1`）。你日常用中文界面——若英文语言包并非发布需求，可反转结构（zh-CN 为类型源、删 en）省一半维护；若打算开源给英文用户则保持现状。页面删减本身会自动缩小它，不必专项攻坚。

## 5. 建议的落地顺序

| 阶段 | 内容 | 预估规模 |
|---|---|---|
| **P0 快赢**（半天） | T1 的 test:e2e shim、stub 清理、absence 测试删除；T3 legacy 路由；T7 废弃目录引用；E4 的 `to_time` 接线/删除；T4 的 test:lib glob 化 | 全是删除与一行修补 |
| **P1 大减法**（1–2 周，按 R1→R2→R3→R4 顺序，每项独立成 PR） | configbundle → 启动标签页+bootstrapconfig → 认证链瘦身 → profiles 冻结；期间拍板 R5–R8 四个决策点并执行 | 净删 ~15k–20k LOC |
| **P2 核心增强**（与 P1 可并行，1–2 周） | E1 故障转移可见性+webhook → E2 未定价下钻 → E4 日志页体验 → E3 价格导入 → E5 延迟趋势 | 每项 ≤ 数百 LOC 新增 |
| **P3 结构债**（穿插进行） | T2 双路由器统一 → T5 统计读写修正 → T6 命名收尾 | 1 周内 |

先减后增的理由：每一项删除都直接降低 P2 增强和 P3 重构要触碰的面积（例如删 configbundle 后，模型/端点契约变更不再需要同步维护 bundle 图校验与其测试）。

## 6. 需要 owner 拍板的决策点

1. **OTel**：现在有没有 Collector/Alloy 在收 Prism 的指标？没有 → R6 整删；有 → 只删一套 OTLP 传输。
2. **实时推送**：R7 方案 A（退役 websocket，告警走 webhook + 轮询）还是方案 B（保留并复用为告警通道）？——推荐 A。
3. **路由拓扑图**：`/observe?tab=routing` 你日常看吗？不看 → R8 删图留表。
4. **操作员登录**：反代兜底后是否彻底移除用户名密码登录（R3 第二步）？
5. **图片路由**：R5 已执行；后续如需图片能力按新需求重新设计。
6. **英文语言包**：是否为开源发布需求（决定 T8 方向）。
