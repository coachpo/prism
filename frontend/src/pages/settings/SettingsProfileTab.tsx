import type { RefObject } from "react";
import { Badge } from "@/components/ui/badge";
import { useLocale } from "@/i18n/useLocale";
import { SettingsSectionsNav } from "./SettingsSectionsNav";
import type { SettingsPageData } from "./useSettingsPageData";
import { AuditConfigurationSection } from "./sections/AuditConfigurationSection";
import { BackupSection } from "./sections/BackupSection";
import { BillingCurrencySection } from "./sections/BillingCurrencySection";
import { RetentionDeletionSection } from "./sections/RetentionDeletionSection";
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
    <div className="space-y-6">
      <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <Badge
            variant="outline"
            className="w-fit border-amber-500/30 bg-amber-500/15 text-amber-700 dark:text-amber-300"
          >
            {messages.settingsPage.profileScopedSettings}
          </Badge>
          <p className="text-sm text-amber-800 dark:text-amber-300">
            {messages.settingsPage.profileScopedDescription(data.selectedProfileLabel)}
          </p>
        </div>
      </div>

      <div className="space-y-4 lg:grid lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-6 lg:space-y-0">
        <aside className="lg:sticky lg:top-4 lg:h-fit">
          <SettingsSectionsNav
            activeSectionId={activeSectionId ?? ""}
            onJumpToSection={onJumpToSection}
          />
        </aside>

        <div className="space-y-6">
          <BackupSection
            selectedProfileLabel={data.selectedProfileLabel}
            exporting={data.exporting}
            handleExport={data.handleExport}
            fileInputRef={data.fileInputRef}
            handleFileSelect={data.handleFileSelect}
            selectedFile={data.selectedFile}
            parsedConfig={data.parsedConfig}
            previewResult={data.previewResult}
            importSummary={data.importSummary}
            importing={data.importing}
            handleImport={data.handleImport}
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
          />

          <RetentionDeletionSection
            selectedProfileLabel={data.selectedProfileLabel}
            cleanupType={data.cleanupType}
            setCleanupType={data.setCleanupType}
            retentionPreset={data.retentionPreset}
            setRetentionPreset={data.setRetentionPreset}
            deleting={data.deleting}
            handleOpenDeleteConfirm={data.handleOpenDeleteConfirm}
          />
        </div>
      </div>
    </div>
  );
}
