import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Vendor } from "@/lib/types";
import { useSettingsPageData } from "../useSettingsPageData";

const {
  mockUseNavigate,
  mockUseAuth,
  mockUseProfileContext,
  mockUseConfigBackupData,
  mockUseAuthenticationSettingsData,
  mockUseCostingSettingsData,
  mockUseAuditConfigurationData,
  mockUseRetentionDeletionData,
  mockUseVendorManagementData,
  mockRenderSectionSaveState,
} = vi.hoisted(() => ({
  mockUseNavigate: vi.fn(),
  mockUseAuth: vi.fn(),
  mockUseProfileContext: vi.fn(),
  mockUseConfigBackupData: vi.fn(),
  mockUseAuthenticationSettingsData: vi.fn(),
  mockUseCostingSettingsData: vi.fn(),
  mockUseAuditConfigurationData: vi.fn(),
  mockUseRetentionDeletionData: vi.fn(),
  mockUseVendorManagementData: vi.fn(),
  mockRenderSectionSaveState: vi.fn(() => "save-state"),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: mockUseNavigate,
}));

vi.mock("@/context/useAuth", () => ({
  useAuth: mockUseAuth,
}));

vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: mockUseProfileContext,
}));

vi.mock("../sectionSaveState", () => ({
  renderSectionSaveState: mockRenderSectionSaveState,
}));

vi.mock("../useConfigBackupData", () => ({
  useConfigBackupData: mockUseConfigBackupData,
}));

vi.mock("../useAuthenticationSettingsData", () => ({
  useAuthenticationSettingsData: mockUseAuthenticationSettingsData,
}));

vi.mock("../useCostingSettingsData", () => ({
  useCostingSettingsData: mockUseCostingSettingsData,
}));

vi.mock("../useAuditConfigurationData", () => ({
  useAuditConfigurationData: mockUseAuditConfigurationData,
}));

vi.mock("../useRetentionDeletionData", () => ({
  useRetentionDeletionData: mockUseRetentionDeletionData,
}));

vi.mock("../useVendorManagementData", () => ({
  useVendorManagementData: mockUseVendorManagementData,
}));

describe("useSettingsPageData", () => {
  it("keeps audit vendors separate from global vendor management vendors", () => {
    const auditVendors: Vendor[] = [
      {
        id: 1,
        key: "openai",
        name: "OpenAI",
        description: null,
        icon_key: "openai",
        audit_enabled: true,
        audit_capture_bodies: false,
        created_at: "",
        updated_at: "",
      },
    ];
    const vendorManagementVendors: Vendor[] = [
      {
        id: 2,
        key: "anthropic",
        name: "Anthropic",
        description: null,
        icon_key: "anthropic",
        audit_enabled: false,
        audit_capture_bodies: false,
        created_at: "",
        updated_at: "",
      },
    ];
    const toggleAudit = vi.fn();

    mockUseNavigate.mockReturnValue(vi.fn());
    mockUseAuth.mockReturnValue({ refreshAuth: vi.fn() });
    mockUseProfileContext.mockReturnValue({
      selectedProfile: { id: 7, name: "Primary" },
      revision: 3,
      bumpRevision: vi.fn(),
    });
    mockUseConfigBackupData.mockReturnValue({ backupToken: "backup" });
    mockUseAuthenticationSettingsData.mockReturnValue({ authToken: "auth" });
    mockUseCostingSettingsData.mockReturnValue({ costingToken: "costing" });
    mockUseAuditConfigurationData.mockReturnValue({
      vendors: auditVendors,
      toggleAudit,
    });
    mockUseRetentionDeletionData.mockReturnValue({ retentionToken: "retention" });
    mockUseVendorManagementData.mockReturnValue({
      vendors: vendorManagementVendors,
      vendorsLoading: false,
    });

    const { result } = renderHook(() => useSettingsPageData());

    expect(result.current.selectedProfileLabel).toBe("Primary (#7)");
    expect(result.current.auditVendors).toEqual(auditVendors);
    expect(result.current.vendors).toEqual(vendorManagementVendors);
    expect(result.current.toggleAudit).toBe(toggleAudit);
  });
});
