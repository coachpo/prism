import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { SETTINGS_SECTIONS } from "./settingsPageHelpers";

interface SettingsSectionsNavProps {
  activeSectionId: string;
  onJumpToSection: (sectionId: string) => void;
}

export function SettingsSectionsNav({
  activeSectionId,
  onJumpToSection,
}: SettingsSectionsNavProps) {
  const { messages } = useLocale();
  const labels: Record<string, string> = {
    backup: messages.settingsPage.backup,
    "billing-currency": messages.settingsPage.billingCurrency,
    timezone: messages.settingsPage.timezone,
    "audit-configuration": messages.settingsPage.auditPrivacy,
    "retention-deletion": messages.settingsPage.retentionDeletion,
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">{messages.settingsPage.sectionsTitle}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {SETTINGS_SECTIONS.map((section) => (
          <Button
            key={section.id}
            type="button"
            variant={activeSectionId === section.id ? "secondary" : "ghost"}
            className="w-full justify-start"
            onClick={() => onJumpToSection(section.id)}
          >
            {labels[section.id]}
          </Button>
        ))}
      </CardContent>
    </Card>
  );
}
