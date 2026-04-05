import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { LoadbalanceStrategyDialog } from "../LoadbalanceStrategyDialog";
import {
  DEFAULT_LOADBALANCE_STRATEGY_FORM,
  getDefaultAutoRecoveryDraft,
  type LoadbalanceStrategyFormState,
} from "../loadbalanceStrategyFormState";

type LegacyFormState = Extract<LoadbalanceStrategyFormState, { strategy_type: "legacy" }>;

function buildAdaptiveRoutingPolicy(overrides: Record<string, unknown> = {}) {
  return {
    kind: "adaptive" as const,
    routing_objective: "minimize_latency" as const,
    deadline_budget_ms: 1500,
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

    expect(screen.getByText("Strategy Family")).toBeInTheDocument();
    expect(screen.getByText("Legacy Strategy Type")).toBeInTheDocument();
    expect(screen.getByText("Auto Recovery")).toBeInTheDocument();

    const familySelect = screen.getByRole("combobox", { name: "Strategy Family" });
    expect(familySelect).toHaveTextContent("Legacy strategy");
    expect(familySelect).toHaveClass("w-full", "min-w-0", "max-w-full");
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
    expect(screen.getByText("策略家族")).toBeInTheDocument();
    expect(screen.getByText("传统策略类型")).toBeInTheDocument();
  });

  it("submits through a real form with adaptive routing_policy fields when the adaptive family is selected", () => {
    const onSave = vi.fn().mockResolvedValue(undefined);

    render(
      <LocaleProvider>
        <TooltipProvider>
          <LoadbalanceStrategyDialog
            editingLoadbalanceStrategy={null}
            loadbalanceStrategyForm={{
              name: "Adaptive availability",
              strategy_type: "adaptive",
              routing_policy: buildAdaptiveRoutingPolicy(),
            }}
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
    expect(screen.getByText("Routing Policy")).toBeInTheDocument();
    expect(screen.getAllByText("Minimize latency").length).toBeGreaterThan(0);
    expect(screen.queryByText("Auto Recovery")).not.toBeInTheDocument();

    const form = screen.getByRole("button", { name: "Save Strategy" }).closest("form");
    expect(form).not.toBeNull();

    fireEvent.submit(form!);

    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("shows legacy failover controls for legacy strategies and hides them for adaptive strategies", () => {
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
            loadbalanceStrategyForm={{
              name: "Adaptive availability",
              strategy_type: "adaptive",
              routing_policy: buildAdaptiveRoutingPolicy(),
            }}
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

    expect(screen.queryByLabelText("Failure Status Codes")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Ban Duration (seconds)")).not.toBeInTheDocument();
    expect(screen.getByText("Routing Policy")).toBeInTheDocument();
  });
});
