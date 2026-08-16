import type { ReactNode, RefObject } from "react";
import { cn } from "@/lib/utils";
import type { ApiFamily, AuditAPIFamilySetting, AuditStorageSummary, HeaderBlocklistRule, UserAgentClientRule } from "@/lib/types";
import { AuditConfigurationAPIFamilyCard } from "./AuditConfigurationAPIFamilyCard";
import { AuditConfigurationHeaderBlocklistCard } from "./AuditConfigurationHeaderBlocklistCard";
import { AuditConfigurationUserAgentClientRulesCard } from "./AuditConfigurationUserAgentClientRulesCard";

interface AuditConfigurationSectionProps {
  auditConfigurationRef: RefObject<HTMLDivElement | null>;
  isAuditConfigurationFocused: boolean;
  apiFamilyAuditSettings: AuditAPIFamilySetting[];
  apiFamilyAuditSettingsDirty: boolean;
  loadingAPIFamilyAuditSettings: boolean;
  savingAPIFamilyAuditSettings: boolean;
  renderSectionSaveState: (section: "audit", isDirty: boolean) => ReactNode;
  setAPIFamilyAuditCaptureBodies: (apiFamily: ApiFamily, checked: boolean) => void;
  setAPIFamilyAuditEnabled: (apiFamily: ApiFamily, checked: boolean) => void;
  auditStorageSummary: AuditStorageSummary | null;
  auditStorageLoading: boolean;
  loadingRules: boolean;
  systemRulesOpen: boolean;
  setSystemRulesOpen: (open: boolean) => void;
  systemRules: HeaderBlocklistRule[];
  userRulesOpen: boolean;
  setUserRulesOpen: (open: boolean) => void;
  customRules: HeaderBlocklistRule[];
  handleToggleRule: (rule: HeaderBlocklistRule, checked: boolean) => Promise<void>;
  openAddRuleDialog: () => void;
  openEditRuleDialog: (rule: HeaderBlocklistRule) => void;
  setDeleteRuleConfirm: (rule: HeaderBlocklistRule | null) => void;
  loadingUserAgentClientRules: boolean;
  userAgentClientSystemRulesOpen: boolean;
  setUserAgentClientSystemRulesOpen: (open: boolean) => void;
  userAgentClientSystemRules: UserAgentClientRule[];
  userAgentClientUserRulesOpen: boolean;
  setUserAgentClientUserRulesOpen: (open: boolean) => void;
  userAgentClientCustomRules: UserAgentClientRule[];
  handleToggleUserAgentClientRule: (
    rule: UserAgentClientRule,
    checked: boolean,
  ) => Promise<void>;
  openAddUserAgentClientRuleDialog: () => void;
  openEditUserAgentClientRuleDialog: (rule: UserAgentClientRule) => void;
  setDeleteUserAgentClientRuleConfirm: (rule: UserAgentClientRule | null) => void;
}

export function AuditConfigurationSection({
  auditConfigurationRef,
  isAuditConfigurationFocused,
  apiFamilyAuditSettings,
  apiFamilyAuditSettingsDirty,
  loadingAPIFamilyAuditSettings,
  savingAPIFamilyAuditSettings,
  renderSectionSaveState,
  setAPIFamilyAuditCaptureBodies,
  setAPIFamilyAuditEnabled,
  auditStorageSummary,
  auditStorageLoading,
  loadingRules,
  systemRulesOpen,
  setSystemRulesOpen,
  systemRules,
  userRulesOpen,
  setUserRulesOpen,
  customRules,
  handleToggleRule,
  openAddRuleDialog,
  openEditRuleDialog,
  setDeleteRuleConfirm,
  loadingUserAgentClientRules,
  userAgentClientSystemRulesOpen,
  setUserAgentClientSystemRulesOpen,
  userAgentClientSystemRules,
  userAgentClientUserRulesOpen,
  setUserAgentClientUserRulesOpen,
  userAgentClientCustomRules,
  handleToggleUserAgentClientRule,
  openAddUserAgentClientRuleDialog,
  openEditUserAgentClientRuleDialog,
  setDeleteUserAgentClientRuleConfirm,
}: AuditConfigurationSectionProps) {
  // Header blocklist and client identification are peers of the API-family
  // card, not anchors buried inside it: the directory lists them as siblings,
  // so the page must show them as siblings too.
  return (
    <>
      <section id="audit-privacy" tabIndex={-1} className="scroll-mt-24">
        <AuditConfigurationAPIFamilyCard
          cardRef={auditConfigurationRef}
          className={cn(
            "transition-all duration-300",
            isAuditConfigurationFocused && "ring-2 ring-primary/50 bg-primary/5"
          )}
          apiFamilyAuditSettings={apiFamilyAuditSettings}
          apiFamilyAuditSettingsDirty={apiFamilyAuditSettingsDirty}
          loadingAPIFamilyAuditSettings={loadingAPIFamilyAuditSettings}
          renderSectionSaveState={renderSectionSaveState}
          savingAPIFamilyAuditSettings={savingAPIFamilyAuditSettings}
          setAPIFamilyAuditCaptureBodies={setAPIFamilyAuditCaptureBodies}
          setAPIFamilyAuditEnabled={setAPIFamilyAuditEnabled}
          auditStorageSummary={auditStorageSummary}
          auditStorageLoading={auditStorageLoading}
        />
      </section>

      <section id="header-blocklist" tabIndex={-1} className="scroll-mt-24">
        <AuditConfigurationHeaderBlocklistCard
          customRules={customRules}
          handleToggleRule={handleToggleRule}
          loadingRules={loadingRules}
          openAddRuleDialog={openAddRuleDialog}
          openEditRuleDialog={openEditRuleDialog}
          setDeleteRuleConfirm={setDeleteRuleConfirm}
          setSystemRulesOpen={setSystemRulesOpen}
          setUserRulesOpen={setUserRulesOpen}
          systemRules={systemRules}
          systemRulesOpen={systemRulesOpen}
          userRulesOpen={userRulesOpen}
        />
      </section>

      <section id="client-rules" tabIndex={-1} className="scroll-mt-24">
        <AuditConfigurationUserAgentClientRulesCard
          customRules={userAgentClientCustomRules}
          handleToggleRule={handleToggleUserAgentClientRule}
          loadingRules={loadingUserAgentClientRules}
          openAddRuleDialog={openAddUserAgentClientRuleDialog}
          openEditRuleDialog={openEditUserAgentClientRuleDialog}
          setDeleteRuleConfirm={setDeleteUserAgentClientRuleConfirm}
          setSystemRulesOpen={setUserAgentClientSystemRulesOpen}
          setUserRulesOpen={setUserAgentClientUserRulesOpen}
          systemRules={userAgentClientSystemRules}
          systemRulesOpen={userAgentClientSystemRulesOpen}
          userRulesOpen={userAgentClientUserRulesOpen}
        />
      </section>
    </>
  );
}
