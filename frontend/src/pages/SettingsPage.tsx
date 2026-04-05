import { useRef } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PageHeader } from "@/components/PageHeader";
import { useLocale } from "@/i18n/useLocale";
import { DeleteConfirmDialog } from "./settings/dialogs/DeleteConfirmDialog";
import { RuleDialog } from "./settings/dialogs/RuleDialog";
import { DeleteRuleConfirmDialog } from "./settings/dialogs/DeleteRuleConfirmDialog";
import { SettingsProfileTab } from "./settings/SettingsProfileTab";
import { SettingsGlobalTab } from "./settings/SettingsGlobalTab";
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
  const data = useSettingsPageData();

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
    <div className="space-y-6">
      <PageHeader
        title={messages.settingsPage.settingsTitle}
        description={messages.settingsPage.settingsDescription}
      />

      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as typeof activeTab)}
      >
        <TabsList>
          <TabsTrigger value={SETTINGS_TABS.profile}>{messages.settingsPage.profileTab}</TabsTrigger>
          <TabsTrigger value={SETTINGS_TABS.global}>{messages.settingsPage.globalTab}</TabsTrigger>
        </TabsList>

        <TabsContent value={SETTINGS_TABS.profile}>
          <SettingsProfileTab
            activeSectionId={activeSectionId}
            auditConfigurationRef={auditConfigurationRef}
            data={data}
            isAuditConfigurationFocused={isAuditConfigurationFocused}
            onJumpToSection={handleJumpToSection}
          />
        </TabsContent>

        <TabsContent value={SETTINGS_TABS.global}>
          <SettingsGlobalTab data={data} />
        </TabsContent>
      </Tabs>

      <DeleteConfirmDialog
        deleteConfirm={data.deleteConfirm}
        displayedDeleteConfirm={data.displayedDeleteConfirm}
        open={data.deleteConfirmDialogOpen}
        setDeleteConfirm={data.setDeleteConfirm}
        selectedProfileLabel={data.selectedProfileLabel}
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

      <DeleteRuleConfirmDialog
        deleteRuleConfirm={data.deleteRuleConfirm}
        displayedDeleteRuleConfirm={data.displayedDeleteRuleConfirm}
        open={data.deleteRuleDialogOpen}
        setDeleteRuleConfirm={data.setDeleteRuleConfirm}
        handleDeleteRule={data.handleDeleteRule}
      />
    </div>
  );
}
