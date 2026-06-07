import type { ReactNode } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import { StatusBadge } from "@/components/StatusBadge";
import type { SettingsSaveSection } from "./settingsSaveTypes";

interface SectionSaveStateProps {
  section: SettingsSaveSection;
  isDirty: boolean;
  recentlySavedSection: SettingsSaveSection | null;
}

export function renderSectionSaveState({
  section,
  isDirty,
  recentlySavedSection,
}: SectionSaveStateProps): ReactNode {
  const messages = getStaticMessages();

  if (isDirty) {
    return <StatusBadge label={messages.settingsSaveState.unsavedChanges} intent="warning" />;
  }

  if (recentlySavedSection === section) {
    return <StatusBadge label={messages.settingsSaveState.saved} intent="success" />;
  }

  return null;
}
