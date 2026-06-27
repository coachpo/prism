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
const { enMessages } = load(path.join(frontendDir, "src/i18n/messages/en.ts"));
const { zhCNMessages } = load(path.join(frontendDir, "src/i18n/messages/zh-CN.ts"));

test("models dialog no longer renders overflow promotion copy", () => {
  assert.doesNotMatch(
    modelDialogSource,
    /Overflow promotion target|overflowPromotionTarget|context_overflow_promotion_target_id/,
    "overflow promotion UI should be hard-deleted from the model dialog",
  );
  assert.equal(Object.hasOwn(enMessages.modelsUi, "overflowPromotionTarget"), false);
  assert.equal(Object.hasOwn(zhCNMessages.modelsUi, "overflowPromotionTarget"), false);
});

test("models dialog no longer renders access target authoring", () => {
  assert.doesNotMatch(modelDialogSource, /AccessTargetsEditor|accessTargets|targetModelsForApiFamily/);
});

test("model detail access target copy avoids fallback wording", () => {
  assert.equal(enMessages.modelsUi.modelFallbackTargets, "Model targets");
  assert.equal(enMessages.modelsUi.modelTarget, "Model target");
  assert.equal(enMessages.modelsUi.selectSameFamilyModel, "Select target model");
  assert.equal(
    enMessages.modelsUi.noSameFamilyModelsAvailable,
    "No other same-family models are available. Save disabled now, or add a model target later before enabling.",
  );

  const englishModelTargetCopy = [
    enMessages.modelsUi.modelFallbackTargets,
    enMessages.modelsUi.modelTarget,
    enMessages.modelsUi.selectSameFamilyModel,
    enMessages.modelsUi.noSameFamilyModelsAvailable,
  ].join("\n");
  assert.doesNotMatch(englishModelTargetCopy, /fallback/i);

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
  assert.doesNotMatch(chineseModelTargetCopy, /回退|退避/);
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
  assert.equal(enMessages.modelsUi.openaiAcceptedFormat, "OpenAI accepted format");
  assert.equal(enMessages.modelsUi.openaiAcceptedFormatDualNative, "Dual native");
  assert.equal(enMessages.modelsUi.openaiAcceptedFormatResponsesOnly, "Responses only");
  assert.equal(enMessages.modelsUi.openaiAcceptedFormatChatCompletionsOnly, "Chat Completions only");
  assert.equal(zhCNMessages.modelsUi.openaiAcceptedFormat, "OpenAI 接受格式");
});
