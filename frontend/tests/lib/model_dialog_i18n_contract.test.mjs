import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const modelDialogSource = readFileSync(
  path.join(frontendDir, "src/pages/models/ModelDialog.tsx"),
  "utf8",
);

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { zhCNMessages } = load(path.join(frontendDir, "src/i18n/messages/zh-CN.ts"));

test("models dialog keeps current model-target copy", () => {
  assert.equal(Object.hasOwn(zhCNMessages.modelsUi, "overflowPromotionTarget"), false);
});

test("model detail access target copy avoids fallback wording", () => {
  assert.equal(zhCNMessages.modelsUi.modelFallbackTargets, "模型目标");
  assert.equal(zhCNMessages.modelsUi.modelTarget, "模型目标");
  assert.equal(zhCNMessages.modelsUi.selectSameFamilyModel, "选择目标模型");
  assert.equal(
    zhCNMessages.modelsUi.noSameFamilyModelsAvailable,
    "没有其他可用的同家族模型。现在可以先以禁用状态保存，稍后在启用前再添加模型目标。",
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
  assert.match(chineseModelTargetCopy, /模型目标|选择目标模型/);
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

test("models dialog shows accepted-format controls only for OpenAI models", () => {
  assert.match(
    modelDialogSource,
    /formData\.api_family === "openai" \? \([\s\S]*?model-openai-accepted-format[\s\S]*?\) : null/,
    "accepted-format control should be rendered behind the OpenAI family guard",
  );
  assert.match(
    modelDialogSource,
    /value=\{openAIAcceptedFormatValue\}/,
    "accepted-format control should consume the normalized form-state value",
  );
  assert.match(
    modelDialogSource,
    /setOpenAIAcceptedFormatOnForm\(prev, fromSelectValue<OpenAIAcceptedFormat>\(value\)\)/,
    "accepted-format control should update through modelFormState helpers",
  );
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormat, "OpenAI 接受格式");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatDualNative, "Responses + Chat Completions");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatResponsesOnly, "仅 Responses");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatChatCompletionsOnly, "仅 Chat Completions");
});

test("models dialog authors the OpenAI image dimension through its own control", () => {
  assert.match(
    modelDialogSource,
    /formData\.api_family === "openai" \? \([\s\S]*?model-openai-image-operations[\s\S]*?\) : null/,
    "image-operations control should be rendered behind the OpenAI family guard",
  );
  assert.match(
    modelDialogSource,
    /setOpenAIImageOperationsOnForm\(prev, fromSelectValue<OpenAIImageOperations>\(value\)\)/,
    "image-operations control should update through modelFormState helpers",
  );
  // Both dimensions must offer an explicit "not served" option, because either
  // one alone is a valid authoring outcome.
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatNone, "不支持文本");
  assert.equal(zhCNMessages.modelsUi.openaiImageOperations, "OpenAI 图片能力");
  assert.equal(zhCNMessages.modelsUi.openaiImageOperationsNone, "不支持图片");
  assert.equal(zhCNMessages.modelsUi.openaiImageOperationsGenerations, "仅生图");
  assert.equal(zhCNMessages.modelsUi.openaiImageOperationsEdits, "仅改图");
  assert.equal(zhCNMessages.modelsUi.openaiImageOperationsGenerationsAndEdits, "生图 + 改图");
});
