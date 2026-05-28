import { expect, test } from "@playwright/test";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const currentDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(currentDir, "../../..");
const readSource = (path: string) => readFileSync(resolve(repoRoot, path), "utf8");

test("model access targets create edit reorder", async () => {
  const appSource = readSource("frontend/src/App.tsx");
  const modelDialogSource = readSource("frontend/src/pages/models/ModelDialog.tsx");
  const detailSource = readSource("frontend/src/pages/ModelDetailPage.tsx");
  const settingsSource = readSource("frontend/src/pages/model-detail/ModelSettingsDialog.tsx");

  expect(appSource).toContain('path="/models/:id"');
  expect(appSource).not.toContain('/models/:id/proxy');
  expect(existsSync(resolve(repoRoot, "frontend/src/pages/ProxyModelDetailPage.tsx"))).toBe(false);
  expect(modelDialogSource).toContain("AccessTargetsEditor");
  expect(settingsSource).toContain("AccessTargetsEditor");
  expect(detailSource).toContain("AccessTargetsEditor");
  expect(modelDialogSource).not.toContain("model_type");
  expect(modelDialogSource).not.toContain("setModelType");
});

test("model access targets invalid target", async () => {
  const apiSource = readSource("frontend/src/lib/api/management.ts");
  const formSource = readSource("frontend/src/pages/models/modelFormState.ts");
  const editorSource = readSource("frontend/src/pages/models/AccessTargetsEditor.tsx");

  expect(apiSource).toContain("/api/models/${modelConfigId}/targets");
  expect(apiSource).toContain("/api/connections");
  expect(apiSource).not.toContain("/api/models/${modelConfigId}/connections");
  expect(apiSource).not.toContain("health-check-preview");
  expect(formSource).toContain("getAccessTargetOptionKeys");
  expect(editorSource).toContain('data-testid="access-targets-error"');
});
