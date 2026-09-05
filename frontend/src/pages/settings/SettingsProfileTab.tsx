import type { RefObject } from "react";
import { SettingsSectionsNav } from "./SettingsSectionsNav";
import type { SettingsPageData } from "./useSettingsPageData";
import { AuditConfigurationSection } from "./sections/AuditConfigurationSection";
import { BasisAndDisplaySection } from "./sections/BasisAndDisplaySection";

interface SettingsProfileTabProps {
  activeSectionId: string | null;
  auditConfigurationRef: RefObject<HTMLDivElement | null>;
  data: SettingsPageData;
  isAuditConfigurationFocused: boolean;
  onJumpToSection: (sectionId: string) => void;
  scope: "global" | "instance";
}

export function SettingsProfileTab({
  activeSectionId,
  auditConfigurationRef,
  data,
  isAuditConfigurationFocused,
  onJumpToSection,
  scope,
}: SettingsProfileTabProps) {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex min-w-0 flex-col gap-4 @3xl/main:grid @3xl/main:grid-cols-[220px_minmax(0,1fr)] @3xl/main:gap-6">
        <aside className="lg:sticky lg:top-4 lg:h-fit">
          <SettingsSectionsNav
            activeSectionId={activeSectionId ?? ""}
            onJumpToSection={onJumpToSection}
            scope={scope}
          />
        </aside>

        <div className="flex flex-col gap-5">
          <BasisAndDisplaySection
            costingDirty={data.costingDirty}
            renderSectionSaveState={data.renderSaveStateForSection}
            costingUnavailable={data.costingUnavailable}
            costingLoading={data.costingLoading}
            costingForm={data.costingForm}
            setCostingForm={data.setCostingForm}
            normalizedCurrentCosting={data.normalizedCurrentCosting}
            onCurrencyMigrated={data.handleCurrencyMigrationCommitted}
            timezonePreviewText={data.timezonePreviewText}
            timezonePreviewZone={data.timezonePreviewZone}
            timezonePreviewOffset={data.timezonePreviewOffset}
          />

          <AuditConfigurationSection
            auditConfigurationRef={auditConfigurationRef}
            isAuditConfigurationFocused={isAuditConfigurationFocused}
            apiFamilyAuditSettings={data.apiFamilyAuditSettings}
            apiFamilyAuditSettingsDirty={data.apiFamilyAuditSettingsDirty}
            loadingAPIFamilyAuditSettings={data.loadingAPIFamilyAuditSettings}
            savingAPIFamilyAuditSettings={data.savingAPIFamilyAuditSettings}
            renderSectionSaveState={data.renderSaveStateForSection}
            setAPIFamilyAuditCaptureBodies={data.setAPIFamilyAuditCaptureBodies}
            setAPIFamilyAuditEnabled={data.setAPIFamilyAuditEnabled}
            auditStorageSummary={data.auditStorageSummary}
            auditStorageLoading={data.auditStorageLoading}
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
