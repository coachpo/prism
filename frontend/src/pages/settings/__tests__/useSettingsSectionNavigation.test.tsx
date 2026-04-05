import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Location } from "react-router-dom";
import { SETTINGS_TABS } from "../settingsPageHelpers";
import { useSettingsSectionNavigation } from "../useSettingsSectionNavigation";

function createLocation(hash: string): Location {
  return {
    pathname: "/settings",
    search: "",
    hash,
    state: null,
    key: hash || "settings",
  } as Location;
}

function createDomRect(top: number) {
  return {
    top,
    bottom: top + 120,
    left: 0,
    right: 300,
    width: 300,
    height: 120,
    x: 0,
    y: top,
    toJSON: () => ({}),
  };
}

function SettingsNavigationHarness({ hash }: { hash: string }) {
  const { activeSectionId, activeTab, setActiveTab } = useSettingsSectionNavigation(
    createLocation(hash)
  );

  return (
    <div>
      <div data-testid="active-tab">{activeTab}</div>
      <div data-testid="active-section">{activeSectionId}</div>
      <button type="button" onClick={() => setActiveTab(SETTINGS_TABS.profile)}>
        Show Profile Tab
      </button>
      {activeTab === SETTINGS_TABS.profile ? (
        <div>
          <section id="backup">Backup</section>
          <section id="billing-currency">Billing &amp; Currency</section>
        </div>
      ) : (
        <section id="authentication">Authentication</section>
      )}
    </div>
  );
}

describe("useSettingsSectionNavigation", () => {
  beforeEach(() => {
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      return window.setTimeout(() => callback(0), 0);
    });
    vi.spyOn(window, "cancelAnimationFrame").mockImplementation((id) => {
      window.clearTimeout(id);
    });
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    Object.defineProperty(document.documentElement, "scrollHeight", {
      configurable: true,
      value: 2000,
    });
    Object.defineProperty(window, "scrollY", {
      configurable: true,
      value: 0,
      writable: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("switches to the instance tab and scrolls after an authentication hash change", async () => {
    const scrollIntoView = HTMLElement.prototype.scrollIntoView as ReturnType<typeof vi.fn>;
    const view = render(<SettingsNavigationHarness hash="#backup" />);

    await waitFor(() => {
      expect(screen.getByTestId("active-tab")).toHaveTextContent(SETTINGS_TABS.profile);
    });

    scrollIntoView.mockClear();
    view.rerender(<SettingsNavigationHarness hash="#authentication" />);

    await waitFor(() => {
      expect(screen.getByTestId("active-tab")).toHaveTextContent(SETTINGS_TABS.global);
    });
    await waitFor(() => {
      expect(scrollIntoView).toHaveBeenCalled();
    });
  });

  it("rebinds profile section tracking after the instance tab is shown first", async () => {
    render(<SettingsNavigationHarness hash="#authentication" />);

    await waitFor(() => {
      expect(screen.getByTestId("active-tab")).toHaveTextContent(SETTINGS_TABS.global);
    });

    fireEvent.click(screen.getByRole("button", { name: "Show Profile Tab" }));

    expect(screen.getByTestId("active-tab")).toHaveTextContent(SETTINGS_TABS.profile);

    let backupTop = 320;
    let billingTop = 120;
    const backupSection = document.getElementById("backup");
    const billingSection = document.getElementById("billing-currency");

    expect(backupSection).not.toBeNull();
    expect(billingSection).not.toBeNull();

    Object.defineProperty(backupSection, "getBoundingClientRect", {
      configurable: true,
      value: () => createDomRect(backupTop),
    });
    Object.defineProperty(billingSection, "getBoundingClientRect", {
      configurable: true,
      value: () => createDomRect(billingTop),
    });

    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });

    expect(screen.getByTestId("active-section")).toHaveTextContent("billing-currency");

    backupTop = 80;
    billingTop = 420;

    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });

    expect(screen.getByTestId("active-section")).toHaveTextContent("backup");
  });
});
