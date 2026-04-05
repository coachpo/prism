import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { Connection, LoadbalanceCurrentStateItem, ModelConfig, Vendor } from "@/lib/types";
import { ConnectionCard } from "../connections-list/ConnectionCard";
import { ConnectionCardActions } from "../connections-list/ConnectionCardActions";
import { ConnectionCardHeader } from "../connections-list/ConnectionCardHeader";

function buildVendor(): Vendor {
  return {
    id: 1,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "2026-03-23T10:00:00Z",
    updated_at: "2026-03-23T10:00:00Z",
  };
}

function buildModel(): ModelConfig {
  return {
    id: 5,
    vendor_id: 1,
    vendor: buildVendor(),
    api_family: "openai",
    model_id: "gpt-5.4",
    display_name: "GPT-5.4",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: 101,
    loadbalance_strategy: {
      id: 101,
      name: "round-robin-primary",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
      auto_recovery: {
        mode: "enabled",
        status_codes: [429, 503],
        cooldown: {
          base_seconds: 45,
          failure_threshold: 4,
          backoff_multiplier: 3.5,
          max_cooldown_seconds: 720,
          jitter_ratio: 0.35,
        },
        ban: {
          mode: "temporary",
          max_cooldown_strikes_before_ban: 3,
          ban_duration_seconds: 1800,
        },
      },
    },
    is_enabled: true,
    connections: [],
    created_at: "2026-03-23T10:00:00Z",
    updated_at: "2026-03-23T10:00:00Z",
  };
}

function buildConnection(): Connection {
  return {
    id: 11,
    model_config_id: 5,
    endpoint_id: 7,
    endpoint: {
      id: 7,
      name: "Primary endpoint",
      base_url: "https://api.example.com/v1",
      has_api_key: true,
      masked_api_key: "sk-••••",
      position: 0,
      created_at: "2026-03-23T10:00:00Z",
      updated_at: "2026-03-23T10:00:00Z",
    },
    is_active: true,
    priority: 0,
    name: "Primary",
    auth_type: null,
    custom_headers: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "healthy",
    health_detail: null,
    last_health_check: "2026-03-23T10:00:00Z",
    created_at: "2026-03-23T10:00:00Z",
    updated_at: "2026-03-23T10:00:00Z",
  };
}

function buildCurrentState(
  state: LoadbalanceCurrentStateItem["state"],
  overrides: Partial<LoadbalanceCurrentStateItem> = {},
): LoadbalanceCurrentStateItem {
  return {
    connection_id: 11,
    consecutive_failures: 3,
    last_failure_kind: "timeout",
    last_cooldown_seconds: 45,
    blocked_until_at: "2026-03-23T10:05:00Z",
    state,
    max_cooldown_strikes: state === "banned" ? 3 : 0,
    ban_mode: state === "banned" ? "temporary" : "off",
    banned_until_at: state === "banned" ? "2026-03-23T10:10:00Z" : null,
    created_at: "2026-03-23T10:00:00Z",
    updated_at: "2026-03-23T10:01:00Z",
    ...overrides,
  } as LoadbalanceCurrentStateItem;
}

function renderWithLocale(ui: React.ReactElement) {
  return render(<LocaleProvider>{ui}</LocaleProvider>);
}

function getStatusDot(container: HTMLElement) {
  return container.querySelector('[data-slot="status-dot"]');
}

describe("ConnectionCard cooldown state", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it.each([
    ["blocked", "Recovery Blocked"],
    ["counting", "Recovery Counting"],
    ["banned", "Banned"],
  ] as const)("renders the %s cooldown signal", (state, badgeLabel) => {
    renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState(state)}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.getByText(badgeLabel)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reset Recovery State" })).toBeEnabled();
  });

  it("hides the cooldown panel when counting has zero tracked failures", () => {
    renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState("counting", {
          consecutive_failures: 0,
          last_failure_kind: null,
        })}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.queryByText("Recovery Counting")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset Recovery State" })).not.toBeInTheDocument();
  });

  it("renders temporary ban copy with the ban expiry time", () => {
    renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState("banned", {
          ban_mode: "temporary" as LoadbalanceCurrentStateItem["ban_mode"],
          banned_until_at: "2026-03-23T10:10:00Z",
          max_cooldown_strikes: 3,
        })}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.getByText(/This connection is banned until formatted:2026-03-23T10:10:00Z/i)).toBeInTheDocument();
  });

  it("renders manual ban copy with dismiss wording", () => {
    renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState("banned", {
          ban_mode: "manual" as LoadbalanceCurrentStateItem["ban_mode"],
          banned_until_at: null,
          max_cooldown_strikes: 4,
        })}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.getByText(/This connection is banned until the operator dismisses it/i)).toBeInTheDocument();
  });

  it("calls reset and shows loading state without affecting the rest of the card", () => {
    const handleResetCooldown = vi.fn();

    const { rerender } = renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState("blocked")}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={handleResetCooldown}
        onToggleActive={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Reset Recovery State" }));

    expect(handleResetCooldown).toHaveBeenCalledWith(11);
    expect(screen.getByRole("switch")).toBeEnabled();

    rerender(
      <LocaleProvider>
        <ConnectionCard
          connection={buildConnection()}
          model={buildModel()}
          loadbalanceCurrentState={buildCurrentState("blocked")}
          isChecking={false}
          isResettingCooldown
          isFocused={false}
          formatTime={(value) => `formatted:${value}`}
          reorderDisabled={false}
          onEdit={vi.fn()}
          onDelete={vi.fn()}
          onHealthCheck={vi.fn()}
          onResetCooldown={handleResetCooldown}
          onToggleActive={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByRole("button", { name: "Reset Recovery State" })).toBeDisabled();
    expect(screen.getByRole("switch")).toBeEnabled();
  });

  it("shows no cooldown panel when there is no current state row", () => {
    renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={undefined}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.queryByRole("button", { name: "Reset Recovery State" })).not.toBeInTheDocument();
    expect(screen.queryByText("Recovery Blocked")).not.toBeInTheDocument();
  });

  it("renders header badges while leaving health visible only through the shared dot", () => {
    const { container } = renderWithLocale(
      <ConnectionCardHeader
        connection={{ ...buildConnection(), is_active: false }}
        connectionName="Primary"
        isChecking={false}
      />,
    );

    expect(getStatusDot(container)).toHaveAttribute("data-intent", "healthy");
    expect(getStatusDot(container)).toHaveAttribute("data-animated", "true");
    expect(screen.queryByText("Healthy")).not.toBeInTheDocument();
    expect(screen.getByText("Pricing Off")).toBeInTheDocument();
    expect(screen.getByText("Inactive")).toBeInTheDocument();
  });

  it("keeps monitoring-derived evidence out of the card body", () => {
    renderWithLocale(
      <ConnectionCard
        connection={buildConnection()}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState("blocked")}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.queryByText("45s cadence")).not.toBeInTheDocument();
    expect(screen.queryByText("Endpoint 82 ms")).not.toBeInTheDocument();
    expect(screen.queryByText("Conversation 310 ms")).not.toBeInTheDocument();
    expect(screen.queryByTestId("monitoring-probe-strip")).not.toBeInTheDocument();
    expect(screen.getByText("Recovery Blocked")).toBeInTheDocument();
  });

  it("applies muted shell styling to inactive cards while keeping actions available", () => {
    const { container } = renderWithLocale(
      <ConnectionCard
        connection={{ ...buildConnection(), is_active: false }}
        model={buildModel()}
        loadbalanceCurrentState={undefined}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    const card = container.firstElementChild;
    const detailsRow = screen.getByText("Primary endpoint").closest("div");
    const contentColumn = detailsRow?.parentElement;

    expect(card).toHaveClass("border-border/60", "bg-muted/20");
    expect(contentColumn).toHaveClass("opacity-75");
    expect(screen.getByRole("switch")).toBeEnabled();
  });

  it("wires action callbacks through the extracted actions component", async () => {
    const onEdit = vi.fn();
    const onDelete = vi.fn();
    const onHealthCheck = vi.fn();
    const onToggleActive = vi.fn();

    renderWithLocale(
      <ConnectionCardActions
        connection={buildConnection()}
        isChecking={false}
        onEdit={onEdit}
        onDelete={onDelete}
        onHealthCheck={onHealthCheck}
        onToggleActive={onToggleActive}
      />,
    );

    fireEvent.click(screen.getByRole("switch"));

    const actionsTrigger = screen.getByRole("button", { name: "Connection actions" });

    expect(actionsTrigger).toBeInTheDocument();
    expect(onEdit).not.toHaveBeenCalled();
    expect(onHealthCheck).not.toHaveBeenCalled();
    expect(onDelete).not.toHaveBeenCalled();
    expect(onToggleActive).toHaveBeenCalledWith(expect.objectContaining({ id: 11 }));

    actionsTrigger.focus();
    fireEvent.keyDown(actionsTrigger, { key: "Enter", code: "Enter" });

    await waitFor(() => {
      expect(screen.getByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    });
    expect(screen.getByRole("menuitem", { name: "Health Check" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
  });

  it("renders localized connection-card copy in Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderWithLocale(
      <ConnectionCard
        connection={{ ...buildConnection(), is_active: false }}
        model={buildModel()}
        loadbalanceCurrentState={buildCurrentState("blocked")}
        isChecking={false}
        isResettingCooldown={false}
        isFocused={false}
        formatTime={(value) => `formatted:${value}`}
        reorderDisabled={false}
        onEdit={vi.fn()}
        onDelete={vi.fn()}
        onHealthCheck={vi.fn()}
        onResetCooldown={vi.fn()}
        onToggleActive={vi.fn()}
      />,
    );

    expect(screen.getByLabelText("拖动以重新排序连接 Primary")).toBeInTheDocument();
    expect(screen.getByText("未启用定价")).toBeInTheDocument();
    expect(screen.getByText("未激活")).toBeInTheDocument();
    expect(screen.getByText("恢复阻止中")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重置恢复状态" })).toBeInTheDocument();
  });
});
