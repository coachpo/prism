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

function getFirstTagClass(source, componentName, tagName) {
  const match = source.match(new RegExp(`<${tagName}[^>]*className="([^"]+)"`));

  assert.ok(match, `${componentName} must render ${tagName} with a className`);

  return match[1];
}

function getFormTag(source, componentName) {
  const match = source.match(/<form\b[^>]*>/);

  assert.ok(match, `${componentName} must render a form`);

  return match[0];
}

function getClassValues(source) {
  return [...source.matchAll(/className="([^"]+)"/g)].map((match) => match[1]);
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

test("model settings dialog mirrors the models dialog shell layout", () => {
  const modelDialogSource = readSource("src/pages/models/ModelDialog.tsx");
  const modelSettingsDialogSource = readSource("src/pages/model-detail/ModelSettingsDialog.tsx");

  assert.doesNotMatch(modelDialogSource, /<DialogHeader[^>]*className=/);
  assert.doesNotMatch(
    modelSettingsDialogSource,
    /<DialogHeader[^>]*className=/,
    "Model Settings dialog should use the default DialogHeader shell",
  );

  assert.equal(
    getFirstTagClass(modelSettingsDialogSource, "ModelSettingsDialog", "form"),
    getFirstTagClass(modelDialogSource, "ModelDialog", "form"),
    "Model Settings form should use the same layout class as the New/Edit Model form",
  );

  const settingsFormTag = getFormTag(modelSettingsDialogSource, "ModelSettingsDialog");
  assert.match(settingsFormTag, /\bautoComplete="off"/);
  assert.match(settingsFormTag, /\bnoValidate\b/);

  assert.equal(
    getFirstTagClass(modelSettingsDialogSource, "ModelSettingsDialog", "DialogBody"),
    getFirstTagClass(modelDialogSource, "ModelDialog", "DialogBody"),
    "Model Settings body should use the same scroll layout class as the New/Edit Model body",
  );
  assert.equal(
    getFirstTagClass(modelSettingsDialogSource, "ModelSettingsDialog", "DialogFooter"),
    getFirstTagClass(modelDialogSource, "ModelDialog", "DialogFooter"),
    "Model Settings footer should use the same layout class as the New/Edit Model footer",
  );

  const modelSettingsClasses = getClassValues(modelSettingsDialogSource);
  const expectedCardClasses = [
    "flex flex-col gap-4 rounded-lg border bg-muted/20 p-4",
    "flex flex-col gap-4 rounded-lg border bg-muted/15 p-4",
    "flex flex-col gap-4 rounded-lg border p-4",
    "flex flex-col gap-3 rounded-lg border bg-muted/15 p-4",
  ];

  for (const className of expectedCardClasses) {
    assert.ok(
      getClassValues(modelDialogSource).includes(className),
      `New/Edit Model dialog should keep reference card class: ${className}`,
    );
    assert.ok(
      modelSettingsClasses.includes(className),
      `Model Settings dialog should mirror reference card class: ${className}`,
    );
  }

  assert.doesNotMatch(
    modelSettingsDialogSource,
    /\{copy\.configuration\}/,
    "Model Settings basics card should not add a separate Configuration heading absent from New Model",
  );
  assert.match(
    modelSettingsDialogSource,
    /<Label>\{fieldCopy\.vendor\}<\/Label>[\s\S]*<Label>\{fieldCopy\.apiFamily\}<\/Label>[\s\S]*htmlFor="edit-model-id"[\s\S]*htmlFor="edit-display-name"/,
    "Model Settings basics fields should follow the New Model order: vendor, API family, model ID, display name",
  );
});
