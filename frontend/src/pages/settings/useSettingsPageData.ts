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
import { useConfigBackupData } from "./useConfigBackupData";
import { useCostingSettingsData } from "./useCostingSettingsData";
import { useRetentionDeletionData } from "./useRetentionDeletionData";
import { useVendorManagementData } from "./useVendorManagementData";

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

  const backup = useConfigBackupData({
    bumpRevision,
    selectedProfileId: selectedProfile?.id ?? null,
  });
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
  const audit = useAuditConfigurationData({ enabled: isProfileTabActive, revision });
  const retention = useRetentionDeletionData({
    enabled: isGlobalTabActive,
    setRecentlySavedSection,
  });
  const vendorManagement = useVendorManagementData({
    bumpRevision,
    enabled: isGlobalTabActive,
    revision,
  });
  const { vendors: auditVendors, ...auditData } = audit;

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
    ...backup,
    ...auth,
    ...costing,
    ...auditData,
    auditVendors,
    ...retention,
    ...vendorManagement,
  };
}

export type SettingsPageData = ReturnType<typeof useSettingsPageData>;
