import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useAuth } from "@/context/useAuth";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { renderSectionSaveState } from "./sectionSaveState";
import type { SettingsSaveSection } from "./settingsSaveTypes";
import { SETTINGS_TABS, type SettingsTab } from "./settingsPageHelpers";
import { useAuditConfigurationData } from "./useAuditConfigurationData";
import { useAuthenticationSettingsData } from "./useAuthenticationSettingsData";
import { useCostingSettingsData } from "./useCostingSettingsData";
import { useRetentionDeletionData } from "./useRetentionDeletionData";

export function useSettingsPageData(activeTab: SettingsTab) {
  const tanStackNavigate = useNavigate();
  const navigate = useCallback((to: string, options?: { replace?: boolean }) => {
    void tanStackNavigate({ to, replace: options?.replace });
  }, [tanStackNavigate]);
  const { refreshAuth } = useAuth();
  const [revision, setRevision] = useState(0);
  const { prime: primeReportingCurrency } = useReportingCurrencyContext();
  const selectedProfileLabel = "Default (#1)";
  const bumpRevision = () => setRevision((current) => current + 1);

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
