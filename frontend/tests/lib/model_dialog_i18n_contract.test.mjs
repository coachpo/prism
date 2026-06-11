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

test("models dialog overflow promotion copy is locale-backed", () => {
  assert.doesNotMatch(
    modelDialogSource,
    /OVERFLOW_PROMOTION_TARGET_LABEL = "Overflow promotion target"/,
    "dialog label must come from the locale catalog",
  );
  assert.match(
    modelDialogSource,
    /label=\{copy\.overflowPromotionTarget\}/,
    "overflow promotion label should use modelsUi copy",
  );
  assert.match(
    modelDialogSource,
    /description=\{copy\.overflowPromotionTargetDescription\}/,
    "overflow promotion helper should use modelsUi copy",
  );
  assert.equal(enMessages.modelsUi.overflowPromotionTarget, "Overflow promotion target");
  assert.equal(
    enMessages.modelsUi.overflowPromotionTargetDescription,
    "Optional selected-profile model ID for one replay when a non-stream response proves context overflow. Prism validates eligibility on save.",
  );
  assert.equal(zhCNMessages.modelsUi.overflowPromotionTarget, "溢出提升目标");
  assert.equal(
    zhCNMessages.modelsUi.overflowPromotionTargetDescription,
    "可选的所选配置档案模型 ID；当非流式响应证明上下文溢出时用于一次重放。Prism 会在保存时验证资格。",
  );
});
