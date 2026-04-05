import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

function buildAdaptiveRoutingPolicy(overrides: Record<string, unknown> = {}) {
  return {
    kind: "adaptive",
    routing_objective: "minimize_latency",
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
      ban_mode: "off",
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

describe("management api compatibility", () => {
  it("keeps legacy loadbalance strategy payloads in their merged family contract", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            {
              id: 12,
              profile_id: 3,
              name: "legacy-round-robin",
              strategy_type: "legacy",
              legacy_strategy_type: "round-robin",
              auto_recovery: {
                mode: "enabled",
                status_codes: [503, 429],
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
              attached_model_count: 2,
              created_at: "2026-03-25T08:00:00Z",
              updated_at: "2026-03-25T08:05:00Z",
            },
          ]),
          { status: 200 },
        ),
      ),
    );

    const { loadbalanceStrategies } = await import("../api/management");

    await expect(loadbalanceStrategies.list()).resolves.toEqual([
      {
        id: 12,
        profile_id: 3,
        name: "legacy-round-robin",
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
        attached_model_count: 2,
        created_at: "2026-03-25T08:00:00Z",
        updated_at: "2026-03-25T08:05:00Z",
      },
    ]);
  });

  it("accepts legacy single strategy payloads that enable failover recovery", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            {
              id: 13,
              profile_id: 3,
              name: "legacy-single",
              strategy_type: "legacy",
              legacy_strategy_type: "single",
              auto_recovery: {
                mode: "enabled",
                status_codes: [503, 429],
                cooldown: {
                  base_seconds: 45,
                  failure_threshold: 4,
                  backoff_multiplier: 3.5,
                  max_cooldown_seconds: 720,
                  jitter_ratio: 0.35,
                },
                ban: {
                  mode: "off",
                },
              },
              attached_model_count: 1,
              created_at: "2026-03-25T08:00:00Z",
              updated_at: "2026-03-25T08:05:00Z",
            },
          ]),
          { status: 200 },
        ),
      ),
    );

    const { loadbalanceStrategies } = await import("../api/management");

    await expect(loadbalanceStrategies.list()).resolves.toEqual([
      {
        id: 13,
        profile_id: 3,
        name: "legacy-single",
        strategy_type: "legacy",
        legacy_strategy_type: "single",
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
            mode: "off",
          },
        },
        attached_model_count: 1,
        created_at: "2026-03-25T08:00:00Z",
        updated_at: "2026-03-25T08:05:00Z",
      },
    ]);
  });

  it("normalizes full adaptive nested model strategy summaries without truncating policy fields", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            id: 1,
            model_id: "gpt-5.4-mini",
            display_name: "GPT-5.4 Mini",
            model_type: "native",
            proxy_targets: [],
            loadbalance_strategy_id: 12,
            loadbalance_strategy: {
              id: 12,
              name: "adaptive-primary",
              strategy_type: "adaptive",
              routing_policy: buildAdaptiveRoutingPolicy(),
            },
            is_enabled: true,
            connections: [],
            created_at: "2026-03-25T08:00:00Z",
            updated_at: "2026-03-25T08:05:00Z",
          }),
          { status: 200 },
        ),
      ),
    );

    const { models } = await import("../api/management");

    await expect(models.get(1)).resolves.toMatchObject({
      loadbalance_strategy: {
        id: 12,
        name: "adaptive-primary",
        strategy_type: "adaptive",
        routing_policy: buildAdaptiveRoutingPolicy(),
      },
    });
  });
});
