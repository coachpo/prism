import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { LoadbalanceStrategy, ModelConfig, Vendor } from "@/lib/types";
import { ModelSettingsDialog } from "../ModelSettingsDialog";

function buildAdaptiveRoutingPolicy() {
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
    monitoring: {
      enabled: true,
      stale_after_seconds: 30,
      endpoint_ping_weight: 0.4,
      conversation_delay_weight: 0.35,
      failure_penalty_weight: 0.25,
    },
  };
}

describe("ModelSettingsDialog", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("shows separate vendor and api family fields for proxy models", () => {
    const vendor: Vendor = {
      id: 30,
      key: "together-ai",
      name: "Together AI",
      description: null,
      icon_key: null,
      audit_enabled: false,
      audit_capture_bodies: false,
      created_at: "",
      updated_at: "",
    };
    const model: ModelConfig = {
      id: 9,
      vendor_id: 30,
      vendor,
      api_family: "openai",
      model_id: "friendly-proxy",
      display_name: "Friendly Proxy",
      model_type: "proxy",
      proxy_targets: [{ target_model_id: "gpt-5.4", position: 0 }],
      loadbalance_strategy_id: null,
      loadbalance_strategy: null,
      is_enabled: true,
      connections: [],
      created_at: "",
      updated_at: "",
    };

    render(
      <LocaleProvider>
        <ModelSettingsDialog
          editLoadbalanceStrategyId=""
          isOpen={true}
          loadbalanceStrategies={[]}
          onOpenChange={vi.fn()}
          vendors={[vendor]}
          setEditLoadbalanceStrategyId={vi.fn()}
          handleEditModelSubmit={vi.fn()}
          model={model}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("Vendor")).toBeInTheDocument();
    expect(screen.getByText("API Family")).toBeInTheDocument();
    expect(screen.getByDisplayValue("friendly-proxy")).toBeInTheDocument();
    expect(screen.getByText("Vendor").parentElement).toHaveClass("min-w-0");
    expect(screen.getByText("API Family").parentElement).toHaveClass("min-w-0");
  });

  it("renders model settings copy from the Chinese locale catalog", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    const vendor: Vendor = {
      id: 30,
      key: "together-ai",
      name: "Together AI",
      description: null,
      icon_key: null,
      audit_enabled: false,
      audit_capture_bodies: false,
      created_at: "",
      updated_at: "",
    };
    const model: ModelConfig = {
      id: 9,
      vendor_id: 30,
      vendor,
      api_family: "openai",
      model_id: "friendly-proxy",
      display_name: "Friendly Proxy",
      model_type: "proxy",
      proxy_targets: [{ target_model_id: "gpt-5.4", position: 0 }],
      loadbalance_strategy_id: null,
      loadbalance_strategy: null,
      is_enabled: true,
      connections: [],
      created_at: "",
      updated_at: "",
    };

    render(
      <LocaleProvider>
        <ModelSettingsDialog
          editLoadbalanceStrategyId=""
          isOpen={true}
          loadbalanceStrategies={[]}
          onOpenChange={vi.fn()}
          vendors={[vendor]}
          setEditLoadbalanceStrategyId={vi.fn()}
          handleEditModelSubmit={vi.fn()}
          model={model}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("模型设置")).toBeInTheDocument();
    expect(screen.getByText("显示名称")).toBeInTheDocument();
    expect(screen.getByText("代理目标")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存更改" })).toBeInTheDocument();
  });

  it("shows legacy and adaptive assignment options with kind-aware summaries for native models", () => {
    const vendor: Vendor = {
      id: 7,
      key: "openai",
      name: "OpenAI",
      description: null,
      icon_key: null,
      audit_enabled: false,
      audit_capture_bodies: false,
      created_at: "",
      updated_at: "",
    };
    const model: ModelConfig = {
      id: 11,
      vendor_id: 7,
      vendor,
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
        auto_recovery: { mode: "disabled" },
      },
      is_enabled: true,
      connections: [],
      created_at: "",
      updated_at: "",
    };
    const strategies: LoadbalanceStrategy[] = [
      {
        id: 101,
        profile_id: 1,
        name: "round-robin-primary",
        strategy_type: "legacy",
        legacy_strategy_type: "round-robin",
        auto_recovery: { mode: "disabled" },
        attached_model_count: 1,
        created_at: "",
        updated_at: "",
      },
      {
        id: 102,
        profile_id: 1,
        name: "adaptive-availability",
        strategy_type: "adaptive",
        routing_policy: buildAdaptiveRoutingPolicy(),
        attached_model_count: 2,
        created_at: "",
        updated_at: "",
      },
    ];

    render(
      <LocaleProvider>
        <ModelSettingsDialog
          editLoadbalanceStrategyId="101"
          isOpen={true}
          loadbalanceStrategies={strategies}
          onOpenChange={vi.fn()}
          vendors={[vendor]}
          setEditLoadbalanceStrategyId={vi.fn()}
          handleEditModelSubmit={vi.fn()}
          model={model}
        />
      </LocaleProvider>,
    );

    expect(screen.getAllByText("round-robin-primary (Legacy strategy • Round robin)").length).toBeGreaterThan(0);
    expect(screen.getByText("adaptive-availability (Adaptive strategy • Minimize latency)")).toBeInTheDocument();
    screen.getAllByRole("combobox").forEach((combobox) => {
      expect(combobox).toHaveClass("w-full", "min-w-0", "max-w-full");
    });
  });

  it("keeps the selected strategy trigger bounded and single-line for long labels", () => {
    const vendor: Vendor = {
      id: 7,
      key: "openai",
      name: "OpenAI",
      description: null,
      icon_key: null,
      audit_enabled: false,
      audit_capture_bodies: false,
      created_at: "",
      updated_at: "",
    };
    const model: ModelConfig = {
      id: 11,
      vendor_id: 7,
      vendor,
      api_family: "openai",
      model_id: "gpt-5.4",
      display_name: "GPT-5.4",
      model_type: "native",
      proxy_targets: [],
      loadbalance_strategy_id: 102,
      loadbalance_strategy: {
        id: 102,
        name: "adaptive-availability-with-an-extremely-long-name-that-should-not-push-neighboring-controls-out-of-place",
        strategy_type: "adaptive",
        routing_policy: buildAdaptiveRoutingPolicy(),
      },
      is_enabled: true,
      connections: [],
      created_at: "",
      updated_at: "",
    };
    const strategies: LoadbalanceStrategy[] = [
      {
        id: 102,
        profile_id: 1,
        name: "adaptive-availability-with-an-extremely-long-name-that-should-not-push-neighboring-controls-out-of-place",
        strategy_type: "adaptive",
        routing_policy: buildAdaptiveRoutingPolicy(),
        attached_model_count: 2,
        created_at: "",
        updated_at: "",
      },
    ];

    render(
      <LocaleProvider>
        <ModelSettingsDialog
          editLoadbalanceStrategyId="102"
          isOpen={true}
          loadbalanceStrategies={strategies}
          onOpenChange={vi.fn()}
          vendors={[vendor]}
          setEditLoadbalanceStrategyId={vi.fn()}
          handleEditModelSubmit={vi.fn()}
          model={model}
        />
      </LocaleProvider>,
    );

    const strategyTrigger = screen.getByLabelText("Loadbalance Strategy");
    const strategyValue = strategyTrigger.querySelector('[data-slot="select-value"]');
    const strategyLabel = strategyValue?.firstElementChild;

    expect(strategyTrigger).toHaveClass("w-full", "min-w-0", "max-w-full");
    expect(strategyValue).not.toBeNull();
    expect(strategyLabel).toHaveTextContent(
      "adaptive-availability-with-an-extremely-long-name-that-should-not-push-neighboring-controls-out-of-place (Adaptive strategy • Minimize latency)",
    );
    expect(strategyLabel).toHaveClass("block", "min-w-0", "truncate");
  });
});
