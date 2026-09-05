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

// 分区上方的表格是异步读出来的：清理作业表一落地，先前算好的锚点就整体下移
// （实测 manual-cleanup / retention-jobs 落到折下 900~1140px）。滚动跟到布局
// 不再变化为止，其间操作者一动手就立刻让位，超时后也停。
const SECTION_SCROLL_SETTLE_MS = 120;
const SECTION_SCROLL_DEADLINE_MS = 2500;
const SECTION_SCROLL_YIELD_EVENTS = ["wheel", "touchmove", "keydown", "pointerdown"] as const;

function alignSectionIntoView(sectionId: string): () => void {
  let settleTimer = 0;
  let deadlineTimer = 0;
  let observer: ResizeObserver | null = null;

  const stop = () => {
    window.clearTimeout(settleTimer);
    window.clearTimeout(deadlineTimer);
    observer?.disconnect();
    observer = null;
    for (const type of SECTION_SCROLL_YIELD_EVENTS) {
      window.removeEventListener(type, stop);
    }
  };

  const align = () => {
    const target = document.getElementById(sectionId);
    if (!target) return;
    target.scrollIntoView({ behavior: "smooth", block: "start" });
    target.focus({ preventScroll: true });
  };

  align();
  if (typeof ResizeObserver !== "undefined") {
    observer = new ResizeObserver(() => {
      window.clearTimeout(settleTimer);
      settleTimer = window.setTimeout(align, SECTION_SCROLL_SETTLE_MS);
    });
    observer.observe(document.body);
  }
  deadlineTimer = window.setTimeout(stop, SECTION_SCROLL_DEADLINE_MS);
  for (const type of SECTION_SCROLL_YIELD_EVENTS) {
    window.addEventListener(type, stop, { passive: true });
  }
  return stop;
}

export function SettingsPage() {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const auditConfigurationRef = useRef<HTMLDivElement | null>(null);
  const sectionScrollStop = useRef<(() => void) | null>(null);
  const {
    scope,
    setScope,
    activeSectionId,
    explicitSection,
    setActiveSectionId,
    jumpToSection,
  } = useSettingsPageSectionState();
  const data = useSettingsPageData(scope);
  const [retentionHasInvalidValue, setRetentionHasInvalidValueState] = useState(false);
  const setRetentionHasInvalidValue = useCallback((hasInvalid: boolean) => {
    setRetentionHasInvalidValueState(hasInvalid);
  }, []);
  const isAuditConfigurationFocused = scope === SETTINGS_SCOPES.global && activeSectionId === "audit-privacy";

  // 只有 URL 里显式带了 section 才滚。没带时 activeSectionId 也会回落到默认分区，
  // 无条件滚动会让页头、h1 与这一族唯一的新鲜度条一落地就在视口之外。
  useEffect(() => {
    if (!activeSectionId || !explicitSection) return;
    sectionScrollStop.current?.();
    const stop = alignSectionIntoView(activeSectionId);
    sectionScrollStop.current = stop;
    return () => {
      stop();
      if (sectionScrollStop.current === stop) sectionScrollStop.current = null;
    };
  }, [activeSectionId, explicitSection, scope]);

  const handleJumpToSection = (sectionId: string) => {
    if (!document.getElementById(sectionId)) {
      return;
    }

    setActiveSectionId(sectionId);
    jumpToSection(sectionId);
    sectionScrollStop.current?.();
    sectionScrollStop.current = alignSectionIntoView(sectionId);
  };

  // The header owns the page's one save action. Which cards are saveable
  // depends on the active scope, so the pending list is built per scope.
  const pendingSaves: SettingsPendingSave[] = scope === SETTINGS_SCOPES.global
    ? [
        ...(data.costingDirty
          ? [{ label: messages.settingsPage.basisAndDisplay, save: data.handleSaveCostingSettings }]
          : []),
        // 待保存项用卡片自己的标题，一个分区一个名字。
        ...(data.apiFamilyAuditSettingsDirty
          ? [{ label: messages.settingsAudit.apiFamilyAuditControls, save: data.handleSaveAPIFamilyAuditSettings }]
          : []),
      ]
    : data.retentionSettingsDirty
      ? [{ label: messages.settingsRetentionDeletion.retentionPolicyTitle, save: data.handleSaveRetentionSettings }]
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
        basis={isGlobal ? messages.settingsAudit.storageSummaryBasis : messages.settingsRetentionDeletion.retentionJobsBasis}
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
