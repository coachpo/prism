import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function readSource(relativePath) {
  return readFileSync(path.join(frontendDir, relativePath), "utf8");
}

function getDialogContentClass(source, componentName) {
  const match = source.match(/<DialogContent className="([^"]+)"/);

  assert.ok(match, `${componentName} must render DialogContent with a className`);

  return match[1];
}
test("model settings dialog uses the same content size as the models dialog", () => {
  const modelDialogClass = getDialogContentClass(
    readSource("src/pages/models/ModelDialog.tsx"),
    "ModelDialog",
  );
  const modelSettingsDialogClass = getDialogContentClass(
    readSource("src/pages/model-detail/ModelSettingsDialog.tsx"),
    "ModelSettingsDialog",
  );

  assert.equal(modelDialogClass, "max-h-[90vh] sm:max-w-4xl");
  assert.equal(
    modelSettingsDialogClass,
    modelDialogClass,
    "Model Settings dialog should match the New/Edit Model dialog content size",
  );
});
