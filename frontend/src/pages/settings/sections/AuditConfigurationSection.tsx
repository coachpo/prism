import type { ReactNode, RefObject } from "react";
import { cn } from "@/lib/utils";
import type { ApiFamily, AuditAPIFamilySetting, HeaderBlocklistRule, UserAgentClientRule } from "@/lib/types";
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
  handleSaveAPIFamilyAuditSettings: () => Promise<void>;
  setAPIFamilyAuditCaptureBodies: (apiFamily: ApiFamily, checked: boolean) => void;
  setAPIFamilyAuditEnabled: (apiFamily: ApiFamily, checked: boolean) => void;
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
  handleSaveAPIFamilyAuditSettings,
  setAPIFamilyAuditCaptureBodies,
  setAPIFamilyAuditEnabled,
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
  return (
    <section id="audit-configuration" tabIndex={-1} className="scroll-mt-24 flex flex-col gap-4">
      <AuditConfigurationAPIFamilyCard
        cardRef={auditConfigurationRef}
        className={cn(
          "transition-all duration-300",
          isAuditConfigurationFocused && "ring-2 ring-primary/50 bg-primary/5"
        )}
        apiFamilyAuditSettings={apiFamilyAuditSettings}
        apiFamilyAuditSettingsDirty={apiFamilyAuditSettingsDirty}
        handleSaveAPIFamilyAuditSettings={handleSaveAPIFamilyAuditSettings}
        loadingAPIFamilyAuditSettings={loadingAPIFamilyAuditSettings}
        renderSectionSaveState={renderSectionSaveState}
        savingAPIFamilyAuditSettings={savingAPIFamilyAuditSettings}
        setAPIFamilyAuditCaptureBodies={setAPIFamilyAuditCaptureBodies}
        setAPIFamilyAuditEnabled={setAPIFamilyAuditEnabled}
      />

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
  );
}
