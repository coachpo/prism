import type { ReactNode } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import { OperatorStatusBadge } from "@/shared/design-system";
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
    return <OperatorStatusBadge label={messages.settingsSaveState.unsavedChanges} intent="degraded" />;
  }

  if (recentlySavedSection === section) {
    return <OperatorStatusBadge label={messages.settingsSaveState.saved} intent="healthy" />;
  }

  return null;
}
