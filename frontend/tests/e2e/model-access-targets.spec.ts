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

test("model target i18n copy stays in locale catalogs", async () => {
  const modelDialogSource = readSource("frontend/src/pages/models/ModelDialog.tsx");
  const modelsPageDataSource = readSource("frontend/src/pages/models/useModelsPageData.ts");
  const detailFormSource = readSource("frontend/src/pages/model-detail/useModelDetailModelForm.ts");
  const accessTargetsEditorSource = readSource("frontend/src/pages/models/AccessTargetsEditor.tsx");
  const modelsTableSource = readSource("frontend/src/pages/models/ModelsTable.tsx");
  const overviewCardsSource = readSource("frontend/src/pages/model-detail/OverviewCards.tsx");
  const enMessagesSource = readSource("frontend/src/i18n/messages/en.ts");
  const zhMessagesSource = readSource("frontend/src/i18n/messages/zh-CN.ts");

  expect(modelDialogSource).not.toContain("Enabled saves require at least one enabled access target. Turn this off while adjusting target attachments.");
  expect(modelDialogSource).not.toContain("New models start disabled so you can save a draft now and attach access targets later. Enabled saves require at least one enabled target.");
  expect(modelsPageDataSource).not.toContain("Enabled models need at least one enabled same-family access target. Save with Enabled off to attach targets later.");
  expect(detailFormSource).not.toContain("Add at least one enabled same-family access target before saving an enabled model.");
  expect(accessTargetsEditorSource).not.toContain('>Access targets<');
  expect(accessTargetsEditorSource).not.toContain("Select same-family models or standalone connections. Prism tries enabled rows in this order using the selected legacy strategy.");
  expect(accessTargetsEditorSource).not.toContain("Current API family:");
  expect(accessTargetsEditorSource).not.toContain("New connection");
  expect(accessTargetsEditorSource).not.toContain("Model target");
  expect(accessTargetsEditorSource).not.toContain("Connection target");
  expect(accessTargetsEditorSource).not.toContain("Select same-family model");
  expect(accessTargetsEditorSource).not.toContain("Select same-family connection");
  expect(accessTargetsEditorSource).not.toContain("No access targets selected. This model can be saved disabled and have a target attached later. Enabled saves still require at least one enabled target.");
  expect(accessTargetsEditorSource).not.toContain("No unattached same-family standalone connections are available. This model can be saved disabled and have a target attached later; enabled saves still require a target.");
  expect(accessTargetsEditorSource).not.toContain("No other same-family models are available. This model can be saved disabled and have a target attached later; enabled saves still require a target.");
  expect(modelsTableSource).not.toContain("Needs target");
  expect(overviewCardsSource).not.toContain("Needs target");
  expect(overviewCardsSource).not.toContain(">Access targets<");

  expect(enMessagesSource).toContain("accessTargets:");
  expect(enMessagesSource).toContain("accessTargetsDescription:");
  expect(enMessagesSource).toContain("currentApiFamily:");
  expect(enMessagesSource).toContain("newConnection:");
  expect(enMessagesSource).toContain("modelTarget:");
  expect(enMessagesSource).toContain("connectionTarget:");
  expect(enMessagesSource).toContain("selectSameFamilyModel:");
  expect(enMessagesSource).toContain("selectSameFamilyConnection:");
  expect(enMessagesSource).toContain("noAccessTargetsSelected:");
  expect(enMessagesSource).toContain("noSameFamilyConnectionsAvailable:");
  expect(enMessagesSource).toContain("noSameFamilyModelsAvailable:");
  expect(enMessagesSource).toContain("needsTarget:");
  expect(enMessagesSource).toContain("newModelEnabledDescription:");
  expect(enMessagesSource).toContain("editModelEnabledDescription:");
  expect(enMessagesSource).toContain("enabledAccessTargetRequired:");

  expect(zhMessagesSource).toContain("accessTargets:");
  expect(zhMessagesSource).toContain("accessTargetsDescription:");
  expect(zhMessagesSource).toContain("currentApiFamily:");
  expect(zhMessagesSource).toContain("newConnection:");
  expect(zhMessagesSource).toContain("modelTarget:");
  expect(zhMessagesSource).toContain("connectionTarget:");
  expect(zhMessagesSource).toContain("selectSameFamilyModel:");
  expect(zhMessagesSource).toContain("selectSameFamilyConnection:");
  expect(zhMessagesSource).toContain("noAccessTargetsSelected:");
  expect(zhMessagesSource).toContain("noSameFamilyConnectionsAvailable:");
  expect(zhMessagesSource).toContain("noSameFamilyModelsAvailable:");
  expect(zhMessagesSource).toContain("needsTarget:");
  expect(zhMessagesSource).toContain("newModelEnabledDescription:");
  expect(zhMessagesSource).toContain("editModelEnabledDescription:");
  expect(zhMessagesSource).toContain("enabledAccessTargetRequired:");
});
