import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSettingsPageSectionState } from "../useSettingsPageSectionState";

const { mockUseSettingsPageData } = vi.hoisted(() => ({
  mockUseSettingsPageData: vi.fn(),
}));

vi.mock("@/i18n/useLocale", () => ({
  useLocale: () => ({
    messages: {
      settingsPage: {
      settingsTitle: "Settings",
      settingsDescription: "Configure Prism.",
      profileTab: "Profile",
      globalTab: "Global",
      profileScopedSettings: "Profile scoped",
      profileScopedDescription: (label: string) => `Scoped to ${label}`,
      globalSettings: "Global settings",
      globalSettingsDescription: "Global settings description",
      },
    },
  }),
}));

vi.mock("@/components/PageHeader", () => ({
  PageHeader: ({ title, description }: { title: string; description: string }) => (
    <div>
      <h1>{title}</h1>
      <p>{description}</p>
    </div>
  ),
}));

vi.mock("../useSettingsPageData", () => ({
  useSettingsPageData: mockUseSettingsPageData,
}));

vi.mock("../SettingsSectionsNav", () => ({
  SettingsSectionsNav: () => <div>mock-settings-sections-nav</div>,
}));

vi.mock("../sections/BackupSection", () => ({
  BackupSection: () => <div>mock-backup-section</div>,
}));

vi.mock("../sections/BillingCurrencySection", () => ({
  BillingCurrencySection: () => <div>mock-billing-currency-section</div>,
}));

vi.mock("../sections/TimezoneSection", () => ({
  TimezoneSection: () => <div>mock-timezone-section</div>,
}));

vi.mock("../sections/AuditConfigurationSection", () => ({
  AuditConfigurationSection: () => <div>mock-audit-configuration-section</div>,
}));

vi.mock("../sections/AuthenticationSection", () => ({
  AuthenticationSection: () => <div>mock-authentication-section</div>,
}));

vi.mock("../sections/RetentionDeletionSection", () => ({
  RetentionDeletionSection: () => <div>mock-retention-deletion-section</div>,
}));

vi.mock("../dialogs/DeleteConfirmDialog", () => ({
  DeleteConfirmDialog: () => <div>delete-confirm-dialog</div>,
}));

vi.mock("../dialogs/RuleDialog", () => ({
  RuleDialog: () => <div>rule-dialog</div>,
}));

vi.mock("../dialogs/DeleteRuleConfirmDialog", () => ({
  DeleteRuleConfirmDialog: () => <div>delete-rule-confirm-dialog</div>,
}));

vi.mock("../SettingsProfileTab", () => ({
  SettingsProfileTab: ({
    activeSectionId,
    data,
  }: {
    activeSectionId: string | null;
    data: { selectedProfileLabel: string };
  }) => <div>{`profile-tab:${activeSectionId}:${data.selectedProfileLabel}`}</div>,
}));

vi.mock("../SettingsGlobalTab", () => ({
  SettingsGlobalTab: ({ data }: { data: { selectedProfileLabel: string } }) => (
    <div>{`global-tab:${data.selectedProfileLabel}`}</div>
  ),
}));

function buildSettingsPageData() {
  return {
    selectedProfileLabel: "Primary profile (#1)",
    deleteConfirm: null,
    setDeleteConfirm: vi.fn(),
    deleteConfirmPhrase: "",
    setDeleteConfirmPhrase: vi.fn(),
    handleBatchDelete: vi.fn(),
    deleting: false,
    isDeletePhraseValid: false,
    ruleDialogOpen: false,
    setRuleDialogOpen: vi.fn(),
    editingRule: null,
    ruleForm: {},
    setRuleForm: vi.fn(),
    handleSaveRule: vi.fn(),
    deleteRuleConfirm: null,
    setDeleteRuleConfirm: vi.fn(),
    handleDeleteRule: vi.fn(),
  } as const;
}

describe("useSettingsPageSectionState", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/settings");
    mockUseSettingsPageData.mockReset();
  });

  it("starts on the global tab without a profile section when the authentication hash is present", () => {
    window.history.replaceState(null, "", "/settings#authentication");

    const { result } = renderHook(() => useSettingsPageSectionState());

    expect(result.current.activeTab).toBe("global");
    expect(result.current.activeSectionId).toBeNull();
  });

  it("activates the profile tab and writes the section hash when jumping to a profile section", () => {
    const { result } = renderHook(() => useSettingsPageSectionState());

    act(() => {
      result.current.jumpToSection("audit-configuration");
    });

    expect(result.current.activeTab).toBe("profile");
    expect(result.current.activeSectionId).toBe("audit-configuration");
    expect(window.location.hash).toBe("#audit-configuration");
  });

  it("clears the active section when switching to the global tab", () => {
    const { result } = renderHook(() => useSettingsPageSectionState());

    act(() => {
      result.current.jumpToSection("audit-configuration");
      result.current.setActiveTab("global");
    });

    expect(result.current.activeTab).toBe("global");
    expect(result.current.activeSectionId).toBeNull();
  });
});

describe("SettingsPage shell", () => {
  beforeEach(() => {
    mockUseSettingsPageData.mockReturnValue(buildSettingsPageData());
    window.history.replaceState(null, "", "/settings");
  });

  it("opens the global settings tab immediately for the authentication hash route", async () => {
    window.history.replaceState(null, "", "/settings#authentication");

    const { SettingsPage } = await import("../../SettingsPage");

    render(<SettingsPage />);

    expect(screen.getByText("global-tab:Primary profile (#1)")).toBeInTheDocument();
  });

  it("mounts extracted profile and global tab bodies while keeping tab state and dialogs in the shell", async () => {
    const { SettingsPage } = await import("../../SettingsPage");

    render(<SettingsPage />);

    expect(screen.getByText("profile-tab:backup:Primary profile (#1)")).toBeInTheDocument();
    expect(screen.getByText("delete-confirm-dialog")).toBeInTheDocument();
    expect(screen.getByText("rule-dialog")).toBeInTheDocument();
    expect(screen.getByText("delete-rule-confirm-dialog")).toBeInTheDocument();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Global" }));

    expect(screen.getByText("global-tab:Primary profile (#1)")).toBeInTheDocument();

    fireEvent.mouseDown(screen.getByRole("tab", { name: "Profile" }));

    expect(screen.getByText("profile-tab:backup:Primary profile (#1)")).toBeInTheDocument();
  });
});
