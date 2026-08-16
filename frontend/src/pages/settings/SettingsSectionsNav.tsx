import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { GLOBAL_NAV_SECTIONS, INSTANCE_NAV_SECTIONS, type SettingsScope } from "./settingsPageHelpers";

interface SettingsSectionsNavProps {
  activeSectionId: string;
  onJumpToSection: (sectionId: string) => void;
  scope: SettingsScope;
}

export function SettingsSectionsNav({
  activeSectionId,
  onJumpToSection,
  scope,
}: SettingsSectionsNavProps) {
  const { messages } = useLocale();
  // Each label matches its card header word for word: a directory entry that
  // renames the card it jumps to is a navigation bug, not a nicety.
  const labels: Record<string, string> = {
    "billing-currency": messages.settingsPage.basisAndDisplay,
    "audit-privacy": messages.settingsAudit.apiFamilyAuditControls,
    "header-blocklist": messages.settingsAudit.headerBlocklist,
    "client-rules": messages.settingsAudit.userAgentClientRules,
    authentication: messages.settingsAuthentication.authentication,
    retention: messages.settingsRetentionDeletion.retentionPolicyTitle,
    "manual-cleanup": messages.settingsRetentionDeletion.manualCleanupTitle,
    "retention-jobs": messages.settingsRetentionDeletion.retentionJobsTitle,
  };
  const sections = scope === "global" ? GLOBAL_NAV_SECTIONS : INSTANCE_NAV_SECTIONS;

  return (
    <Card className="operator-section-surface sticky top-20">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{messages.settingsPage.sectionsTitle}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1">
        {sections.map((section) => (
          <Button
            key={section.id}
            type="button"
            variant={activeSectionId === section.id ? "secondary" : "ghost"}
            className="h-8 w-full justify-start px-2.5 text-sm"
            aria-current={activeSectionId === section.id ? "page" : undefined}
            onClick={() => onJumpToSection(section.id)}
          >
            {labels[section.id]}
          </Button>
        ))}
      </CardContent>
    </Card>
  );
}
