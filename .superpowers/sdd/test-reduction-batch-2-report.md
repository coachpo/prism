# Test Reduction Batch 2 Report

## 变更清单
- 删除 `frontend/tests/lib/dashboard_contract.test.mjs`
- 删除 `frontend/tests/main/main_entrypoint_structure.test.mjs`
- 删除 `frontend/tests/loadbalance/ban_policy_schema_contract.test.mjs`
- 删除 `frontend/tests/loadbalance/loadbalance_strategy_form_state_contract.test.mjs`
- 瘦身 `frontend/tests/lib/dashboard_bootstrap_contract.test.mjs`
- 瘦身 `frontend/tests/lib/management_contract.test.mjs`
- 瘦身 `frontend/tests/lib/model_dialog_i18n_contract.test.mjs`
- 更新 `.github/workflows/ci.yml`，在 `Run frontend seam tests` 前新增 `Run frontend unit tests`
- 更新 `frontend/tests/AGENTS.md`，同步当前 CI/test split 与 5 个 Playwright journey specs

## 删除覆盖去向表
| 删除文件 | 覆盖去向 |
|---|---|
| `frontend/tests/lib/dashboard_contract.test.mjs` | 仪表盘路由/数据形状保留在 `frontend/tests/lib/dashboard_bootstrap_contract.test.mjs` 与 `frontend/tests/lib/dashboard_routing_list_contract.test.mjs`；源码负正则按 brief 直接删除，不再保留 |
| `frontend/tests/main/main_entrypoint_structure.test.mjs` | 无脚本引用的孤儿；无需保留覆盖 |
| `frontend/tests/loadbalance/ban_policy_schema_contract.test.mjs` | Ban Policy schema 与 payload 语义由 `frontend/src/features/loadbalance/banPolicySchemas.test.ts` 持有 |
| `frontend/tests/loadbalance/loadbalance_strategy_form_state_contract.test.mjs` | Ban Policy form-state/schema 语义由 `frontend/src/features/loadbalance/banPolicySchemas.test.ts` 持有；本孤儿目录不再保留 |

## 局部瘦身映射
| 文件 | 删除内容 | 保留内容 |
|---|---|---|
| `frontend/tests/lib/dashboard_bootstrap_contract.test.mjs` | 5 个 `doesNotMatch`/源码正则旧概念断言 | `shouldApplyDashboardSnapshotRevision`、`snapshot_revision`、`DashboardRecentActivityWatermark`、`dashboardRecentActivity` 等活契约形状与行为断言 |
| `frontend/tests/lib/management_contract.test.mjs` | 3 个 removed-concept 拒绝测试（`cheapest_eligible_context`、removed retry key、removed ban value） | 现存 loadbalance strategy normalization、endpoint contract、model connection/target route shape、connection references 断言 |
| `frontend/tests/lib/model_dialog_i18n_contract.test.mjs` | 旧 UI/文案 `doesNotMatch` 断言 | 当前文案键值与 OpenAI accepted-format 控件形状断言 |

## 验证命令及结果
| 命令 | 结果 |
|---|---|
| `cd frontend && pnpm run test:lib` | 通过 |
| `cd frontend && pnpm exec vitest run` | 通过 |
| `cd frontend && pnpm run build` | 通过 |
| `cd frontend && pnpm run lint` | 通过 |
| `cd frontend && pnpm run test:server` | 通过 |
| `cd frontend && pnpm run test:e2e` | 通过 |
| `cd /Users/qingli/Documents/proj/prism && rg -l "doesNotMatch" frontend/tests/lib` | 仅剩 `frontend/tests/lib/dashboard_routing_list_contract.test.mjs` |

## commit hash
- `HEAD`（提交后以 `git rev-parse HEAD` 为准）

## concerns
- Brief 的 `rg -l "doesNotMatch" frontend/tests/lib` 目标“只剩 `model_dialog_i18n_contract.test.mjs`（0 个更好）”与仓库现状不一致；本批删除后仍剩 `frontend/tests/lib/dashboard_routing_list_contract.test.mjs` 的活跃布局断言。
