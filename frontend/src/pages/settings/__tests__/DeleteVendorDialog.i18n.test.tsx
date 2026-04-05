import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Vendor, VendorModelUsageItem } from "@/lib/types";
import { DeleteVendorDialog } from "../dialogs/DeleteVendorDialog";

function buildVendor(overrides: Partial<Vendor> = {}): Vendor {
  const { icon_key = null, ...rest } = overrides;

  return {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "",
    updated_at: "",
    ...rest,
    icon_key,
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

describe("DeleteVendorDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("renders localized blocked-delete copy and dependency rows", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    render(
      <LocaleProvider>
        <DeleteVendorDialog
          deleteVendorConfirm={buildVendor()}
          deleteVendorConflict={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          vendorDeleting={false}
          vendorUsageLoading={false}
          vendorUsageRows={[buildUsageRow()]}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除供应商")).toBeInTheDocument();
    expect(screen.getByText("此供应商已被 1 个模型引用，当前无法删除。"));
    expect(screen.getByText("配置档案")).toBeInTheDocument();
    expect(screen.getByText("模型 ID")).toBeInTheDocument();
    expect(screen.getByText("API 家族")).toBeInTheDocument();
    expect(screen.getByText("模型类型")).toBeInTheDocument();
    expect(screen.getByText("Team Blue")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeDisabled();
  });

  it("renders the simple irreversible warning when the vendor is unused", () => {
    localStorage.setItem("prism.locale", "en");

    render(
      <LocaleProvider>
        <DeleteVendorDialog
          deleteVendorConfirm={buildVendor()}
          deleteVendorConflict={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          vendorDeleting={false}
          vendorUsageLoading={false}
          vendorUsageRows={[]}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Delete Vendor")).toBeInTheDocument();
    expect(screen.getByText("This action cannot be undone.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeEnabled();
  });
});
