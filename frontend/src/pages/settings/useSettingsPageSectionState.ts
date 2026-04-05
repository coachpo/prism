import { useCallback, useEffect, useMemo, useState } from "react";
import {
  INSTANCE_SECTION_IDS,
  SETTINGS_SECTIONS,
  SETTINGS_SECTION_IDS,
  SETTINGS_TABS,
  type SettingsTab,
} from "./settingsPageHelpers";

function getCurrentHash(): string {
  return window.location.hash.replace("#", "");
}

function resolveInitialTab(hash: string): SettingsTab {
  return INSTANCE_SECTION_IDS.has(hash) ? SETTINGS_TABS.global : SETTINGS_TABS.profile;
}

function resolveInitialSectionId(hash: string): string | null {
  if (INSTANCE_SECTION_IDS.has(hash)) {
    return null;
  }
  return SETTINGS_SECTION_IDS.has(hash) ? hash : SETTINGS_SECTIONS[0].id;
}

export function useSettingsPageSectionState() {
  const [activeTab, setActiveTabState] = useState<SettingsTab>(() => resolveInitialTab(getCurrentHash()));
  const [activeSectionId, setActiveSectionId] = useState<string | null>(() =>
    resolveInitialSectionId(getCurrentHash()),
  );
  const [isAuditConfigurationFocused, setIsAuditConfigurationFocused] = useState(false);

  useEffect(() => {
    const handleHashChange = () => {
      const hash = getCurrentHash();
      const shouldHighlightAudit = hash === "audit-configuration";
      if (INSTANCE_SECTION_IDS.has(hash)) {
        setActiveTabState(SETTINGS_TABS.global);
        setActiveSectionId(null);
        setIsAuditConfigurationFocused(false);
        return;
      }

      if (SETTINGS_SECTION_IDS.has(hash)) {
        setActiveTabState(SETTINGS_TABS.profile);
        setActiveSectionId(hash);
        setIsAuditConfigurationFocused(shouldHighlightAudit);
      } else {
        setIsAuditConfigurationFocused(false);
      }
    };

    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, []);

  const setActiveTab = useCallback((nextTab: SettingsTab) => {
    setActiveTabState(nextTab);
    if (nextTab === SETTINGS_TABS.global) {
      setActiveSectionId(null);
    } else if (activeSectionId === null) {
      setActiveSectionId(SETTINGS_SECTIONS[0].id);
    }
  }, [activeSectionId]);

  const jumpToSection = useCallback((sectionId: string) => {
    setActiveTab(SETTINGS_TABS.profile);
    setActiveSectionId(sectionId);
    setIsAuditConfigurationFocused(sectionId === "audit-configuration");
    window.history.replaceState(null, "", `#${sectionId}`);
  }, [setActiveTab]);

  return useMemo(() => ({
    activeSectionId,
    activeTab,
    isAuditConfigurationFocused,
    jumpToSection,
    setActiveSectionId,
    setActiveTab,
  }), [activeSectionId, activeTab, isAuditConfigurationFocused, jumpToSection, setActiveTab]);
}
