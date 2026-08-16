import { useCallback, useEffect, useMemo, useState } from "react";
import {
  DEFAULT_SECTION_BY_SCOPE,
  GLOBAL_SECTION_IDS,
  INSTANCE_SECTION_IDS,
  type SettingsScope,
} from "./settingsPageHelpers";

// Settings scope/section URL state (Settings SPEC §12.2): `scope=global|instance`
// matches the visible tabs exactly, scope-only URLs stay canonical with an
// implicit default section, and the legacy `tab` parameter is invalid and
// dropped during canonicalization. Section navigation uses history push;
// normalization uses replace. Back/forward restores the same visible state.

function getSearchParam(name: string): string {
  return new URLSearchParams(window.location.search).get(name) ?? "";
}

function isSettingsScope(value: string): value is SettingsScope {
  return value === "global" || value === "instance";
}

function resolveScope(scopeParam: string, sectionParam: string): SettingsScope {
  if (isSettingsScope(scopeParam)) {
    return scopeParam;
  }
  if (GLOBAL_SECTION_IDS.has(sectionParam)) return "global";
  if (INSTANCE_SECTION_IDS.has(sectionParam)) return "instance";
  return "global";
}

function resolveSection(scope: SettingsScope, sectionParam: string): string {
  if (GLOBAL_SECTION_IDS.has(sectionParam) && scope === "global") return sectionParam;
  if (INSTANCE_SECTION_IDS.has(sectionParam) && scope === "instance") return sectionParam;
  return DEFAULT_SECTION_BY_SCOPE[scope];
}

// Canonicalize the URL: drop the legacy `tab` parameter and any unknown
// scope/section; scope-only URLs stay canonical (no implicit section is
// serialized). Pricing-owned billing-currency keys are cleared unless an
// explicit section=billing-currency is present.
function canonicalizeSettingsLocation(scope: SettingsScope, explicitSection: string | null, explicitScope: boolean) {
  const params = new URLSearchParams(window.location.search);
  const pricingKeysAllowed = explicitSection === "billing-currency";
  const hasDisallowedPricingKeys = !pricingKeysAllowed && (params.has("costing_action") || params.has("pricing_inventory_id"));
  const needsCanonicalize = params.has("tab") || params.has("profile") || params.has("scope") !== explicitScope || window.location.hash !== "" || hasDisallowedPricingKeys;
  if (!needsCanonicalize && explicitSection !== null) {
    const currentSection = params.get("section");
    if (currentSection !== explicitSection) {
      params.set("section", explicitSection);
      window.history.replaceState(null, "", `${window.location.pathname}?${params.toString()}`);
    }
    return;
  }
  params.delete("tab");
  params.delete("profile");
  params.set("scope", scope);
  if (explicitSection) {
    params.set("section", explicitSection);
  } else {
    params.delete("section");
  }
  if (explicitSection !== "billing-currency") {
    // Pricing section-owned keys without an explicit billing section are
    // dropped (SPEC §12.2); they never infer a section.
    params.delete("costing_action");
    params.delete("pricing_inventory_id");
  }
  const search = params.toString();
  const nextUrl = `${window.location.pathname}${search ? `?${search}` : ""}`;
  window.history.replaceState(null, "", nextUrl);
}

function getResolvedState() {
  const scopeParam = getSearchParam("scope");
  const sectionParam = getSearchParam("section");
  const scope = resolveScope(scopeParam, sectionParam);
  const explicitScope = isSettingsScope(scopeParam);
  const sectionBelongsToScope = (GLOBAL_SECTION_IDS.has(sectionParam) && scope === "global")
    || (INSTANCE_SECTION_IDS.has(sectionParam) && scope === "instance");
  const hasDisallowedPricingKeys = sectionParam !== "billing-currency"
    && (getSearchParam("costing_action") !== "" || getSearchParam("pricing_inventory_id") !== "");
  return {
    scope,
    explicitScope,
    activeSectionId: resolveSection(scope, sectionParam),
    shouldNormalize: !explicitScope
      || (sectionParam !== "" && !sectionBelongsToScope)
      || getSearchParam("tab") !== ""
      || getSearchParam("profile") !== ""
      || hasDisallowedPricingKeys
      || window.location.hash !== "",
  };
}

export function useSettingsPageSectionState() {
  const [initialState] = useState(getResolvedState);
  const [scope, setScopeState] = useState<SettingsScope>(initialState.scope);
  const [activeSectionId, setActiveSectionIdState] = useState<string>(initialState.activeSectionId);

  useEffect(() => {
    const handleLocationStateChange = () => {
      const nextState = getResolvedState();
      setScopeState(nextState.scope);
      setActiveSectionIdState(nextState.activeSectionId);
      if (nextState.shouldNormalize) {
        const currentSection = getSearchParam("section");
        const sectionIsValid = (nextState.scope === "global" ? GLOBAL_SECTION_IDS : INSTANCE_SECTION_IDS).has(currentSection);
        canonicalizeSettingsLocation(
          nextState.scope,
          sectionIsValid ? nextState.activeSectionId : null,
          nextState.explicitScope,
        );
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

  const setScope = useCallback((nextScope: SettingsScope) => {
    setScopeState(nextScope);
    const params = new URLSearchParams(window.location.search);
    params.set("scope", nextScope);
    params.delete("section");
    params.delete("costing_action");
    params.delete("pricing_inventory_id");
    const search = params.toString();
    window.history.pushState(null, "", `${window.location.pathname}?${search}`);
    setActiveSectionIdState(DEFAULT_SECTION_BY_SCOPE[nextScope]);
  }, []);

  const jumpToSection = useCallback((sectionId: string) => {
    const nextScope: SettingsScope = GLOBAL_SECTION_IDS.has(sectionId) ? "global" : "instance";
    setScopeState(nextScope);
    setActiveSectionIdState(sectionId);
    const params = new URLSearchParams(window.location.search);
    params.set("scope", nextScope);
    params.set("section", sectionId);
    const search = params.toString();
    window.history.pushState(null, "", `${window.location.pathname}?${search}`);
  }, []);

  const setActiveSectionId = useCallback((sectionId: string) => {
    setActiveSectionIdState(sectionId);
  }, []);

  return useMemo(() => ({
    scope,
    activeSectionId,
    jumpToSection,
    setActiveSectionId,
    setScope,
  }), [scope, activeSectionId, jumpToSection, setActiveSectionId, setScope]);
}
