import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { zhCNMessages } = load(path.join(frontendDir, "src/i18n/messages/zh-CN.ts"));

test("model detail distinguishes service connections from forwarding to models", () => {
  assert.equal(zhCNMessages.modelsUi.modelFallbackTargets, "转发到其他模型");
  assert.equal(zhCNMessages.modelsUi.modelTarget, "转发到其他模型");
  // 「加目标」曾经有三个名字（新增/新建/选择目标模型）：统一为动宾短语「添加」。
  assert.equal(zhCNMessages.modelsUi.selectSameFamilyModel, "添加转发到其他模型");
  assert.equal(
    zhCNMessages.modelsUi.noSameFamilyModelsAvailable,
    "暂无其他同接口类型的已启用模型可供转发。已有服务连接仍可正常使用；需要备用模型时再添加。",
  );

  const chineseModelTargetCopy = [
    zhCNMessages.modelsUi.accessTargetsDescription,
    zhCNMessages.modelsUi.modelFallbackTargets,
    zhCNMessages.modelsUi.modelTarget,
    zhCNMessages.modelsUi.noAccessTargetsSelected,
    zhCNMessages.modelsUi.noSameFamilyModelsAvailable,
    zhCNMessages.modelsUi.selectSameFamilyModel,
    zhCNMessages.modelsUi.noTerminalTargetsSelected,
  ].join("\n");
  assert.match(chineseModelTargetCopy, /转发到其他模型/);
});

test("single-instance copy uses neutral labels", () => {
  assert.equal(zhCNMessages.loadbalanceStrategiesPage.title, "路由策略");
  assert.equal(
    zhCNMessages.loadbalanceStrategiesPage.description,
    "配置路由策略：准确表达路由方式、显式默认与安全的修改；运行态调查请进入路由健康。",
  );
  assert.equal(
    zhCNMessages.routingStrategyDialog.description,
    "配置路由方式、失败判定、重试节奏、封禁条件，并在保存前预览连续失败反馈。",
  );
  assert.equal(zhCNMessages.settingsPage.globalTab, "显示与记录");
  assert.equal(zhCNMessages.settingsPage.instanceTab, "访问与数据");
  // Fixed terminology is 路由策略 everywhere, including inside the
  // loadbalanceStrategy* message keys that still carry the old name.
  assert.equal(
    zhCNMessages.modelDetail.noLoadbalanceStrategiesAvailable,
    "还没有可用的服务选择方式。可以创建默认策略，或到「路由策略」页面添加。",
  );
  assert.equal(
    JSON.stringify(zhCNMessages).includes("负载均衡策略"),
    false,
    "no message may still say 负载均衡策略",
  );
  assert.equal(zhCNMessages.modelDetail.noEndpointsFound, "未找到可用服务。");
});
