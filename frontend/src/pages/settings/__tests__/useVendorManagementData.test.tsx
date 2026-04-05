import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import type { Vendor, VendorModelUsageItem } from "@/lib/types";
import { useVendorManagementData } from "../useVendorManagementData";

const api = vi.hoisted(() => ({
  vendors: {
    create: vi.fn(),
    delete: vi.fn(),
    models: vi.fn(),
    update: vi.fn(),
  },
}));

const referenceData = vi.hoisted(() => ({
  getSharedVendors: vi.fn(),
  setSharedVendors: vi.fn(),
}));

const MockApiError = vi.hoisted(
  () =>
    class MockApiError extends Error {
      status: number;
      detail: unknown;

      constructor(message: string, status: number, detail: unknown) {
        super(message);
        this.status = status;
        this.detail = detail;
      }
    },
);

vi.mock("@/lib/api", () => ({ api, ApiError: MockApiError }));
vi.mock("@/lib/referenceData", () => referenceData);
vi.mock("@/i18n/format", () => ({
  getCurrentLocale: () => "en",
}));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function buildVendor(overrides: Partial<Vendor> = {}): Vendor {
  return {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: "Primary OpenAI vendor",
    icon_key: "openai",
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-27T00:00:00Z",
    updated_at: "2026-03-27T00:00:00Z",
    ...overrides,
  };
}

function buildUsageRow(overrides: Partial<VendorModelUsageItem> = {}): VendorModelUsageItem {
  return {
    model_config_id: 9,
    profile_id: 3,
    profile_name: "Team Blue",
    model_id: "openai/gpt-4.1",
    display_name: "GPT-4.1",
    model_type: "native",
    api_family: "openai",
    is_enabled: true,
    ...overrides,
  };
}

describe("useVendorManagementData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    referenceData.getSharedVendors.mockResolvedValue([]);
  });

  it("bootstraps vendors from the shared vendor cache", async () => {
    const vendor = buildVendor();
    referenceData.getSharedVendors.mockResolvedValue([vendor]);

    const { result } = renderHook(() => useVendorManagementData({ revision: 7 }));

    await waitFor(() => {
      expect(result.current.vendorsLoading).toBe(false);
    });

    expect(referenceData.getSharedVendors).toHaveBeenCalledWith(7);
    expect(result.current.vendors).toEqual([vendor]);
  });

  it("opens create and edit dialog state from vendor data", async () => {
    const vendor = buildVendor({ description: null, key: "anthropic", name: "Anthropic" });
    referenceData.getSharedVendors.mockResolvedValue([vendor]);

    const { result } = renderHook(() => useVendorManagementData({ revision: 7 }));

    await waitFor(() => {
      expect(result.current.vendorsLoading).toBe(false);
    });

    act(() => {
      result.current.openCreateVendorDialog();
    });

    expect(result.current.vendorDialogOpen).toBe(true);
    expect(result.current.editingVendor).toBeNull();
    expect(result.current.vendorForm).toEqual({
      description: "",
      icon_key: null,
      key: "",
      name: "",
    });

    act(() => {
      result.current.handleEditVendor(vendor);
    });

    expect(result.current.vendorDialogOpen).toBe(true);
    expect(result.current.editingVendor).toEqual(vendor);
    expect(result.current.vendorForm).toEqual({
      description: "",
      icon_key: "openai",
      key: "anthropic",
      name: "Anthropic",
    });
  });

  it("fetches vendor usage rows before opening the delete dialog", async () => {
    const vendor = buildVendor();
    const usageRow = buildUsageRow();
    referenceData.getSharedVendors.mockResolvedValue([vendor]);
    api.vendors.models.mockResolvedValue([usageRow]);

    const { result } = renderHook(() => useVendorManagementData({ revision: 7 }));

    await waitFor(() => {
      expect(result.current.vendorsLoading).toBe(false);
    });

    await act(async () => {
      await result.current.handleDeleteVendorClick(vendor);
    });

    expect(api.vendors.models).toHaveBeenCalledWith(vendor.id);
    expect(result.current.deleteVendorConfirm).toEqual(vendor);
    expect(result.current.vendorUsageRows).toEqual([usageRow]);
    expect(result.current.vendorUsageLoading).toBe(false);
  });

  it("parses blocked delete conflicts and keeps the vendor in local state", async () => {
    const vendor = buildVendor();
    const usageRow = buildUsageRow();
    referenceData.getSharedVendors.mockResolvedValue([vendor]);
    api.vendors.models.mockResolvedValue([]);
    api.vendors.delete.mockRejectedValue(
      new MockApiError("Conflict", 409, {
        message: "Cannot delete vendor that is referenced by models",
        models: [usageRow],
      }),
    );

    const { result } = renderHook(() => useVendorManagementData({ revision: 7 }));

    await waitFor(() => {
      expect(result.current.vendorsLoading).toBe(false);
    });

    await act(async () => {
      await result.current.handleDeleteVendorClick(vendor);
    });

    await act(async () => {
      await result.current.handleDeleteVendor();
    });

    expect(result.current.vendors).toEqual([vendor]);
    expect(result.current.deleteVendorConflict).toEqual([usageRow]);
    expect(toast.error).toHaveBeenCalled();
    expect(referenceData.setSharedVendors).not.toHaveBeenCalled();
  });

  it("updates the shared vendor cache after create, edit, and successful delete", async () => {
    const originalVendor = buildVendor();
    const createdVendor = buildVendor({
      id: 2,
      key: "anthropic",
      name: "Anthropic",
      description: "Claude models",
      icon_key: "anthropic",
    });
    const updatedVendor = buildVendor({
      id: 2,
      key: "anthropic-enterprise",
      name: "Anthropic Enterprise",
      description: "Enterprise Claude models",
    });

    referenceData.getSharedVendors.mockResolvedValue([originalVendor]);
    api.vendors.create.mockResolvedValue(createdVendor);
    api.vendors.update.mockResolvedValue(updatedVendor);
    api.vendors.models.mockResolvedValue([]);
    api.vendors.delete.mockResolvedValue(undefined);

    const { result } = renderHook(() => useVendorManagementData({ revision: 7 }));

    await waitFor(() => {
      expect(result.current.vendorsLoading).toBe(false);
    });

    act(() => {
      result.current.openCreateVendorDialog();
      result.current.setVendorForm({
        key: "anthropic",
        name: "Anthropic",
        description: "Claude models",
        icon_key: "anthropic",
      });
    });

    await act(async () => {
      await result.current.handleSaveVendor();
    });

    expect(api.vendors.create).toHaveBeenCalledWith({
      key: "anthropic",
      name: "Anthropic",
      description: "Claude models",
      icon_key: "anthropic",
    });
    expect(referenceData.setSharedVendors).toHaveBeenNthCalledWith(1, 7, [createdVendor, originalVendor]);
    expect(result.current.vendorDialogOpen).toBe(false);
    expect(result.current.vendorForm).toEqual({
      key: "anthropic",
      name: "Anthropic",
      description: "Claude models",
      icon_key: "anthropic",
    });

    act(() => {
      result.current.handleEditVendor(createdVendor);
      result.current.setVendorForm({
        key: "anthropic-enterprise",
        name: "Anthropic Enterprise",
        description: "Enterprise Claude models",
        icon_key: null,
      });
    });

    await act(async () => {
      await result.current.handleSaveVendor();
    });

    expect(api.vendors.update).toHaveBeenCalledWith(createdVendor.id, {
      key: "anthropic-enterprise",
      name: "Anthropic Enterprise",
      description: "Enterprise Claude models",
      icon_key: null,
    });
    expect(referenceData.setSharedVendors).toHaveBeenNthCalledWith(2, 7, [updatedVendor, originalVendor]);
    expect(result.current.vendorDialogOpen).toBe(false);
    expect(result.current.editingVendor?.id).toBe(2);
    expect(result.current.vendorForm).toEqual({
      key: "anthropic-enterprise",
      name: "Anthropic Enterprise",
      description: "Enterprise Claude models",
      icon_key: null,
    });

    await act(async () => {
      await result.current.handleDeleteVendorClick(updatedVendor);
    });

    await act(async () => {
      await result.current.handleDeleteVendor();
    });

    expect(api.vendors.delete).toHaveBeenCalledWith(updatedVendor.id);
    expect(referenceData.setSharedVendors).toHaveBeenNthCalledWith(3, 7, [originalVendor]);
    expect(result.current.vendors).toEqual([originalVendor]);
    expect(result.current.deleteVendorDialogOpen).toBe(false);
    expect(result.current.deleteVendorConfirm).toBeNull();
    expect(result.current.displayedDeleteVendorConfirm).toMatchObject({
      id: 2,
      name: "Anthropic Enterprise",
    });
  });
});
