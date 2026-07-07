import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/context/useAuth";
import { useProfileContext } from "@/context/ProfileContext";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { useLocale } from "@/i18n/useLocale";
import { renderSectionSaveState } from "./sectionSaveState";
import type { SettingsSaveSection } from "./settingsSaveTypes";
import { SETTINGS_TABS, type SettingsTab } from "./settingsPageHelpers";
import { useAuditConfigurationData } from "./useAuditConfigurationData";
import { useAuthenticationSettingsData } from "./useAuthenticationSettingsData";
import { useCostingSettingsData } from "./useCostingSettingsData";
import { useRetentionDeletionData } from "./useRetentionDeletionData";

export function useSettingsPageData(activeTab: SettingsTab) {
  const navigate = useNavigate();
  const { messages } = useLocale();
  const { refreshAuth } = useAuth();
  const { selectedProfile, revision, bumpRevision } = useProfileContext();
  const { prime: primeReportingCurrency } = useReportingCurrencyContext();
  const selectedProfileLabel = selectedProfile
    ? `${selectedProfile.name} (#${selectedProfile.id})`
    : messages.settingsPage.selectedProfileFallback;

  const [recentlySavedSection, setRecentlySavedSection] = useState<SettingsSaveSection | null>(null);

  const isProfileTabActive = activeTab === SETTINGS_TABS.profile;
  const isGlobalTabActive = activeTab === SETTINGS_TABS.global;
  const auth = useAuthenticationSettingsData({
    enabled: isGlobalTabActive,
    navigate,
    refreshAuth,
    revision,
  });
  const costing = useCostingSettingsData({
    bumpRevision,
    enabled: isProfileTabActive,
    primeReportingCurrency,
    revision,
    setRecentlySavedSection,
  });
  const audit = useAuditConfigurationData({
    enabled: isProfileTabActive,
    revision,
    setRecentlySavedSection,
  });
  const retention = useRetentionDeletionData({
    enabled: isGlobalTabActive,
    setRecentlySavedSection,
  });

  useEffect(() => {
    if (!recentlySavedSection) {
      return;
    }
    const timerId = window.setTimeout(() => {
      setRecentlySavedSection(null);
    }, 2500);
    return () => {
      window.clearTimeout(timerId);
    };
  }, [recentlySavedSection]);

  const renderSaveStateForSection = (section: SettingsSaveSection, isDirty: boolean) =>
    renderSectionSaveState({
      section,
      isDirty,
      recentlySavedSection,
    });

  return {
    recentlySavedSection,
    renderSaveStateForSection,
    selectedProfileLabel,
    ...auth,
    ...costing,
    ...audit,
    ...retention,
  };
}

export type SettingsPageData = ReturnType<typeof useSettingsPageData>;
