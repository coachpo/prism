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
    "Choose an enabled same-family model for recursive overflow promotion. Prism validates chain depth, cycles, terminal loops, and routing-plan issues on save.",
  );
  assert.equal(zhCNMessages.modelsUi.overflowPromotionTarget, "溢出提升目标");
  assert.equal(
    zhCNMessages.modelsUi.overflowPromotionTargetDescription,
    "选择已启用的同家族模型作为递归溢出提升目标。Prism 会在保存时验证链深度、环路、终端循环与路由计划问题。",
  );
});
