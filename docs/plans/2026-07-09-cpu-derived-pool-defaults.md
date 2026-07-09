# CPU 感知的连接池/准入默认值 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Postgres 连接池与 management admission 的默认值从写死的小常量改为按 CPU 核数派生(带下限/上限),使 `/system/settings` 页面的并发管理请求在任何机器上开箱即用,不再需要手工调 `config.json`;同时对"配置被静默 clamp"增加启动告警。

**Architecture:** 所有默认值集中在 `backend/internal/platform/config/config.go`(见该目录 AGENTS.md 约定"canonical defaults 留在此包")。新增纯函数 `derivedPoolUnit(cores)` / `derivedPostgresPoolsBudget(cores)` / `derivedManagementAdmissionBudget(cores)`,`runtime.NumCPU()` 只在默认值入口注入一次,测试用固定核数保证确定性。种子 `config.json` 走 `buildSeededBootstrapDocument → settings.PostgresPoolsBudgetOrDefault()/ManagementAdmissionBudget()`,自动继承新默认值,种子路径零改动。已存在的合法 `config.json` 显式值优先、不受影响(现有约定)。

**Tech Stack:** Go(std 只用 `runtime`、`log/slog`,均已在用),`go test`。

## Global Constraints

- 默认值只放在 `backend/internal/platform/config/config.go`,不新增环境变量(AGENTS.md:"avoid adding env knobs")。
- 外部 `config.json` 编辑仍是 restart-applied,不做热加载(AGENTS.md 反模式)。
- 公式必须满足不变量:`m2 == management.maxConns - 1`(否则默认值自己就会被 clamp)、`m3 <= m2`、`SumMaxConns() == TotalMaxConns`、`Validate()` 通过。
- 下限:任何核数下 `m2 >= 8`(settings 页 Profile tab 并发打 5 个 M2 接口,见 `frontend/src/pages/settings/costing/useCostingSettingsBootstrap.ts:72` 与 `useAuditConfigurationData.ts:195`,留余量)。
- 上限:lane 总和 ≤ 53,远低于 docker-compose Postgres 16 默认 `max_connections=100`。
- Go `min`/`max` 内建可用(config.go:377 已在用)。
- 提交信息用 Conventional Commits。

## 派生公式(唯一事实来源)

```
unit = clamp(NumCPU, 8, 16)

pools:
  management       maxConns = unit + 1   minIdle = 1
  runtimeExecution maxConns = unit       minIdle = 2
  runtimeTelemetry maxConns = unit / 2   minIdle = 1
  runtimeFeedback  maxConns = unit / 4   minIdle = 0
  cacheRefresh     maxConns = unit / 4   minIdle = 0
  backgroundJobs   maxConns = unit / 4   minIdle = 0
  totalMaxConns    = 各 lane 之和

managementAdmission:
  m2 = unit
  m3 = unit / 2
```

| cores | unit | management | exec | telemetry | fb/cache/bg | total | m2/m3 |
|------|------|-----------|------|-----------|-------------|-------|-------|
| ≤8   | 8    | 9         | 8    | 4         | 2/2/2       | 27    | 8/4   |
| 12   | 12   | 13        | 12   | 6         | 3/3/3       | 40    | 12/6  |
| ≥16  | 16   | 17        | 16   | 8         | 4/4/4       | 53    | 16/8  |

8 核(本机)结果与研究员手工建议值一致(management 9、m2 8、m3 4、total 27)。

---

### Task 1: 派生公式函数 + 公式测试

**Files:**
- Modify: `backend/internal/platform/config/config.go`(在 `DefaultPostgresPoolsBudget` 附近新增函数;imports 加 `"runtime"`)
- Test: `backend/internal/platform/config/config_test.go`

**Interfaces:**
- Produces:
  - `func derivedPoolUnit(cores int) int32`
  - `func derivedPostgresPoolsBudget(cores int) PostgresPoolsBudget`
  - `func derivedManagementAdmissionBudget(cores int) ManagementAdmissionBudget`
  - 本任务只新增函数,不改任何现有默认值行为。

- [ ] **Step 1: 写失败测试**

在 `config_test.go` 末尾追加:

```go
func TestDerivedPoolDefaults(t *testing.T) {
	for _, tc := range []struct {
		cores int
		unit  int32
	}{
		{cores: 1, unit: 8},
		{cores: 8, unit: 8},
		{cores: 12, unit: 12},
		{cores: 32, unit: 16},
	} {
		budget := derivedPostgresPoolsBudget(tc.cores)
		admission := derivedManagementAdmissionBudget(tc.cores)
		if budget.Management.MaxConns != tc.unit+1 || budget.Management.MinIdleConns != 1 {
			t.Fatalf("cores=%d unexpected management budget: %+v", tc.cores, budget.Management)
		}
		if budget.RuntimeExecution.MaxConns != tc.unit || budget.RuntimeExecution.MinIdleConns != 2 {
			t.Fatalf("cores=%d unexpected runtime execution budget: %+v", tc.cores, budget.RuntimeExecution)
		}
		if budget.RuntimeTelemetry.MaxConns != tc.unit/2 {
			t.Fatalf("cores=%d unexpected runtime telemetry budget: %+v", tc.cores, budget.RuntimeTelemetry)
		}
		for lane, got := range map[string]DatabasePoolBudget{
			"runtimeFeedback": budget.RuntimeFeedback,
			"cacheRefresh":    budget.CacheRefresh,
			"backgroundJobs":  budget.BackgroundJobs,
		} {
			if got.MaxConns != tc.unit/4 || got.MinIdleConns != 0 {
				t.Fatalf("cores=%d unexpected %s budget: %+v", tc.cores, lane, got)
			}
		}
		if int64(budget.TotalMaxConns) != budget.SumMaxConns() {
			t.Fatalf("cores=%d total %d != lane sum %d", tc.cores, budget.TotalMaxConns, budget.SumMaxConns())
		}
		if err := budget.Validate(); err != nil {
			t.Fatalf("cores=%d derived budget must validate: %v", tc.cores, err)
		}
		if admission.M2MaxConcurrent != int64(tc.unit) || admission.M3MaxConcurrent != int64(tc.unit/2) {
			t.Fatalf("cores=%d unexpected derived admission: %+v", tc.cores, admission)
		}
		// m2 必须正好占满 management lane 减 1(留 M1 位),否则默认值会被自身 clamp。
		if admission.M2MaxConcurrent != int64(budget.Management.MaxConns-1) {
			t.Fatalf("cores=%d m2=%d must equal management.maxConns-1=%d", tc.cores, admission.M2MaxConcurrent, budget.Management.MaxConns-1)
		}
	}
	// 下限必须覆盖 settings 页 5 个并发 M2 请求。
	if got := derivedManagementAdmissionBudget(1).M2MaxConcurrent; got < 6 {
		t.Fatalf("floor m2=%d cannot admit the settings page fan-out of 5 concurrent M2 calls", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/config/ -run TestDerivedPoolDefaults -v`
Expected: FAIL(compile error,`undefined: derivedPostgresPoolsBudget`)

- [ ] **Step 3: 最小实现**

在 `config.go` 的 `DefaultPostgresPoolsBudget()`(当前 309 行)之前插入,并在 import 块加 `"runtime"`:

```go
// derivedPoolUnit maps host CPU count to the sizing unit for pool and
// admission defaults. Floor 8 keeps the /system/settings page fan-out
// (5 concurrent M2 requests) admitted on small hosts; ceiling 16 keeps
// the lane sum (53) well under the postgres default max_connections=100.
func derivedPoolUnit(cores int) int32 {
	return int32(min(max(cores, 8), 16))
}

func derivedPostgresPoolsBudget(cores int) PostgresPoolsBudget {
	unit := derivedPoolUnit(cores)
	budget := PostgresPoolsBudget{
		Management:       DatabasePoolBudget{MaxConns: unit + 1, MinIdleConns: 1},
		RuntimeExecution: DatabasePoolBudget{MaxConns: unit, MinIdleConns: 2},
		RuntimeTelemetry: DatabasePoolBudget{MaxConns: unit / 2, MinIdleConns: 1},
		RuntimeFeedback:  DatabasePoolBudget{MaxConns: unit / 4, MinIdleConns: 0},
		CacheRefresh:     DatabasePoolBudget{MaxConns: unit / 4, MinIdleConns: 0},
		BackgroundJobs:   DatabasePoolBudget{MaxConns: unit / 4, MinIdleConns: 0},
	}
	budget.TotalMaxConns = int32(budget.SumMaxConns())
	return budget
}

// derivedManagementAdmissionBudget keeps m2 == management.maxConns-1 so the
// derived defaults are never clamped by normalizeManagementAdmissionBudget.
func derivedManagementAdmissionBudget(cores int) ManagementAdmissionBudget {
	unit := derivedPoolUnit(cores)
	return ManagementAdmissionBudget{M2MaxConcurrent: int64(unit), M3MaxConcurrent: int64(unit / 2)}
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/config/ -run TestDerivedPoolDefaults -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/qingli/Documents/proj/prism
git add backend/internal/platform/config/config.go backend/internal/platform/config/config_test.go
git commit -m "feat(config): add cpu-derived pool and admission default formula"
```

---

### Task 2: 把 canonical 默认值切到派生公式,删除旧常量

**Files:**
- Modify: `backend/internal/platform/config/config.go`
- Modify: `backend/internal/platform/config/config_test.go`(三处旧数字断言)

**Interfaces:**
- Consumes: Task 1 的三个 derived 函数。
- Produces:
  - `DefaultPostgresPoolsBudget()` 返回 `derivedPostgresPoolsBudget(runtime.NumCPU())`
  - 新增 `func defaultManagementAdmissionBudget() ManagementAdmissionBudget`(返回 `derivedManagementAdmissionBudget(runtime.NumCPU())`),`Load()` 与 `ManagementAdmissionBudget()` 改用它
  - 删除常量:`defaultPostgresTotalMaxConns`、`defaultManagementDatabaseMaxConns`、`defaultManagementDatabaseMinIdleConns`、`defaultRuntimeExecutionDatabaseMaxConns`、`defaultRuntimeExecutionDatabaseMinIdleConns`、`defaultRuntimeTelemetryDatabaseMaxConns`、`defaultRuntimeTelemetryDatabaseMinIdleConns`、`defaultRuntimeFeedbackDatabaseMaxConns`、`defaultRuntimeFeedbackDatabaseMinIdleConns`、`defaultCacheRefreshDatabaseMaxConns`、`defaultCacheRefreshDatabaseMinIdleConns`、`defaultBackgroundJobsDatabaseMaxConns`、`defaultBackgroundJobsDatabaseMinIdleConns`、`defaultManagementM2MaxConcurrent`、`defaultManagementM3MaxConcurrent`(config.go:85-99)

- [ ] **Step 1: 改实现**

config.go 中:

1. 删除 85-99 行的 15 个常量(`defaultRuntimeTransport*` 等其余常量保留)。
2. 重写三个默认值函数(替换当前 309-327 行):

```go
func DefaultPostgresPoolsBudget() PostgresPoolsBudget {
	return derivedPostgresPoolsBudget(runtime.NumCPU())
}

func defaultManagementDatabasePoolBudget() DatabasePoolBudget {
	return DefaultPostgresPoolsBudget().Management
}

func defaultRuntimeExecutionDatabasePoolBudget() DatabasePoolBudget {
	return DefaultPostgresPoolsBudget().RuntimeExecution
}

func defaultManagementAdmissionBudget() ManagementAdmissionBudget {
	return derivedManagementAdmissionBudget(runtime.NumCPU())
}
```

3. `loadCanonicalDefaultSettings`(当前 274 行)改为:

```go
		ManagementAdmissionControlBudget: defaultManagementAdmissionBudget(),
```

4. `ManagementAdmissionBudget()`(当前 375-379 行)改为:

```go
func (s Settings) ManagementAdmissionBudget() ManagementAdmissionBudget {
	maxLowerPriority := max(int64(s.ManagementDatabaseBudget().MaxConns)-1, int64(1))
	return normalizeManagementAdmissionBudget(s.ManagementAdmissionControlBudget, defaultManagementAdmissionBudget(), maxLowerPriority)
}
```

- [ ] **Step 2: 更新三处旧数字断言**

config_test.go:

1. 第 35-51 行(canonical defaults 测试)替换为:

```go
	assertPostgresPoolsBudget(t, settings.PostgresPoolsBudgetOrDefault(), derivedPostgresPoolsBudget(runtime.NumCPU()))
	wantAdmission := derivedManagementAdmissionBudget(runtime.NumCPU())
	if got := settings.ManagementAdmissionControlBudget; got != wantAdmission {
		t.Fatalf("unexpected raw management admission defaults: %+v", got)
	}
	admission := settings.ManagementAdmissionBudget()
	if admission != wantAdmission {
		t.Fatalf("unexpected normalized management admission defaults: %+v", admission)
	}
	if reservedM1 := int64(settings.ManagementDatabaseBudget().MaxConns) - admission.M2MaxConcurrent; reservedM1 != 1 {
		t.Fatalf("expected management lane to leave M1 reservation of 1, got %d", reservedM1)
	}
```

同时在 config_test.go 的 import 块加 `"runtime"`。

2. 第 147 行(stale realtime lane 测试)改为:

```go
	if got := settings.PostgresPoolsBudgetOrDefault().SumMaxConns(); got != DefaultPostgresPoolsBudget().SumMaxConns() {
		t.Fatalf("expected stale realtime pool to stay out of active budget, got sum=%d", got)
	}
```

3. `TestNormalizeManagementAdmissionBudget`(第 203-224 行)整体替换为:

```go
func TestNormalizeManagementAdmissionBudget(t *testing.T) {
	defaults := defaultManagementAdmissionBudget()
	got := normalizeManagementAdmissionBudget(ManagementAdmissionBudget{}, defaults, 3)
	if got != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 3}) {
		t.Fatalf("unexpected normalized empty management admission budget: %+v", got)
	}

	settings := loadCanonicalDefaultSettings("")
	admission := settings.ManagementAdmissionBudget()
	if admission != defaults {
		t.Fatalf("expected canonical admission budget to avoid clamp drift, got %+v", admission)
	}
	if reservedM1 := int64(settings.ManagementDatabaseBudget().MaxConns) - admission.M2MaxConcurrent; reservedM1 != 1 {
		t.Fatalf("expected normalized admission to leave one M1 slot, got %d", reservedM1)
	}

	clamped := normalizeManagementAdmissionBudget(ManagementAdmissionBudget{M2MaxConcurrent: 9, M3MaxConcurrent: 7}, defaults, 3)
	if clamped != (ManagementAdmissionBudget{M2MaxConcurrent: 3, M3MaxConcurrent: 3}) {
		t.Fatalf("unexpected high-budget clamp result: %+v", clamped)
	}
}
```

(第一处期望从 `{3,2}` 变 `{3,3}`:派生默认 m3 是 4,被 clamp 到 m2=3。)

- [ ] **Step 3: 跑 config 包全部测试**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/config/`
Expected: ok(全部 PASS)

- [ ] **Step 4: 跑受影响的相邻包**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/http/ ./internal/platform/startup/...`
Expected: ok。`server_test.go:211/339` 显式设了 `ManagementDatabasePoolBudget{MaxConns:3/4}`,clamp 后有效 m2 与旧默认相同(2/3),不应失败;若失败按同样"派生默认+clamp"逻辑修断言。

- [ ] **Step 5: Commit**

```bash
cd /Users/qingli/Documents/proj/prism
git add backend/internal/platform/config/config.go backend/internal/platform/config/config_test.go
git commit -m "feat(config): derive pool and admission defaults from cpu count"
```

---

### Task 3: 种子 config.json 回归测试

**Files:**
- Test: `backend/internal/platform/config/config_test.go`

**Interfaces:**
- Consumes: `buildSeededBootstrapDocument`(bootstrap.go:2007)、Task 2 的默认值函数。
- Produces: 回归网——种子文档必须写入派生值(防止未来有人把种子路径改回写死数字)。

- [ ] **Step 1: 写测试**

```go
func TestSeededBootstrapDocumentUsesDerivedPoolDefaults(t *testing.T) {
	document, err := buildSeededBootstrapDocument(Load(), time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("build seeded bootstrap document: %v", err)
	}
	wantPools := DefaultPostgresPoolsBudget()
	pools := document.Database.Pools
	if *pools.TotalMaxConns != int(wantPools.TotalMaxConns) {
		t.Fatalf("seeded totalMaxConns=%d want %d", *pools.TotalMaxConns, wantPools.TotalMaxConns)
	}
	if *pools.Management.MaxConns != int(wantPools.Management.MaxConns) {
		t.Fatalf("seeded management maxConns=%d want %d", *pools.Management.MaxConns, wantPools.Management.MaxConns)
	}
	wantAdmission := defaultManagementAdmissionBudget()
	admission := document.Database.ManagementAdmission
	if *admission.M2MaxConcurrent != int(wantAdmission.M2MaxConcurrent) || *admission.M3MaxConcurrent != int(wantAdmission.M3MaxConcurrent) {
		t.Fatalf("seeded admission m2=%d m3=%d want %+v", *admission.M2MaxConcurrent, *admission.M3MaxConcurrent, wantAdmission)
	}
}
```

(种子路径本来就走 `PostgresPoolsBudgetOrDefault()`,此测试应直接 PASS——这是回归测试,不是驱动实现的红灯。字段名以 bootstrap.go:2038-2052 的 `bootstrapDatabasePools`/`bootstrapManagementAdmission` 为准。)

- [ ] **Step 2: 跑测试**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/config/ -run TestSeededBootstrapDocumentUsesDerivedPoolDefaults -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/qingli/Documents/proj/prism
git add backend/internal/platform/config/config_test.go
git commit -m "test(config): pin seeded config.json to derived pool defaults"
```

---

### Task 4: 静默 clamp 改为启动告警

用户这次踩的坑:`m2MaxConcurrent: 32` 被静默压到 3,毫无提示。给它加一条启动日志。

**Files:**
- Modify: `backend/internal/platform/config/config.go`
- Modify: `backend/internal/platform/http/admission.go`(`newHTTPAdmissionController`,144 行)
- Modify: `backend/internal/platform/http/hot_bootstrap_runtime.go`(`buildHotAdmissionSnapshot`,252 行)
- Test: `backend/internal/platform/config/config_test.go`

**Interfaces:**
- Produces: `func (s Settings) ManagementAdmissionClamp() (configured, effective ManagementAdmissionBudget, clamped bool)`
- Consumes: 无新依赖(http 包已 import `config`;admission.go 需新增 `"log/slog"` import)。

- [ ] **Step 1: 写失败测试**

```go
func TestManagementAdmissionClamp(t *testing.T) {
	settings := loadCanonicalDefaultSettings("")
	settings.PostgresPoolsBudget.Management = DatabasePoolBudget{MaxConns: 4, MinIdleConns: 1}
	settings.ManagementAdmissionControlBudget = ManagementAdmissionBudget{M2MaxConcurrent: 32, M3MaxConcurrent: 32}
	configured, effective, clamped := settings.ManagementAdmissionClamp()
	if !clamped {
		t.Fatal("expected clamp to be reported")
	}
	if configured.M2MaxConcurrent != 32 || effective.M2MaxConcurrent != 3 || effective.M3MaxConcurrent != 3 {
		t.Fatalf("unexpected clamp report: configured=%+v effective=%+v", configured, effective)
	}

	if _, _, clamped := loadCanonicalDefaultSettings("").ManagementAdmissionClamp(); clamped {
		t.Fatal("canonical defaults must not report a clamp")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/config/ -run TestManagementAdmissionClamp -v`
Expected: FAIL(compile error,`ManagementAdmissionClamp` 未定义)

- [ ] **Step 3: 实现**

config.go,加在 `ManagementAdmissionBudget()` 后面:

```go
// ManagementAdmissionClamp reports whether the configured M2/M3 admission
// budget was reduced to fit database.pools.management.maxConns, so callers
// can surface the silent clamp at startup.
func (s Settings) ManagementAdmissionClamp() (configured, effective ManagementAdmissionBudget, clamped bool) {
	configured = s.ManagementAdmissionControlBudget
	effective = s.ManagementAdmissionBudget()
	clamped = (configured.M2MaxConcurrent > 0 && configured.M2MaxConcurrent > effective.M2MaxConcurrent) ||
		(configured.M3MaxConcurrent > 0 && configured.M3MaxConcurrent > effective.M3MaxConcurrent)
	return configured, effective, clamped
}
```

admission.go:在 import 块加 `"log/slog"`,并改 `newHTTPAdmissionController`:

```go
func newHTTPAdmissionController(settings config.Settings) *admission.Controller {
	warnIfManagementAdmissionClamped(settings)
	managementBudget := settings.ManagementAdmissionBudget()
	return admission.NewController(admission.Limits{
		ManagementM1: managementM1AdmissionBudget(settings, managementBudget),
		ManagementM2: managementBudget.M2MaxConcurrent,
		ManagementM3: managementBudget.M3MaxConcurrent,
	})
}

func warnIfManagementAdmissionClamped(settings config.Settings) {
	configured, effective, clamped := settings.ManagementAdmissionClamp()
	if !clamped {
		return
	}
	slog.Warn(
		"management admission budget clamped by database.pools.management.maxConns; raise maxConns or lower m2MaxConcurrent",
		slog.Int64("configured_m2", configured.M2MaxConcurrent),
		slog.Int64("effective_m2", effective.M2MaxConcurrent),
		slog.Int64("configured_m3", configured.M3MaxConcurrent),
		slog.Int64("effective_m3", effective.M3MaxConcurrent),
		slog.Int("management_max_conns", int(settings.ManagementDatabaseBudget().MaxConns)),
	)
}
```

hot_bootstrap_runtime.go `buildHotAdmissionSnapshot`(252 行)第一行加:

```go
	warnIfManagementAdmissionClamped(settings)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./internal/platform/config/ -run TestManagementAdmissionClamp -v && go build ./... && go test ./internal/platform/http/`
Expected: 全 PASS,build 无错。

- [ ] **Step 5: Commit**

```bash
cd /Users/qingli/Documents/proj/prism
git add backend/internal/platform/config/config.go backend/internal/platform/config/config_test.go backend/internal/platform/http/admission.go backend/internal/platform/http/hot_bootstrap_runtime.go
git commit -m "feat(http): warn at startup when admission budget is clamped by pool size"
```

---

### Task 5: 全量测试 + 修复本机运行实例 + 端到端验证

**Files:**
- Modify: `/Users/qingli/Documents/proj/prism/config.json`(运行实例的真实配置,8 核机器)

- [ ] **Step 1: 后端全量测试**

Run: `cd /Users/qingli/Documents/proj/prism/backend && go test ./...`
Expected: 全部 ok。(`tests/priority/db/db_lane_isolation_test.go` 自带显式 pool 值,不受默认值变化影响;若该套件需要本地 Postgres,确保 docker compose 的 postgres 在跑。)

- [ ] **Step 2: 更新运行实例 config.json**

现有文件是旧默认值种出来的(management.maxConns=4 把 m2 压到 3)。把 `database` 段改成派生值(8 核 → unit 8),其余段不动:

```json
"pools": {
  "totalMaxConns": 27,
  "management": { "maxConns": 9, "minIdleConns": 1 },
  "runtimeExecution": { "maxConns": 8, "minIdleConns": 2 },
  "runtimeTelemetry": { "maxConns": 4, "minIdleConns": 1 },
  "runtimeFeedback": { "maxConns": 2, "minIdleConns": 0 },
  "cacheRefresh": { "maxConns": 2, "minIdleConns": 0 },
  "backgroundJobs": { "maxConns": 2, "minIdleConns": 0 }
},
"managementAdmission": {
  "m2MaxConcurrent": 8,
  "m3MaxConcurrent": 4
}
```

- [ ] **Step 3: 重建并重启后端**

用平时的启动方式(仓库根 `start.sh` 或手动 `go build` + 重启)。外部 config.json 编辑是 restart-applied,必须重启。

- [ ] **Step 4: 端到端验证**

1. 浏览器打开 `http://localhost:5173/system/settings`,Profile tab 正常加载,无 "加载 FX 映射模型失败",Network 面板无 503。
2. 后端启动日志无 `management admission budget clamped` 告警(新配置 m2=8 ≤ management.maxConns-1=8,不应触发)。
3. 反向验证告警:临时把 `m2MaxConcurrent` 改成 32 重启一次,日志应出现该 Warn(configured_m2=32, effective_m2=8),验证后改回 8。

- [ ] **Step 5: Commit**

```bash
cd /Users/qingli/Documents/proj/prism
git add config.json
git commit -m "chore: resize local pools to derived defaults for 8-core host"
```

(若 config.json 本不该入库则跳过此 commit,只改文件。)

---

## 明确不做(YAGNI)

- 前端对 503 + `Retry-After` 的只读 GET 轻量重试:默认值修好后 settings 页 fan-out(5)远低于 m2(8),触发不到;真再遇到再加。
- pool 值热加载:AGENTS.md 明确反模式。
- 按内存/Postgres `max_connections` 探测动态调整:单人 LAN 部署,clamp(8,16) 的静态公式足够,复杂度不值。
