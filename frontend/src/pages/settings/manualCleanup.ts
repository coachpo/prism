import { getStaticMessages } from "@/i18n/staticMessages";

export type CleanupType = "" | "requests" | "audits" | "loadbalance_events" | "statistics";
export type DeleteCleanupType = Exclude<CleanupType, "">;
export type RetentionPreset = "" | "1" | "7" | "30" | "90" | "all";

export function getCleanupTypeLabel(type: DeleteCleanupType): string {
  const messages = getStaticMessages();

  switch (type) {
    case "requests":
      return messages.settingsDialogs.cleanupTypeRequests;
    case "audits":
      return messages.settingsDialogs.cleanupTypeAudits;
    case "loadbalance_events":
      return messages.settingsDialogs.cleanupTypeLoadbalanceEvents;
    case "statistics":
      return messages.settingsDialogs.cleanupTypeStatistics;
  }
}

export function getCleanupRetentionLabel(deleteAll: boolean, days: number | null): string {
  const messages = getStaticMessages();

  if (deleteAll) {
    return messages.settingsDialogs.allData;
  }

  return messages.settingsDialogs.olderThanDays(days);
}
