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

function getSearchParam(name: string): string {
  return new URLSearchParams(window.location.search).get(name) ?? "";
}

function replaceSettingsLocation(tab: SettingsTab, sectionId: string | null) {
  const params = new URLSearchParams(window.location.search);
  params.set("tab", tab);
  if (sectionId) {
    params.set("section", sectionId);
  } else {
    params.delete("section");
  }

  const search = params.toString();
  const hash = sectionId;
  const nextUrl = `${window.location.pathname}${search ? `?${search}` : ""}${hash ? `#${hash}` : ""}`;
  window.history.replaceState(null, "", nextUrl);
}
function isKnownHash(hash: string): boolean {
  return INSTANCE_SECTION_IDS.has(hash) || SETTINGS_SECTION_IDS.has(hash);
}

function isSettingsTab(value: string): value is SettingsTab {
  return value === SETTINGS_TABS.profile || value === SETTINGS_TABS.global;
}

function resolveTab(hash: string, tabParam = "", sectionParam = ""): SettingsTab {
  if (isKnownHash(hash)) {
    return INSTANCE_SECTION_IDS.has(hash) ? SETTINGS_TABS.global : SETTINGS_TABS.profile;
  }

  if (INSTANCE_SECTION_IDS.has(sectionParam)) return SETTINGS_TABS.global;
  if (SETTINGS_SECTION_IDS.has(sectionParam)) return SETTINGS_TABS.profile;
  if (isSettingsTab(tabParam)) return tabParam;
  return SETTINGS_TABS.profile;
}

function resolveSectionId(tab: SettingsTab, hash: string, sectionParam = ""): string | null {
  if (INSTANCE_SECTION_IDS.has(hash)) return null;
  if (SETTINGS_SECTION_IDS.has(hash)) return hash;
  if (tab === SETTINGS_TABS.profile && SETTINGS_SECTION_IDS.has(sectionParam)) return sectionParam;
  return tab === SETTINGS_TABS.profile ? DEFAULT_PROFILE_SECTION_ID : null;
}

function getResolvedState() {
  const hash = getCurrentHash();
  const tabParam = getSearchParam("tab");
  const sectionParam = getSearchParam("section");
  const tab = resolveTab(hash, tabParam, sectionParam);
  return {
    activeTab: tab,
    activeSectionId: resolveSectionId(tab, hash, sectionParam),
    isAuditConfigurationFocused: hash === "audit-configuration" || sectionParam === "audit-configuration",
    shouldNormalizeLocation: (hash.length > 0 && !isKnownHash(hash)) || (tabParam.length > 0 && !isSettingsTab(tabParam)),
  };
}

export function useSettingsPageSectionState() {
  const [initialState] = useState(getResolvedState);
  const [activeTab, setActiveTabState] = useState<SettingsTab>(initialState.activeTab);
  const [activeSectionId, setActiveSectionId] = useState<string | null>(initialState.activeSectionId);
  const [isAuditConfigurationFocused, setIsAuditConfigurationFocused] = useState(initialState.isAuditConfigurationFocused);

  useEffect(() => {
    const handleLocationStateChange = () => {
      const nextState = getResolvedState();
      setActiveTabState(nextState.activeTab);
      setActiveSectionId(nextState.activeSectionId);
      setIsAuditConfigurationFocused(nextState.isAuditConfigurationFocused);
      if (nextState.shouldNormalizeLocation) {
        replaceSettingsLocation(nextState.activeTab, nextState.activeSectionId);
      }
    };

    handleLocationStateChange();
    window.addEventListener("hashchange", handleLocationStateChange);
    window.addEventListener("popstate", handleLocationStateChange);
    return () => {
      window.removeEventListener("hashchange", handleLocationStateChange);
      window.removeEventListener("popstate", handleLocationStateChange);
    };
  }, []);

  const setActiveTab = useCallback((nextTab: SettingsTab) => {
    setActiveTabState(nextTab);
    if (nextTab === SETTINGS_TABS.global) {
      setActiveSectionId(null);
      setIsAuditConfigurationFocused(false);
      replaceSettingsLocation(SETTINGS_TABS.global, DEFAULT_GLOBAL_SECTION_ID);
      return;
    }

    setActiveSectionId(DEFAULT_PROFILE_SECTION_ID);
    setIsAuditConfigurationFocused(false);
    replaceSettingsLocation(SETTINGS_TABS.profile, DEFAULT_PROFILE_SECTION_ID);
  }, []);

  const jumpToSection = useCallback((sectionId: string) => {
    const nextTab = INSTANCE_SECTION_IDS.has(sectionId) ? SETTINGS_TABS.global : SETTINGS_TABS.profile;
    setActiveTabState(nextTab);
    setActiveSectionId(nextTab === SETTINGS_TABS.profile ? sectionId : null);
    setIsAuditConfigurationFocused(sectionId === "audit-configuration");
    replaceSettingsLocation(nextTab, sectionId);
  }, []);

  return useMemo(() => ({
    activeSectionId,
    activeTab,
    isAuditConfigurationFocused,
    jumpToSection,
    setActiveSectionId,
    setActiveTab,
  }), [activeSectionId, activeTab, isAuditConfigurationFocused, jumpToSection, setActiveTab]);
}
