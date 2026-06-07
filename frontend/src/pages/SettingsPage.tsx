import { useRef } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/PageHeader";
import { useLocale } from "@/i18n/useLocale";
import { DeleteConfirmDialog } from "./settings/dialogs/DeleteConfirmDialog";
import { RuleDialog } from "./settings/dialogs/RuleDialog";
import { DeleteRuleConfirmDialog } from "./settings/dialogs/DeleteRuleConfirmDialog";
import { DeleteUserAgentClientRuleConfirmDialog } from "./settings/dialogs/DeleteUserAgentClientRuleConfirmDialog";
import { UserAgentClientRuleDialog } from "./settings/dialogs/UserAgentClientRuleDialog";
import { SettingsProfileTab } from "./settings/SettingsProfileTab";
import { SettingsGlobalTab } from "./settings/SettingsGlobalTab";
import { SettingsStartupTab } from "./settings/SettingsStartupTab";
import { useSettingsPageData } from "./settings/useSettingsPageData";
import { useSettingsPageSectionState } from "./settings/useSettingsPageSectionState";
import { SETTINGS_TABS } from "./settings/settingsPageHelpers";

export function SettingsPage() {
  const { messages } = useLocale();
  const auditConfigurationRef = useRef<HTMLDivElement | null>(null);
  const {
    activeTab,
    setActiveTab,
    activeSectionId,
    setActiveSectionId,
    isAuditConfigurationFocused,
    jumpToSection,
  } = useSettingsPageSectionState();
  const data = useSettingsPageData(activeTab);

  const handleJumpToSection = (sectionId: string) => {
    const target = document.getElementById(sectionId);
    if (!target) {
      return;
    }

    setActiveSectionId(sectionId);
    jumpToSection(sectionId);
    target.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)] pb-8">
      <PageHeader
        title={messages.settingsPage.settingsTitle}
        description={messages.settingsPage.settingsDescription}
      />

      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as typeof activeTab)}
        className="flex flex-col gap-4"
      >
        <TabsList className="w-full justify-start overflow-x-auto rounded-xl bg-muted/70 p-1 sm:w-fit">
          <TabsTrigger className="h-8" value={SETTINGS_TABS.profile}>{messages.settingsPage.profileTab}</TabsTrigger>
          <TabsTrigger className="h-8" value={SETTINGS_TABS.global}>{messages.settingsPage.globalTab}</TabsTrigger>
          <TabsTrigger className="h-8" value={SETTINGS_TABS.startup}>{messages.settingsPage.startupTab}</TabsTrigger>
        </TabsList>

        <TabsContent className="mt-0" value={SETTINGS_TABS.profile}>
          <SettingsProfileTab
            activeSectionId={activeSectionId}
            auditConfigurationRef={auditConfigurationRef}
            data={data}
            isAuditConfigurationFocused={isAuditConfigurationFocused}
            onJumpToSection={handleJumpToSection}
          />
        </TabsContent>

        <TabsContent className="mt-0" value={SETTINGS_TABS.global}>
          <SettingsGlobalTab data={data} />
        </TabsContent>

        <TabsContent className="mt-0" value={SETTINGS_TABS.startup}>
          <SettingsStartupTab />
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
