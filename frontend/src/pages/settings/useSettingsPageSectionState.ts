import { useCallback, useEffect, useMemo, useState } from "react";
import {
  INSTANCE_SECTION_IDS,
  SETTINGS_SECTIONS,
  SETTINGS_SECTION_IDS,
  SETTINGS_TABS,
  type SettingsTab,
} from "./settingsPageHelpers";

const DEFAULT_GLOBAL_SECTION_ID = "authentication";
const DEFAULT_PROFILE_SECTION_ID = SETTINGS_SECTIONS[0].id;

function getCurrentHash(): string {
  return window.location.hash.replace("#", "");
}

function replaceCurrentHash(hash: string | null) {
  const baseUrl = `${window.location.pathname}${window.location.search}`;
  window.history.replaceState(null, "", hash ? `${baseUrl}#${hash}` : baseUrl);
}

function isKnownHash(hash: string): boolean {
  return hash === SETTINGS_TABS.startup || INSTANCE_SECTION_IDS.has(hash) || SETTINGS_SECTION_IDS.has(hash);
}

function resolveTab(hash: string): SettingsTab {
  if (hash === SETTINGS_TABS.startup) {
    return SETTINGS_TABS.startup;
  }
  if (INSTANCE_SECTION_IDS.has(hash)) {
    return SETTINGS_TABS.global;
  }
  return SETTINGS_TABS.profile;
}

function resolveSectionId(hash: string): string | null {
  if (hash === SETTINGS_TABS.startup || INSTANCE_SECTION_IDS.has(hash)) {
    return null;
  }
  return SETTINGS_SECTION_IDS.has(hash) ? hash : DEFAULT_PROFILE_SECTION_ID;
}

export function useSettingsPageSectionState() {
  const [activeTab, setActiveTabState] = useState<SettingsTab>(() => resolveTab(getCurrentHash()));
  const [activeSectionId, setActiveSectionId] = useState<string | null>(() =>
    resolveSectionId(getCurrentHash()),
  );
  const [isAuditConfigurationFocused, setIsAuditConfigurationFocused] = useState(false);

  useEffect(() => {
    const handleHashChange = () => {
      const hash = getCurrentHash();
      const normalizedHash = isKnownHash(hash) ? hash : "";
      const shouldHighlightAudit = normalizedHash === "audit-configuration";

      if (hash && !normalizedHash) {
        replaceCurrentHash(null);
      }

      setActiveTabState(resolveTab(normalizedHash));
      setActiveSectionId(resolveSectionId(normalizedHash));
      setIsAuditConfigurationFocused(shouldHighlightAudit);
    };

    handleHashChange();
    window.addEventListener("hashchange", handleHashChange);
    return () => window.removeEventListener("hashchange", handleHashChange);
  }, []);

  const setActiveTab = useCallback((nextTab: SettingsTab) => {
    setActiveTabState(nextTab);
    if (nextTab === SETTINGS_TABS.startup) {
      setActiveSectionId(null);
      setIsAuditConfigurationFocused(false);
      replaceCurrentHash(SETTINGS_TABS.startup);
      return;
    }

    if (nextTab === SETTINGS_TABS.global) {
      setActiveSectionId(null);
      setIsAuditConfigurationFocused(false);
      replaceCurrentHash(DEFAULT_GLOBAL_SECTION_ID);
      return;
    }

    setActiveSectionId(DEFAULT_PROFILE_SECTION_ID);
    setIsAuditConfigurationFocused(false);
    replaceCurrentHash(null);
  }, []);

  const jumpToSection = useCallback((sectionId: string) => {
    setActiveTab(SETTINGS_TABS.profile);
    setActiveSectionId(sectionId);
    setIsAuditConfigurationFocused(sectionId === "audit-configuration");
    replaceCurrentHash(sectionId);
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
