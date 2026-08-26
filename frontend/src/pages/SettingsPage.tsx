import { useCallback, useEffect, useRef, useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import {
  OperatorFreshnessBar,
  OperatorMissingValue,
  OperatorPageHeader,
  OperatorStalenessBadge,
} from "@/shared/design-system";
import { DeleteConfirmDialog } from "./settings/dialogs/DeleteConfirmDialog";
import { RuleDialog } from "./settings/dialogs/RuleDialog";
import { DeleteRuleConfirmDialog } from "./settings/dialogs/DeleteRuleConfirmDialog";
import { DeleteUserAgentClientRuleConfirmDialog } from "./settings/dialogs/DeleteUserAgentClientRuleConfirmDialog";
import { RetentionPolicyPreflightDialog } from "./settings/dialogs/RetentionPolicyPreflightDialog";
import { UserAgentClientRuleDialog } from "./settings/dialogs/UserAgentClientRuleDialog";
import { SettingsProfileTab } from "./settings/SettingsProfileTab";
import { SettingsGlobalTab } from "./settings/SettingsGlobalTab";
import { SettingsSaveAction, type SettingsPendingSave } from "./settings/SettingsSaveAction";
import { useSettingsPageData } from "./settings/useSettingsPageData";
import { useSettingsPageSectionState } from "./settings/useSettingsPageSectionState";
import { SETTINGS_SCOPES } from "./settings/settingsNavigation";

export function SettingsPage() {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const auditConfigurationRef = useRef<HTMLDivElement | null>(null);
  const {
    scope,
    setScope,
    activeSectionId,
    setActiveSectionId,
    jumpToSection,
  } = useSettingsPageSectionState();
  const data = useSettingsPageData(scope);
  const [retentionHasInvalidValue, setRetentionHasInvalidValueState] = useState(false);
  const setRetentionHasInvalidValue = useCallback((hasInvalid: boolean) => {
    setRetentionHasInvalidValueState(hasInvalid);
  }, []);
  const isAuditConfigurationFocused = scope === SETTINGS_SCOPES.global && activeSectionId === "audit-privacy";

  useEffect(() => {
    if (!activeSectionId) return;
    const frame = window.requestAnimationFrame(() => {
      const target = document.getElementById(activeSectionId);
      if (!target) return;
      target.scrollIntoView({ behavior: "smooth", block: "start" });
      target.focus({ preventScroll: true });
    });
    return () => window.cancelAnimationFrame(frame);
  }, [activeSectionId, scope]);

  const handleJumpToSection = (sectionId: string) => {
    const target = document.getElementById(sectionId);
    if (!target) {
      return;
    }

    setActiveSectionId(sectionId);
    jumpToSection(sectionId);
    target.scrollIntoView({ behavior: "smooth", block: "start" });
    window.setTimeout(() => target.focus({ preventScroll: true }), 0);
  };

  // The header owns the page's one save action. Which cards are saveable
  // depends on the active scope, so the pending list is built per scope.
  const pendingSaves: SettingsPendingSave[] = scope === SETTINGS_SCOPES.global
    ? [
        ...(data.costingDirty
          ? [{ label: messages.settingsPage.basisAndDisplay, save: data.handleSaveCostingSettings }]
          : []),
        ...(data.apiFamilyAuditSettingsDirty
          ? [{ label: messages.settingsPage.auditAndPrivacy, save: data.handleSaveAPIFamilyAuditSettings }]
          : []),
      ]
    : data.retentionSettingsDirty
      ? [{ label: messages.settingsPage.retentionPolicy, save: data.handleSaveRetentionSettings }]
      : [];

  const saving = scope === SETTINGS_SCOPES.global
    ? data.costingSaving || data.savingAPIFamilyAuditSettings
    : data.retentionSettingsSaving;

  const saveBlockedReason = scope === SETTINGS_SCOPES.global
    ? data.costingDirty && data.costingUnavailable
      ? messages.settingsPage.saveBlockedUnavailable
      : data.costingDirty && data.costingLoading
        ? messages.settingsPage.saveBlockedLoading
        : data.costingDirty
          ? data.costingValidationError
          : null
    : retentionHasInvalidValue
      ? messages.settingsPage.saveBlockedInvalidRetention
      : data.retentionSettingsDirty && data.retentionSettingsLoading
        ? messages.settingsPage.saveBlockedLoading
        : data.retentionSettingsDirty && data.retentionSettings === null
          ? messages.settingsPage.saveBlockedUnavailable
          : data.retentionSettingsDirty && (data.policyPreflightLoading || data.manualPreflightLoading)
            ? messages.settingsPage.saveBlockedLoading
            : null;

  // One freshness bar for the page. Both scopes have a read that can go stale
  // — the audit storage snapshot and the retention job list — and they now
  // report it through the same badge instead of two local notices.
  const isGlobal = scope === SETTINGS_SCOPES.global;
  const loadedAt = isGlobal ? data.auditStorageSummary?.generated_at ?? null : data.jobsLoadedAt;
  const staleBadge = isGlobal
    ? data.auditStorageFailed
      ? (
        <OperatorStalenessBadge
          label={messages.settingsAudit.storageSummaryStaleBadge}
          reason={messages.settingsAudit.storageSummaryStaleReason}
        />
      )
      : null
    : data.jobsStale
      ? (
        <OperatorStalenessBadge
          label={messages.settingsRetentionDeletion.jobsStaleBadge}
          reason={messages.settingsRetentionDeletion.jobsStaleReason}
        />
      )
      : null;

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)] pb-8">
      <OperatorPageHeader
        title={messages.settingsPage.settingsTitle}
        description={messages.settingsPage.settingsDescription}
        actions={
          <SettingsSaveAction
            blockedReason={saveBlockedReason}
            pending={pendingSaves}
            saving={saving}
          />
        }
      />

      <OperatorFreshnessBar
        updatedAt={
          loadedAt ? (
            messages.freshness.updatedAt(formatTime(loadedAt))
          ) : (
            <OperatorMissingValue reason={messages.freshness.neverLoaded} />
          )
        }
        basis={isGlobal ? messages.settingsAudit.storageSummaryBasis : messages.settingsRetentionDeletion.retentionJobsDescription}
        refresh={{
          label: messages.freshness.refresh,
          onRefresh: () => {
            if (isGlobal) {
              void data.refreshAuditStorage();
              return;
            }
            data.refreshJobs();
          },
          pending: isGlobal ? data.auditStorageLoading : data.jobsLoading,
        }}
        badges={staleBadge}
      />

      <Tabs
        value={scope}
        onValueChange={(value) => setScope(value as typeof scope)}
        className="flex flex-col gap-4"
      >
        <TabsList className="w-full justify-start sm:w-fit">
          <TabsTrigger value={SETTINGS_SCOPES.global}>{messages.settingsPage.globalTab}</TabsTrigger>
          <TabsTrigger value={SETTINGS_SCOPES.instance}>{messages.settingsPage.instanceTab}</TabsTrigger>
        </TabsList>

        <TabsContent className="mt-0" value={SETTINGS_SCOPES.global}>
          <SettingsProfileTab
            activeSectionId={activeSectionId}
            auditConfigurationRef={auditConfigurationRef}
            data={data}
            isAuditConfigurationFocused={isAuditConfigurationFocused}
            onJumpToSection={handleJumpToSection}
            scope={scope}
          />
        </TabsContent>

        <TabsContent className="mt-0" value={SETTINGS_SCOPES.instance}>
          <SettingsGlobalTab
            activeSectionId={activeSectionId}
            data={data}
            onJumpToSection={handleJumpToSection}
            onRetentionInvalidChange={setRetentionHasInvalidValue}
          />
        </TabsContent>
      </Tabs>

      <DeleteConfirmDialog
        deleteConfirm={data.deleteConfirm}
        displayedDeleteConfirm={data.displayedDeleteConfirm}
        open={data.deleteConfirmDialogOpen}
        setDeleteConfirm={data.setDeleteConfirm}
        deleteConfirmPhrase={data.deleteConfirmPhrase}
        setDeleteConfirmPhrase={data.setDeleteConfirmPhrase}
        handleBatchDelete={data.handleBatchDelete}
        deleting={data.deleting}
		isDeletePhraseValid={data.isDeletePhraseValid}
		preflightSemanticsComplete={data.isManualPreflightSemanticallyComplete}
		preflight={data.manualPreflight}
      />

      <RuleDialog
        ruleDialogOpen={data.ruleDialogOpen}
        setRuleDialogOpen={data.setRuleDialogOpen}
        editingRule={data.editingRule}
        ruleForm={data.ruleForm}
        setRuleForm={data.setRuleForm}
        handleSaveRule={data.handleSaveRule}
      />

      <UserAgentClientRuleDialog
        ruleDialogOpen={data.userAgentClientRuleDialogOpen}
        setRuleDialogOpen={data.setUserAgentClientRuleDialogOpen}
        editingRule={data.editingUserAgentClientRule}
        ruleForm={data.userAgentClientRuleForm}
        setRuleForm={data.setUserAgentClientRuleForm}
        handleSaveRule={data.handleSaveUserAgentClientRule}
      />

      <DeleteRuleConfirmDialog
        deleteRuleConfirm={data.deleteRuleConfirm}
        displayedDeleteRuleConfirm={data.displayedDeleteRuleConfirm}
        open={data.deleteRuleDialogOpen}
        setDeleteRuleConfirm={data.setDeleteRuleConfirm}
        handleDeleteRule={data.handleDeleteRule}
      />

      <RetentionPolicyPreflightDialog
        open={data.policyConfirmOpen}
        onOpenChange={data.setPolicyConfirmOpen}
        preflight={data.policyPreflight}
        phrase={data.policyConfirmationPhrase}
        setPhrase={data.setPolicyConfirmationPhrase}
		valid={data.isPolicyPreflightValid}
		preflightSemanticsComplete={data.isPolicyPreflightSemanticallyComplete}
        submitting={data.retentionSettingsSaving}
        onSubmit={data.submitRetentionSettings}
      />

      <DeleteUserAgentClientRuleConfirmDialog
        deleteRuleConfirm={data.deleteUserAgentClientRuleConfirm}
        displayedDeleteRuleConfirm={data.displayedDeleteUserAgentClientRuleConfirm}
        open={data.deleteUserAgentClientRuleDialogOpen}
        setDeleteRuleConfirm={data.setDeleteUserAgentClientRuleConfirm}
        handleDeleteRule={data.handleDeleteUserAgentClientRule}
      />
    </div>
  );
}
