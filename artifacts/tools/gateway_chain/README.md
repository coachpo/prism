# 网关请求链路实测套件

用 `./start.sh` 真实启动 Prism，配置真实上游模型，向本网关发送真实请求，然后逐环节核对
整条链路记录了它应该记录的东西。不使用任何 mock 上游、不使用内存数据库、不使用 `httptest`。

它回答的是运维视角的问题：**请求真的转发出去了吗？转发的每一跳留痕了吗？打开或关闭请求审计
会不会影响转发？审计该记的记了、不该记的没记吗？记下来的东西读得回来吗？**

## 与仓库既有测试的关系

`backend/tests/` 下的 contract / integration / runtime / priority 套件是密闭回归，CI 每次都跑，
不碰真实网络。本套件相反：它花真实上游额度、依赖 Docker 和本机端口，因此**不进 CI**，
只在需要验证真实部署链路时手动运行。两者不重叠也不互相替代。

## 前置条件

- `docker` + `docker compose`、`go`、`python3`（仅标准库，3.9 起可用）
- 端口 `8000`（后端）与 `15432`（PostgreSQL）空闲
- 一个可用的上游凭据。没有它，依赖真实模型的用例会报 **BLOCKED**，不会伪装成通过。

## 运行

```bash
python3 artifacts/tools/gateway_chain up
```

```bash
PRISM_CHAIN_UPSTREAM_API_KEY='<真实上游 key>' \
PRISM_CHAIN_UPSTREAM_ENDPOINT_ID=3 \
PRISM_CHAIN_LIVE_MODEL='codex/gpt-5.6-luna' \
python3 artifacts/tools/gateway_chain run
```

```bash
python3 artifacts/tools/gateway_chain down
```

`all` 子命令等于 `up` + `run` + `down`。`--only L5-01,L5-03` 只跑指定用例。

### 从一台在跑的实例同步配置

把线上实例的引导配置和数据库整体拉到本地，只改写两处必须本地化的值
（`database.url` 改成启动器要求的本地 DSN、`http.corsAllowedOrigins` 改成本地前端），
其余包括 `runtime.secretEncryptionKey` 原样保留——上游密钥是用它加密的，换掉就全部解不开。

```bash
PRISM_CHAIN_SYNC_HOST=capy \
PRISM_CHAIN_SYNC_CONTAINER=prism-a-postgres-1 \
PRISM_CHAIN_SYNC_DATABASE=prism_v1 \
PRISM_CHAIN_SYNC_CONFIG_PATH=/home/ubuntu/orange_work/curse/prism-a/prism-config/config.json \
python3 artifacts/tools/gateway_chain sync
```

`sync` 会覆盖本地 `config.json` 并重建本地数据库，是破坏性操作，确认目标后再执行。

## 环境变量

| 变量 | 作用 |
|---|---|
| `PRISM_CHAIN_UPSTREAM_API_KEY` | 写入本地实例某个端点的真实上游密钥。只写本地，绝不回写远端。 |
| `PRISM_CHAIN_UPSTREAM_ENDPOINT_ID` | 上面这个密钥写到哪个端点 |
| `PRISM_CHAIN_LIVE_MODEL` | 调用方可见的模型名，用于成功路径 |
| `PRISM_CHAIN_SYNC_*` | 同步来源：`HOST` / `CONTAINER` / `DATABASE` / `CONFIG_PATH` |
| `PRISM_REPO_ROOT` | 仓库根，默认从本文件位置推出 |
| `PRISM_CHAIN_EVIDENCE_ROOT` | 证据输出目录，默认 `artifacts/evidence/` |

## 四种结果，互不塌陷

| 结果 | 含义 |
|---|---|
| `PASSED` | 断言全部成立 |
| `FAILED` | 断言执行了但不成立 |
| `BLOCKED` | 前置条件缺失（例如没有可用上游凭据），**没有验证过任何东西** |
| `ERRORED` | 用例根本没能求值（例如数据库读不到），或者一个断言都没记录 |

数据库读不到时整轮直接退出码 3，不会把读取失败呈现成"零条记录"。
一个用例如果一条断言都没记录，判为 `ERRORED` 而不是 `PASSED`——空跑不算通过。

## 用例矩阵

| 环节 | 用例 |
|---|---|
| L0 启动器 | 引导契约、`/health` 就绪、还原库不重复迁移、同步配置保留密钥、就绪态与数据库可达性一致、**就绪态对真实断库有反应** |
| L1 入口准入 | 未注册路径、错误方法、未知模型（三者均须零副作用）、代理密钥强制与实例开关一致、响应关联头能否回查 |
| L2/L3 真实转发 | 非流式成功、流式 SSE 成功、原生 Responses、`GET /v1/models`、上游拒绝如实回传、多目标故障转移 |
| L4 请求记录 | 标识字段齐全、用量事件与尝试行一致、负载均衡事件只记路由状态转移、当日分区、记录路径不落凭据 |
| L5 审计开关 | 关闭态零审计行、`metadata_only` 不落体、`body_capture` 落双向体且脱敏、流式整条累积、切换下一请求即生效、三种模式转发结果一致、失败请求同样审计 |
| L6 读取面 | 请求列表与详情、未知过滤参数被拒、CSV 导出（必须带时间范围）、审计列表必须带时间窗、审计详情与原始体读取、零/缺失/失败/截断四态可区分、未审计的请求读回来是"未审计"而不是空 |
| L7 遥测管道 | **不可达上游不会卡死管道**、外发盒能排空、没有行在不计次的情况下被反复重试、永久插不进去的行必须进隔离区、已服务的请求数等于已记录的请求数 |

### 两条会动实例的用例

绝大多数用例是只读的观察，这两条不是，它们主动制造故障才能证明缺陷存在：

- **L0-06** 停掉 PostgreSQL 容器，确认网关确实无法服务，然后问 `/health`，再把容器拉起来等自愈。
  容器的数据卷不动，栈是专用测试实例，全程可逆。这条存在的理由是 L0-05 只能观察"恰好存在"的矛盾，
  而 `/health` 什么都不查，正常情况下永远自洽。
- **L7-05** 临时建一个指向 `127.0.0.1:9`（保留的 discard 端口，必然连不上）的端点、连接和模型，
  带代理密钥打一发。连接被拒不是上游 HTTP 状态，会落到网关侧的"全部连接失败"路径。
  跑完删除这三样；如果这一发把管道卡住了，它还会把自己造成的滞留行清掉并在报告里写明清掉了几条——
  否则一个真实发现会把后面几十条用例全部连坐成无关失败。

L7 是在一次实测中发现真实缺陷后补的：一条用量事件带着
`proxy_api_key_attribution_state='identified'` 却是 `proxy_api_key_id_snapshot=NULL`，
违反 `ck_usage_request_events_proxy_key_snapshot_consistent`，被每秒数次无限重试且
`core_attempt_count` 始终为 0，把排在它后面的 20 条正常记录全部头阻塞。
期间网关照常转发、`/health` 照常报 ready，但任何请求都不再被记录。

## 环境级故障哨兵

一旦遥测外发盒卡住，后续每个断言都会因为同一个原因失败。把二十条派生失败摆出来会淹没那一个原因，
所以运行器在每个用例前检查哨兵：命中时该用例直接判 `ERRORED` 并写明环境故障，不再执行。
L7 那组用例本身是用来诊断这个故障的，不受哨兵拦截。报告里的 `environment_faults` 字段列出命中过的故障。

## 关联方式与它的前提

运行时响应带 `X-Prism-Ingress-Request-Id`，但该值由中间件生成，**与
`request_logs.ingress_request_id` 不是同一个值**，调用方无法用它回查自己的请求。
套件因此改用「请求前取 `max(id)` 水位线」关联，这要求**运行期间本实例只有套件一个流量来源**。
用例 `L1-05` 会显式检查这个关联头能否回查，检查不通过时把它记为 GAP 而不是绕过。

## 证据

每轮写 `artifacts/evidence/gateway-chain-<UTC 时间戳>/report.json`，含逐条断言的期望值与实际值。
写入前对所有已知密钥做字面量替换（上游密钥、本轮创建的代理密钥）。

## 副作用与收尾

- 会在本地实例创建一个代理密钥，跑完删除（`--keep-proxy-key` 可保留）
- 会改写审计策略，跑完恢复成开始前的值
- 会向真实上游发若干次请求；提示词极短、`max_tokens` 上限 32
- 不修改远端实例的任何东西

## 单元测试

```bash
python3 -m unittest discover -s artifacts/tools -p 'test_gateway_chain.py'
```

覆盖脱敏、bytea 解码、四态判定、compose 项目名推导——这些写错会让整轮实测悄悄给出错误结论。
