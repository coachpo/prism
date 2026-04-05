import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/context/useAuth";
import { useProfileContext } from "@/context/ProfileContext";
import { renderSectionSaveState } from "./sectionSaveState";
import type { SettingsSaveSection } from "./settingsSaveTypes";
import { useAuditConfigurationData } from "./useAuditConfigurationData";
import { useAuthenticationSettingsData } from "./useAuthenticationSettingsData";
import { useConfigBackupData } from "./useConfigBackupData";
import { useCostingSettingsData } from "./useCostingSettingsData";
import { useRetentionDeletionData } from "./useRetentionDeletionData";
import { useVendorManagementData } from "./useVendorManagementData";

export function useSettingsPageData() {
  const navigate = useNavigate();
  const { refreshAuth } = useAuth();
  const { selectedProfile, revision, bumpRevision } = useProfileContext();
  const selectedProfileLabel = selectedProfile
    ? `${selectedProfile.name} (#${selectedProfile.id})`
    : "the selected profile";

  const [recentlySavedSection, setRecentlySavedSection] = useState<SettingsSaveSection | null>(null);

  const backup = useConfigBackupData({ bumpRevision });
  const auth = useAuthenticationSettingsData({ navigate, refreshAuth, revision });
  const costing = useCostingSettingsData({ revision, setRecentlySavedSection });
  const audit = useAuditConfigurationData({ revision });
  const retention = useRetentionDeletionData();
  const vendorManagement = useVendorManagementData({ revision });
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
