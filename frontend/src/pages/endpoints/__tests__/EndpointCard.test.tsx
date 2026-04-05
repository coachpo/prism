import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Endpoint, ModelConfigListItem, Vendor } from "@/lib/types";
import { EndpointCardView } from "../EndpointCard";

function buildVendor(overrides: Partial<Vendor> = {}): Vendor {
  const { icon_key = null, ...rest } = overrides;

  return {
    id: 7,
    key: "openai",
    name: "OpenAI",
    description: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    ...rest,
    icon_key,
  };
}

function buildModel(overrides: Partial<ModelConfigListItem> = {}): ModelConfigListItem {
  return {
    id: 11,
    vendor_id: 7,
    vendor: buildVendor(),
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o Mini",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: 100,
    loadbalance_strategy: {
      id: 100,
      name: "single-primary",
      strategy_type: "legacy",
      legacy_strategy_type: "single",
      auto_recovery: { mode: "disabled" },
    },
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 99,
    health_total_requests: 100,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    ...overrides,
  };
}

function buildEndpoint(overrides: Partial<Endpoint> = {}): Endpoint {
  return {
    id: 5,
    profile_id: 2,
    name: "Primary OpenAI",
    base_url: "https://api.openai.com",
    has_api_key: true,
    masked_api_key: "sk-••••1234",
    position: 0,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-20T10:00:00Z",
    ...overrides,
  };
}

describe("EndpointCardView", () => {
  it("renders accessible duplicate, edit, and delete controls and wires their callbacks", () => {
    const endpoint = buildEndpoint();
    const onDuplicate = vi.fn();
    const onEdit = vi.fn();
    const onDelete = vi.fn();

    render(
      <LocaleProvider>
        <EndpointCardView
          endpoint={endpoint}
          formatTime={() => "Mar 20, 2026"}
          models={[buildModel()]}
          onDuplicate={onDuplicate}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      </LocaleProvider>
    );

    const duplicateButton = screen.getByRole("button", {
      name: "Duplicate endpoint Primary OpenAI",
    });
    const editButton = screen.getByRole("button", { name: "Edit endpoint Primary OpenAI" });
    const deleteButton = screen.getByRole("button", {
      name: "Are you sure you want to delete \"Primary OpenAI\"? This action cannot be undone.",
    });

    fireEvent.click(duplicateButton);
    fireEvent.click(editButton);
    fireEvent.click(deleteButton);

    expect(duplicateButton).toBeEnabled();
    expect(editButton).toBeEnabled();
    expect(deleteButton).toBeEnabled();
    expect(onDuplicate).toHaveBeenCalledWith(endpoint);
    expect(onEdit).toHaveBeenCalledWith(endpoint);
    expect(onDelete).toHaveBeenCalledWith(endpoint);
  });

  it("keeps the duplicate action disabled and spinning while duplication is in progress", () => {
    const endpoint = buildEndpoint();

    render(
      <LocaleProvider>
        <EndpointCardView
          endpoint={endpoint}
          formatTime={() => "Mar 20, 2026"}
          models={[]}
          isDuplicating={true}
        />
      </LocaleProvider>
    );

    const duplicateButton = screen.getByRole("button", {
      name: "Duplicate endpoint Primary OpenAI",
    });
    const editButton = screen.getByRole("button", { name: "Edit endpoint Primary OpenAI" });
    const deleteButton = screen.getByRole("button", {
      name: "Are you sure you want to delete \"Primary OpenAI\"? This action cannot be undone.",
    });
    const duplicateIcon = duplicateButton.querySelector("svg");

    expect(duplicateButton).toBeDisabled();
    expect(editButton).toBeEnabled();
    expect(deleteButton).toBeEnabled();
    expect(duplicateIcon).not.toBeNull();
    expect(duplicateIcon).toHaveClass("animate-spin");
  });

  it("renders localized endpoint action labels when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");
    const endpoint = buildEndpoint();

    render(
      <LocaleProvider>
        <EndpointCardView
          endpoint={endpoint}
          formatTime={() => "2026-03-20"}
          models={[]}
        />
      </LocaleProvider>
    );

    expect(screen.getByRole("button", { name: "复制端点 Primary OpenAI" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "编辑端点 Primary OpenAI" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "确定要删除“Primary OpenAI”吗？此操作无法撤销。" })).toBeInTheDocument();
  });

  it("renders localized endpoint card metadata when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");
    const endpoint = buildEndpoint();

    render(
      <LocaleProvider>
        <EndpointCardView
          endpoint={endpoint}
          formatTime={() => "2026-03-20"}
          models={[]}
        />
      </LocaleProvider>
    );

    expect(screen.getByRole("button", { name: "拖动以重新排序端点 Primary OpenAI" })).toBeInTheDocument();
    expect(screen.getByText("创建于 2026-03-20")).toBeInTheDocument();
    expect(screen.getByText("模型")).toBeInTheDocument();
    expect(screen.getByText("无")) .toBeInTheDocument();
  });
});
