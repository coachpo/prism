import type { RefObject } from "react";
import { SettingsSectionsNav } from "./SettingsSectionsNav";
import type { SettingsPageData } from "./useSettingsPageData";
import { AuditConfigurationSection } from "./sections/AuditConfigurationSection";
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
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-4 lg:grid lg:grid-cols-[220px_minmax(0,1fr)] lg:gap-6">
        <aside className="lg:sticky lg:top-4 lg:h-fit">
          <SettingsSectionsNav
            activeSectionId={activeSectionId ?? ""}
            onJumpToSection={onJumpToSection}
          />
        </aside>

        <div className="flex flex-col gap-5">
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
            apiFamilyAuditSettings={data.apiFamilyAuditSettings}
            apiFamilyAuditSettingsDirty={data.apiFamilyAuditSettingsDirty}
            loadingAPIFamilyAuditSettings={data.loadingAPIFamilyAuditSettings}
            savingAPIFamilyAuditSettings={data.savingAPIFamilyAuditSettings}
            renderSectionSaveState={data.renderSaveStateForSection}
            handleSaveAPIFamilyAuditSettings={data.handleSaveAPIFamilyAuditSettings}
            setAPIFamilyAuditCaptureBodies={data.setAPIFamilyAuditCaptureBodies}
            setAPIFamilyAuditEnabled={data.setAPIFamilyAuditEnabled}
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
