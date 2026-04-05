import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { useShellNavigation } from "../useShellNavigation";

function createWrapper(initialEntry: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <LocaleProvider>
        <MemoryRouter initialEntries={[initialEntry]}>{children}</MemoryRouter>
      </LocaleProvider>
    );
  };
}

function getNavigation(initialEntry: string) {
  return renderHook(() => useShellNavigation(), {
    wrapper: createWrapper(initialEntry),
  }).result.current;
}

describe("useShellNavigation", () => {
  it("builds deterministic breadcrumbs for model detail routes without pathname splitting", () => {
    const navigation = getNavigation("/models/5");

    expect(navigation.matchedRoute.id).toBe("model-detail");
    expect(navigation.activeSidebarItem.id).toBe("models");
    expect(navigation.isProfileScopedPage).toBe(true);
    expect(navigation.breadcrumbs).toEqual([
      { current: false, href: "/models", id: "models", label: "Models" },
      { current: true, href: null, id: "model-detail", label: "Configuration" },
    ]);
  });

  it("keeps proxy model routes under the models sidebar item and adds the model-detail ancestor", () => {
    const navigation = getNavigation("/models/5/proxy");

    expect(navigation.matchedRoute.id).toBe("proxy-model-detail");
    expect(navigation.activeSidebarItem.id).toBe("models");
    expect(navigation.isProfileScopedPage).toBe(true);
    expect(navigation.breadcrumbs).toEqual([
      { current: false, href: "/models", id: "models", label: "Models" },
      { current: false, href: "/models/5", id: "model-detail", label: "Configuration" },
      { current: true, href: null, id: "proxy-model-detail", label: "Proxy Routing" },
    ]);
  });

  it("adds settings hash sections as breadcrumb leaves without making them sidebar destinations", () => {
    const navigation = getNavigation("/settings#authentication");

    expect(navigation.matchedRoute.id).toBe("settings");
    expect(navigation.activeSidebarItem.id).toBe("settings");
    expect(navigation.isProfileScopedPage).toBe(false);
    expect(navigation.breadcrumbs).toEqual([
      { current: false, href: "/settings", id: "settings", label: "Settings" },
      { current: true, href: null, id: "settings-authentication", label: "Authentication" },
    ]);
    expect(navigation.sidebarItems.some((item) => item.to.includes("#authentication"))).toBe(false);
  });

  it("does not treat the removed monitoring settings hash as a breadcrumb leaf", () => {
    const navigation = getNavigation("/settings#monitoring");

    expect(navigation.matchedRoute.id).toBe("settings");
    expect(navigation.activeSidebarItem.id).toBe("settings");
    expect(navigation.breadcrumbs).toEqual([
      { current: true, href: null, id: "settings", label: "Settings" },
    ]);
  });

  it("keeps request-log exact mode as a breadcrumb leaf only", () => {
    const navigation = getNavigation("/request-logs?request_id=42&detail_tab=audit");

    expect(navigation.matchedRoute.id).toBe("request-logs");
    expect(navigation.activeSidebarItem.id).toBe("request-logs");
    expect(navigation.isProfileScopedPage).toBe(true);
    expect(navigation.breadcrumbs).toEqual([
      { current: false, href: "/request-logs", id: "request-logs", label: "Request Logs" },
      { current: true, href: null, id: "request-logs-request", label: "#42" },
    ]);
    expect(navigation.sidebarItems.some((item) => item.to.includes("request_id="))).toBe(false);
  });
});
