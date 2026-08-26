import type { SettingsSaveSection } from "./settingsSaveTypes";
import { useAPIFamilyAuditSettings } from "./useAPIFamilyAuditSettings";
import { useHeaderBlocklistRules } from "./useHeaderBlocklistRules";
import { useUserAgentClientRules } from "./useUserAgentClientRules";

interface UseAuditConfigurationDataInput {
  enabled: boolean;
  revision: number;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
}

/**
 * Settings page coordinator: each child owns one backend resource and its
 * lifecycle; this boundary only assembles the props consumed by the audit card.
 */
export function useAuditConfigurationData({
  enabled,
  revision,
  setRecentlySavedSection,
}: UseAuditConfigurationDataInput) {
  const apiFamilyAudit = useAPIFamilyAuditSettings({
    enabled,
    revision,
    setRecentlySavedSection,
  });
  const headerBlocklist = useHeaderBlocklistRules({ enabled, revision });
  const userAgentClient = useUserAgentClientRules({ enabled, revision });

  return {
    ...apiFamilyAudit,
    ...headerBlocklist,
    ...userAgentClient,
  };
}
