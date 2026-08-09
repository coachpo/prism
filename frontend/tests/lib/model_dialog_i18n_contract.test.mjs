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
    zhCNMessages.modelsUi.modelFallbackTargetsDescription,
    zhCNMessages.modelsUi.modelTarget,
    zhCNMessages.modelsUi.noAccessTargetsSelected,
    zhCNMessages.modelsUi.noSameFamilyModelsAvailable,
    zhCNMessages.modelsUi.selectSameFamilyModel,
    zhCNMessages.modelsUi.terminalTargetsDescription,
  ].join("\n");
  assert.match(chineseModelTargetCopy, /模型目标|选择目标模型/);
});

test("single-instance copy uses neutral labels", () => {
  assert.equal(zhCNMessages.loadbalanceStrategiesPage.title, "负载均衡策略");
  assert.equal(
    zhCNMessages.loadbalanceStrategiesPage.description,
    "管理可复用的 Ban Policy 与终端目标路由族",
  );
  assert.equal(
    zhCNMessages.loadbalanceStrategyDialog.description,
    "配置可复用的终端目标路由族与 Ban Policy。",
  );
  assert.equal(zhCNMessages.settingsPage.profileTab, "全局");
  assert.equal(zhCNMessages.settingsPage.globalTab, "实例");
  assert.equal(
    zhCNMessages.modelDetail.noLoadbalanceStrategiesAvailable,
    "没有可用的负载均衡策略。请先在负载均衡策略页面创建一个。",
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
    /setOpenAIAcceptedFormatOnForm\(prev, value as OpenAIAcceptedFormat\)/,
    "accepted-format control should update through modelFormState helpers",
  );
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormat, "OpenAI 接受格式");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatDualNative, "Responses + Chat Completions");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatResponsesOnly, "仅 Responses");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormatChatCompletionsOnly, "仅 Chat Completions");
});
