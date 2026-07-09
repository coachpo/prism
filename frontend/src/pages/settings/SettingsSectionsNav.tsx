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
    "billing-currency": messages.settingsPage.billingCurrency,
    timezone: messages.settingsPage.timezone,
    "audit-configuration": messages.settingsPage.auditPrivacy,
    "retention-deletion": messages.settingsPage.retentionDeletion,
  };

  return (
    <Card className="operator-section-surface sticky top-20">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">{messages.settingsPage.sectionsTitle}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1">
        {SETTINGS_SECTIONS.map((section) => (
          <Button
            key={section.id}
            type="button"
            variant={activeSectionId === section.id ? "secondary" : "ghost"}
            className="h-8 w-full justify-start px-2.5 text-sm"
            onClick={() => onJumpToSection(section.id)}
          >
            {labels[section.id]}
          </Button>
        ))}
      </CardContent>
    </Card>
  );
}
