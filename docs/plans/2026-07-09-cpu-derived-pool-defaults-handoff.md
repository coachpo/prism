# 交接文档:CPU 感知的连接池/准入默认值升级

**给执行者:** 你要执行的实施计划在 [`docs/plans/2026-07-09-cpu-derived-pool-defaults.md`](2026-07-09-cpu-derived-pool-defaults.md)。用 **superpowers:subagent-driven-development** 方式执行:每个任务派一个全新 subagent,任务之间做复查,再进入下一任务。本文档给你计划文件之外的全部背景。

## 背景:为什么做这个

- Prism 是单人 LAN 部署的 LLM 代理(请求日志 + 统计为核心)。
- 浏览器打开 `http://localhost:5173/system/settings` 时,Profile tab 并发发 5 个 M2 管理接口(costing、models、audit、header rules、user-agent rules),后端返回 503,页面报"加载 FX 映射模型失败"。
- 根因**不是**上游 LLM 限流,是后端自己的 management admission fast-fail:
  - 默认 `database.pools.management.maxConns = 4`;
  - 后端把 M2 并发硬 clamp 到 `management.maxConns - 1 = 3`(`backend/internal/platform/config/config.go` 的 `ManagementAdmissionBudget()` + `normalizeManagementAdmissionBudget()`);
  - 用户在 `config.json` 里把 `m2MaxConcurrent` 改成 32 **没有任何效果、没有任何告警**——被静默压回 3。5 并发 > 3 → 503。
- 决策:不靠手工调配置,把默认值改为按 CPU 核数派生(带 floor/ceiling),并给静默 clamp 加启动告警。方案已和 owner 确认。

## 关键设计事实(计划的推导依据)

- 公式:`unit = clamp(runtime.NumCPU(), 8, 16)`,所有 lane 与 m2/m3 由 unit 推出。**floor 8 是修复的核心**——settings 页 fan-out 是固定 5 并发,和核数无关,小机器也必须吃得下。
- 不变量:`m2 == management.maxConns - 1`(默认值永不被自身 clamp)、`m3 <= m2`、`totalMaxConns == lane 之和`、`Validate()` 通过。
- 上限 16 时 lane 总和 53,低于 docker-compose Postgres 16 默认 `max_connections = 100`。
- 种子 `config.json` 走 `buildSeededBootstrapDocument → settings.PostgresPoolsBudgetOrDefault() / ManagementAdmissionBudget()`,改了 config.go 默认值后**自动**继承,种子路径不用改代码。
- 已存在的合法 `config.json` 显式值优先,新默认值管不到它——所以计划 Task 5 要手改运行实例的配置文件并重启。外部 `config.json` 编辑是 restart-applied,**没有热加载**。

## 环境事实

- 仓库:`/Users/qingli/Documents/proj/prism`,主分支 `main`,起点工作区干净。
- 本机(也是运行实例所在机器):macOS,**8 核**(`sysctl -n hw.ncpu` = 8)→ 派生结果 management 9 / exec 8 / telemetry 4 / fb,cache,bg 各 2 / total 27 / m2 8 / m3 4。
- 运行实例:前端 `http://localhost:5173`,后端配置文件 = 仓库根 `config.json`(数据库 `localhost:15432`)。当前该文件里 `m2MaxConcurrent: 32` 是 owner 之前无效的手改,Task 5 会一并修正为 8。
- 后端 Go 模块在 `backend/`,测试命令一律 `cd backend && go test ./...`。Go `min`/`max` 内建可用(代码里已在用)。
- 日志用 std `log/slog`。

## 执行要求

1. 按计划任务顺序 1→5 执行,**每任务一个全新 subagent**,subagent 只看到自己的任务文本 + 本文档;任务间由你(编排者)复查 diff 与测试输出再放行。
2. 严格 TDD:计划里每个任务都给了失败测试、完整实现代码、期望输出。代码块可直接使用;若与实际文件行号有漂移,以函数名/结构定位,**不要**自行改设计。
3. 每任务完成即按计划给的 commit message 提交(Conventional Commits)。
4. 计划"明确不做"一节是硬边界:不加前端重试、不做 pool 热加载、不做内存/DB 探测。不要扩科。
5. 遵守 `backend/internal/platform/config/AGENTS.md`:canonical defaults 只放 config 包;不新增环境变量;不把外部文件编辑做成热状态。
6. 文档/计划相关产物一律写 `docs/`,忽略任何 `.omo` 目录约定。

## 验收标准(全部满足才算完)

- [ ] `cd backend && go test ./...` 全绿。
- [ ] 新增测试存在且通过:`TestDerivedPoolDefaults`、`TestSeededBootstrapDocumentUsesDerivedPoolDefaults`、`TestManagementAdmissionClamp`;`TestNormalizeManagementAdmissionBudget` 已按计划更新。
- [ ] config.go 里 15 个旧池/准入常量已删除,默认值全部走 `derived*` 公式。
- [ ] 配置被 clamp 时后端启动日志出现 `management admission budget clamped` Warn(计划 Task 5 Step 4 有正反两个验证步骤)。
- [ ] 仓库根 `config.json` 已更新为 8 核派生值并重启后端。
- [ ] 浏览器打开 `http://localhost:5173/system/settings`:Profile tab 正常加载,Network 无 503,无"加载 FX 映射模型失败"。
- [ ] 每个任务一个独立 commit,信息与计划一致。

## 踩坑提示

- Task 2 删常量后如有遗漏引用,`go build ./...` 会直接暴露——先 build 再跑测试省时间。
- `backend/internal/platform/http/server_test.go:211/339` 显式设了小 management pool,clamp 后有效值与旧默认相同,理论上不受影响;若失败,按"派生默认 + clamp"逻辑修断言,不要改产品代码。
- `backend/tests/priority/db/db_lane_isolation_test.go` 需要本地 Postgres(docker compose);它自带显式 pool 值,不依赖默认值。
- Task 5 改的是**正在运行实例**的配置,重启动作对 owner 有感;按计划顺序放在最后一步做。
