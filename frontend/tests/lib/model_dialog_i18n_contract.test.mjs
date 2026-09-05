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

test("model detail access target copy avoids fallback wording", () => {
  assert.equal(zhCNMessages.modelsUi.modelFallbackTargets, "模型目标");
  assert.equal(zhCNMessages.modelsUi.modelTarget, "模型目标");
  // 「加目标」曾经有三个名字（新增/新建/选择目标模型）：统一为动宾短语「添加」。
  assert.equal(zhCNMessages.modelsUi.selectSameFamilyModel, "添加模型目标");
  assert.equal(
    zhCNMessages.modelsUi.noSameFamilyModelsAvailable,
    "没有其他可用的同家族模型配置。现在可以先以禁用状态保存，稍后在启用前再添加模型目标。",
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
  assert.match(chineseModelTargetCopy, /模型目标/);
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
  assert.equal(zhCNMessages.settingsPage.globalTab, "全局");
  assert.equal(zhCNMessages.settingsPage.instanceTab, "实例");
  // Fixed terminology is 路由策略 everywhere, including inside the
  // loadbalanceStrategy* message keys that still carry the old name.
  assert.equal(
    zhCNMessages.modelDetail.noLoadbalanceStrategiesAvailable,
    "没有可用的路由策略。请先在路由策略页面创建一个。",
  );
  assert.equal(
    JSON.stringify(zhCNMessages).includes("负载均衡策略"),
    false,
    "no message may still say 负载均衡策略",
  );
  assert.equal(zhCNMessages.modelDetail.noEndpointsFound, "未找到可用端点。");
});
