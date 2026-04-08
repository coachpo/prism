import { useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { LoadbalanceStrategyDialog } from "../LoadbalanceStrategyDialog";
import { DeleteLoadbalanceStrategyDialog } from "../DeleteLoadbalanceStrategyDialog";
import {
  DEFAULT_LOADBALANCE_STRATEGY_FORM,
  getDefaultAutoRecoveryDraft,
  type LoadbalanceStrategyFormState,
} from "../loadbalanceStrategyFormState";
import { createDefaultTimeoutPolicy } from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategy } from "@/lib/types";

type AdaptiveFormState = Extract<LoadbalanceStrategyFormState, { strategy_type: "adaptive" }>;
type LegacyFormState = Extract<LoadbalanceStrategyFormState, { strategy_type: "legacy" }>;
type LegacyLoadbalanceStrategy = Extract<LoadbalanceStrategy, { strategy_type: "legacy" }>;

function buildAdaptiveRoutingPolicy(overrides: Record<string, unknown> = {}) {
  return {
    kind: "adaptive" as const,
    routing_objective: "minimize_latency" as const,
    hedge: {
      enabled: false,
      delay_ms: 75,
      max_additional_attempts: 1,
    },
    circuit_breaker: {
      failure_status_codes: [429, 503, 504],
      base_open_seconds: 60,
      failure_threshold: 2,
      backoff_multiplier: 2,
      max_open_seconds: 900,
      jitter_ratio: 0.2,
      ban_mode: "off" as const,
      max_open_strikes_before_ban: 0,
      ban_duration_seconds: 0,
    },
    admission: {
      respect_qps_limit: true,
      respect_in_flight_limits: true,
    },
    ...overrides,
  };
}

function buildForm(
  overrides: Partial<LegacyFormState> = {},
): LegacyFormState {
  return {
    ...DEFAULT_LOADBALANCE_STRATEGY_FORM,
    name: "legacy-round-robin",
    strategy_type: "legacy",
    legacy_strategy_type: "round-robin",
    auto_recovery: getDefaultAutoRecoveryDraft("round-robin"),
    ...overrides,
  };
}

function buildAdaptiveForm(
  overrides: Partial<AdaptiveFormState> = {},
): AdaptiveFormState {
  return {
    name: "Adaptive availability",
    strategy_type: "adaptive",
    timeout_policy: createDefaultTimeoutPolicy(),
    routing_policy: buildAdaptiveRoutingPolicy(),
    circuit_breaker_status_code_input: "",
    ...overrides,
  };
}

function buildStrategy(overrides: Partial<LegacyLoadbalanceStrategy> = {}): LegacyLoadbalanceStrategy {
  return {
    id: 9,
    profile_id: 1,
    name: "round-robin-primary",
    strategy_type: "legacy",
    timeout_policy: createDefaultTimeoutPolicy(),
    legacy_strategy_type: "round-robin",
    auto_recovery: { mode: "disabled" },
    attached_model_count: 0,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

describe("LoadbalanceStrategyDialog", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("shows an explicit strategy family selector for new strategies", () => {
    render(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={DEFAULT_LOADBALANCE_STRATEGY_FORM}
            loadbalanceStrategySaving={false}
            onClose={vi.fn()}
            onOpenChange={vi.fn()}
            onSave={vi.fn().mockResolvedValue(undefined)}
            open
            setLoadbalanceStrategyForm={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getAllByText("Strategy Family").length).toBeGreaterThan(0);
    expect(screen.getByText("Basics")).toBeInTheDocument();
    expect(screen.getByText("Strategy Behavior")).toBeInTheDocument();
    expect(screen.getByText("Reliability Controls")).toBeInTheDocument();
    expect(screen.getAllByText("Legacy Strategy Type").length).toBeGreaterThan(0);
    expect(screen.getByText("Auto Recovery")).toBeInTheDocument();

    const familySelect = screen.getByRole("combobox", { name: "Strategy Family" });
    expect(familySelect).toHaveTextContent("Legacy strategy");
    expect(familySelect).toHaveClass("w-full", "min-w-0", "max-w-full");
  });

  it("uses the shared large-dialog shell with one main scroll region and a pinned footer", () => {
    render(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={DEFAULT_LOADBALANCE_STRATEGY_FORM}
            loadbalanceStrategySaving={false}
            onClose={vi.fn()}
            onOpenChange={vi.fn()}
            onSave={vi.fn().mockResolvedValue(undefined)}
            open
            setLoadbalanceStrategyForm={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    const dialogContent = document.querySelector('[data-slot="dialog-content"]');
    const scrollBody = screen.getByTestId("loadbalance-strategy-scroll-body");

    expect(dialogContent).toHaveClass("h-[min(94vh,56rem)]", "max-w-3xl", "overflow-hidden");
    expect(scrollBody).toHaveClass("flex", "flex-col", "gap-6", "px-6", "py-5", "sm:px-7");
    expect(screen.queryByTestId("loadbalance-strategy-summary-card")).not.toBeInTheDocument();
  });

  it("shows the default adaptive routing objective after switching families inside the open dialog", async () => {
    function TestHarness() {
      const [formState, setFormState] = useState<LoadbalanceStrategyFormState>(
        DEFAULT_LOADBALANCE_STRATEGY_FORM,
      );

      return (
        <LocaleProvider>
          <TooltipProvider>
            <LoadbalanceStrategyDialog
              editingLoadbalanceStrategy={null}
              loadbalanceStrategyForm={formState}
              loadbalanceStrategySaving={false}
              onClose={vi.fn()}
              onOpenChange={vi.fn()}
              onSave={vi.fn().mockResolvedValue(undefined)}
              open
              setLoadbalanceStrategyForm={setFormState}
            />
          </TooltipProvider>
        </LocaleProvider>
      );
    }

    render(<TestHarness />);

    fireEvent.click(screen.getByRole("combobox", { name: "Strategy Family" }));
    fireEvent.click(screen.getByRole("option", { name: "Adaptive strategy" }));

    await waitFor(() => {
      expect(screen.getByRole("combobox", { name: "Routing Policy" })).toHaveTextContent(
        "Minimize latency",
      );
    });
  });

  it("renders localized dual-strategy dialog copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    render(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={DEFAULT_LOADBALANCE_STRATEGY_FORM}
            loadbalanceStrategySaving={false}
            onClose={vi.fn()}
            onOpenChange={vi.fn()}
            onSave={vi.fn().mockResolvedValue(undefined)}
            open
            setLoadbalanceStrategyForm={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getByText("新增负载均衡策略")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存策略" })).toBeInTheDocument();
    expect(screen.getAllByText("策略家族").length).toBeGreaterThan(0);
    expect(screen.getByText("基础信息")).toBeInTheDocument();
    expect(screen.getByText("策略行为")).toBeInTheDocument();
    expect(screen.getByText("可靠性控制")).toBeInTheDocument();
    expect(screen.getAllByText("传统策略类型").length).toBeGreaterThan(0);
  });

  it("submits through a real form with adaptive routing_policy fields when the adaptive family is selected", () => {
    const onSave = vi.fn().mockResolvedValue(undefined);

    render(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={buildAdaptiveForm()}
            loadbalanceStrategySaving={false}
            onClose={vi.fn()}
            onOpenChange={vi.fn()}
            onSave={onSave}
            open
            setLoadbalanceStrategyForm={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getByLabelText("Name")).toHaveAttribute("name", "name");
    expect(screen.getByText("Basics")).toBeInTheDocument();
    expect(screen.getByText("Strategy Behavior")).toBeInTheDocument();
    expect(screen.getByText("Reliability Controls")).toBeInTheDocument();
    expect(screen.getAllByText("Routing Policy").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Minimize latency").length).toBeGreaterThan(0);
    expect(screen.queryByText("Auto Recovery")).not.toBeInTheDocument();

    const form = screen.getByRole("button", { name: "Save Strategy" }).closest("form");
    expect(form).not.toBeNull();

    fireEvent.submit(form!);

    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("shows legacy failover controls for legacy strategies and adaptive circuit-breaker controls for adaptive strategies", () => {
    const { rerender } = render(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={buildForm({
              legacy_strategy_type: "fill-first",
              auto_recovery: {
                mode: "enabled",
                status_codes: [429, 503],
                status_code_input: "",
                cooldown: {
                  base_seconds: 60,
                  failure_threshold: 2,
                  backoff_multiplier: 2,
                  max_cooldown_seconds: 900,
                  jitter_ratio: 0.2,
                },
                ban: {
                  mode: "temporary",
                  max_cooldown_strikes_before_ban: 3,
                  ban_duration_seconds: 1800,
                },
              },
            })}
            loadbalanceStrategySaving={false}
            onClose={vi.fn()}
            onOpenChange={vi.fn()}
            onSave={vi.fn().mockResolvedValue(undefined)}
            open
            setLoadbalanceStrategyForm={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getByLabelText("Failure Status Codes")).toBeInTheDocument();
    expect(screen.getByLabelText("Ban Duration (seconds)")).toBeInTheDocument();

    rerender(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={buildAdaptiveForm({
              routing_policy: buildAdaptiveRoutingPolicy({
                circuit_breaker: {
                  failure_status_codes: [429, 503],
                  base_open_seconds: 30,
                  failure_threshold: 3,
                  backoff_multiplier: 1.5,
                  max_open_seconds: 600,
                  jitter_ratio: 0.15,
                  ban_mode: "temporary",
                  max_open_strikes_before_ban: 4,
                  ban_duration_seconds: 1800,
                },
              }),
            })}
            loadbalanceStrategySaving={false}
            onClose={vi.fn()}
            onOpenChange={vi.fn()}
            onSave={vi.fn().mockResolvedValue(undefined)}
            open
            setLoadbalanceStrategyForm={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getAllByText("Routing Policy").length).toBeGreaterThan(0);
    expect(screen.getByLabelText("Failure Status Codes")).toBeInTheDocument();
    expect(screen.getByLabelText("Base Open Window (seconds)")).toBeInTheDocument();
    expect(screen.getByLabelText("Failure Threshold")).toBeInTheDocument();
    expect(screen.getByLabelText("Backoff Multiplier")).toBeInTheDocument();
    expect(screen.getByLabelText("Max Open Window (seconds)")).toBeInTheDocument();
    expect(screen.getByLabelText("Jitter Ratio")).toBeInTheDocument();
    expect(screen.getByLabelText("Ban Mode")).toBeInTheDocument();
    expect(screen.getByLabelText("Ban Duration (seconds)")).toBeInTheDocument();
    expect(screen.queryByText("Auto Recovery")).not.toBeInTheDocument();
  });

  it("turns the delete dialog into a blocker when the strategy is still attached to native models", () => {
    render(
      <LocaleProvider>
        <DeleteLoadbalanceStrategyDialog
          deleteLoadbalanceStrategyConfirm={buildStrategy({ attached_model_count: 2 })}
          loadbalanceStrategyDeleting={false}
          onClose={vi.fn()}
          onDelete={vi.fn().mockResolvedValue(undefined)}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Deletion summary")).toBeInTheDocument();
    expect(screen.getByText("round-robin-primary")).toBeInTheDocument();
    expect(screen.getByText("This strategy is attached to 2 native models and cannot be deleted yet.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
  });

  it("keeps an explicit destructive confirm path when the strategy can be deleted", () => {
    render(
      <LocaleProvider>
        <DeleteLoadbalanceStrategyDialog
          deleteLoadbalanceStrategyConfirm={buildStrategy()}
          loadbalanceStrategyDeleting={false}
          onClose={vi.fn()}
          onDelete={vi.fn().mockResolvedValue(undefined)}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Deletion summary")).toBeInTheDocument();
    expect(screen.getByText("This action cannot be undone.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Delete" })).toBeEnabled();
  });
});
