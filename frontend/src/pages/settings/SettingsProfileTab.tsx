import type { RefObject } from "react";
import { SemanticCallout } from "@/components/SemanticCallout";
import { useLocale } from "@/i18n/useLocale";
import { SettingsSectionsNav } from "./SettingsSectionsNav";
import type { SettingsPageData } from "./useSettingsPageData";
import { AuditConfigurationSection } from "./sections/AuditConfigurationSection";
import { BackupSection } from "./sections/BackupSection";
import { BillingCurrencySection } from "./sections/BillingCurrencySection";
import { TimezoneSection } from "./sections/TimezoneSection";

interface SettingsProfileTabProps {
  activeSectionId: string | null;
  auditConfigurationRef: RefObject<HTMLDivElement | null>;
  data: SettingsPageData;
  isAuditConfigurationFocused: boolean;
  onJumpToSection: (sectionId: string) => void;
}

export function SettingsProfileTab({
  activeSectionId,
  auditConfigurationRef,
  data,
  isAuditConfigurationFocused,
  onJumpToSection,
}: SettingsProfileTabProps) {
  const { messages } = useLocale();

  return (
    <div className="flex flex-col gap-5">
      <SemanticCallout
        intent="info"
        title={messages.settingsPage.profileScopedSettings}
        description={messages.settingsPage.profileScopedDescription(data.selectedProfileLabel)}
        className="py-2.5"
      />

      <div className="flex flex-col gap-4 lg:grid lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-6">
        <aside className="lg:sticky lg:top-4 lg:h-fit">
          <SettingsSectionsNav
            activeSectionId={activeSectionId ?? ""}
            onJumpToSection={onJumpToSection}
          />
        </aside>

        <div className="flex flex-col gap-5">
          <BackupSection
            selectedProfileLabel={data.selectedProfileLabel}
            exportSecretsAcknowledged={data.exportSecretsAcknowledged}
            exportingMode={data.exportingMode}
            fileInputRef={data.fileInputRef}
            handleDangerousExport={data.handleDangerousExport}
            handleFileSelect={data.handleFileSelect}
            handleImport={data.handleImport}
            handlePreviewImport={data.handlePreviewImport}
            handleSafeExport={data.handleSafeExport}
            importing={data.importing}
            importSummary={data.importSummary}
            parsedConfig={data.parsedConfig}
            previewInvalidationReason={data.previewInvalidationReason}
            previewResult={data.previewResult}
            previewing={data.previewing}
            selectedFile={data.selectedFile}
            setExportSecretsAcknowledged={data.setExportSecretsAcknowledged}
          />

          <BillingCurrencySection
            billingDirty={data.billingDirty}
            renderSectionSaveState={data.renderSaveStateForSection}
            handleSaveCostingSettings={data.handleSaveCostingSettings}
            costingUnavailable={data.costingUnavailable}
            costingLoading={data.costingLoading}
            costingSaving={data.costingSaving}
            costingForm={data.costingForm}
            setCostingForm={data.setCostingForm}
            normalizedCurrentCosting={data.normalizedCurrentCosting}
            nativeModels={data.nativeModels}
            modelLabelMap={data.modelLabelMap}
            mappingConnections={data.mappingConnections}
            mappingLoading={data.mappingLoading}
            mappingModelId={data.mappingModelId}
            setMappingModelId={data.setMappingModelId}
            loadMappingConnections={data.loadMappingConnections}
            mappingEndpointId={data.mappingEndpointId}
            setMappingEndpointId={data.setMappingEndpointId}
            mappingEndpointOptions={data.mappingEndpointOptions}
            mappingFxRate={data.mappingFxRate}
            setMappingFxRate={data.setMappingFxRate}
            addMappingFxError={data.addMappingFxError}
            handleAddFxMapping={data.handleAddFxMapping}
            editingMappingKey={data.editingMappingKey}
            editingMappingFxRate={data.editingMappingFxRate}
            setEditingMappingFxRate={data.setEditingMappingFxRate}
            editMappingFxError={data.editMappingFxError}
            handleSaveEditFxMapping={data.handleSaveEditFxMapping}
            handleCancelEditFxMapping={data.handleCancelEditFxMapping}
            handleStartEditFxMapping={data.handleStartEditFxMapping}
            handleDeleteFxMapping={data.handleDeleteFxMapping}
          />

          <TimezoneSection
            timezoneDirty={data.timezoneDirty}
            renderSectionSaveState={data.renderSaveStateForSection}
            handleSaveCostingSettings={data.handleSaveCostingSettings}
            costingUnavailable={data.costingUnavailable}
            costingLoading={data.costingLoading}
            costingSaving={data.costingSaving}
            costingForm={data.costingForm}
            setCostingForm={data.setCostingForm}
            timezonePreviewText={data.timezonePreviewText}
            timezonePreviewZone={data.timezonePreviewZone}
          />

          <AuditConfigurationSection
            auditConfigurationRef={auditConfigurationRef}
            isAuditConfigurationFocused={isAuditConfigurationFocused}
            vendors={data.auditVendors}
            toggleAudit={data.toggleAudit}
            toggleBodies={data.toggleBodies}
            loadingRules={data.loadingRules}
            systemRulesOpen={data.systemRulesOpen}
            setSystemRulesOpen={data.setSystemRulesOpen}
            systemRules={data.systemRules}
            userRulesOpen={data.userRulesOpen}
            setUserRulesOpen={data.setUserRulesOpen}
            customRules={data.customRules}
            handleToggleRule={data.handleToggleRule}
            openAddRuleDialog={data.openAddRuleDialog}
            openEditRuleDialog={data.openEditRuleDialog}
            setDeleteRuleConfirm={data.setDeleteRuleConfirm}
            loadingUserAgentClientRules={data.loadingUserAgentClientRules}
            userAgentClientSystemRulesOpen={data.userAgentClientSystemRulesOpen}
            setUserAgentClientSystemRulesOpen={data.setUserAgentClientSystemRulesOpen}
            userAgentClientSystemRules={data.userAgentClientSystemRules}
            userAgentClientUserRulesOpen={data.userAgentClientUserRulesOpen}
            setUserAgentClientUserRulesOpen={data.setUserAgentClientUserRulesOpen}
            userAgentClientCustomRules={data.userAgentClientCustomRules}
            handleToggleUserAgentClientRule={data.handleToggleUserAgentClientRule}
            openAddUserAgentClientRuleDialog={data.openAddUserAgentClientRuleDialog}
            openEditUserAgentClientRuleDialog={data.openEditUserAgentClientRuleDialog}
            setDeleteUserAgentClientRuleConfirm={data.setDeleteUserAgentClientRuleConfirm}
          />
        </div>
      </div>
    </div>
  );
}
