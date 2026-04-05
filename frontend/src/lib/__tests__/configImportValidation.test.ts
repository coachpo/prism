import { describe, expect, it } from "vitest";
import type { ApiFamily, Vendor } from "../types";
import { ConfigImportSchema } from "../configImportValidation";

function buildAutoRecoveryDisabled() {
  return {
    mode: "disabled" as const,
  };
}

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
  };
}

function buildV2ProfileBundle() {
  return {
    version: 2,
    bundle_kind: "profile_config" as const,
    exported_at: "2026-04-04T15:00:00Z",
    vendor_refs: [
      {
        key: "openai",
        name_hint: "OpenAI",
        description_hint: "OpenAI vendor",
        icon_key_hint: "openai",
      },
    ],
    endpoints: [
      {
        name: "openai-main",
        base_url: "https://api.openai.com",
        api_key_secret_ref: "endpoint:openai-main:api_key",
        position: 0,
      },
    ],
    pricing_templates: [],
    loadbalance_strategies: [
      {
        name: "single-primary",
        strategy_type: "legacy" as const,
        legacy_strategy_type: "single" as const,
        auto_recovery: buildAutoRecoveryDisabled(),
      },
    ],
    models: [
      {
        vendor_key: "openai",
        api_family: "openai" as const,
        model_id: "gpt-4o",
        display_name: "GPT-4o",
        model_type: "native" as const,
        proxy_targets: [],
        loadbalance_strategy_name: "single-primary",
        is_enabled: true,
        connections: [
          {
            endpoint_name: "openai-main",
            pricing_template_name: null,
            is_active: true,
            priority: 0,
            name: "Primary",
            auth_type: "openai",
            custom_headers: { "X-Org": "my-org" },
          },
        ],
      },
    ],
    profile_settings: {
      report_currency_code: "USD",
      report_currency_symbol: "$",
      timezone_preference: "Europe/Helsinki",
      endpoint_fx_mappings: [],
    },
    header_blocklist_rules: [],
    secret_payload: {
      kind: "encrypted" as const,
      cipher: "fernet-v1" as const,
      key_id: "sha256:test",
      entries: [
        {
          ref: "endpoint:openai-main:api_key",
          ciphertext: "enc:test-ciphertext",
        },
      ],
    },
  };
}

function getIssuePairs(payload: unknown) {
  const result = ConfigImportSchema.safeParse(payload);

  expect(result.success).toBe(false);
  if (result.success) {
    throw new Error("Expected config import validation to fail");
  }

  return result.error.issues.map(({ path, message }) => ({ path, message }));
}

describe("ConfigImportSchema", () => {
  it("accepts the v2 profile-config bundle format", () => {
    const vendor: Vendor = {
      id: 1,
      key: "openai",
      name: "OpenAI",
      description: "OpenAI vendor",
      icon_key: "openai",
      audit_enabled: true,
      audit_capture_bodies: false,
      created_at: "2026-03-25T08:00:00Z",
      updated_at: "2026-03-25T08:00:00Z",
    };
    const apiFamily: ApiFamily = "openai";
    const result = ConfigImportSchema.safeParse(buildV2ProfileBundle());

    expect(vendor.key).toBe("openai");
    expect(apiFamily).toBe("openai");
    expect(result.success).toBe(true);
  });

  it("rejects the legacy v1 config format", () => {
    const issues = getIssuePairs({
      version: 1,
      vendors: [],
      endpoints: [],
      pricing_templates: [],
      loadbalance_strategies: [],
      models: [],
    });

    expect(issues).toContainEqual({
      path: ["version"],
      message: "Invalid input: expected 2",
    });
    expect(issues).toContainEqual({
      path: ["bundle_kind"],
      message: 'Invalid input: expected "profile_config"',
    });
  });

  it("accepts explicit null vendor hint icon keys in the v2 profile bundle", () => {
    const result = ConfigImportSchema.safeParse({
      ...buildV2ProfileBundle(),
      vendor_refs: [
        {
          key: "groq",
          name_hint: "Groq",
          description_hint: null,
          icon_key_hint: null,
        },
      ],
      models: [
        {
          ...buildV2ProfileBundle().models[0],
          vendor_key: "groq",
        },
      ],
    });

    expect(result.success).toBe(true);
  });

  it("accepts endpoints without API keys by using a null api_key_secret_ref", () => {
    const result = ConfigImportSchema.safeParse({
      ...buildV2ProfileBundle(),
      endpoints: [
        {
          name: "public-endpoint",
          base_url: "https://example.com",
          api_key_secret_ref: null,
          position: 0,
        },
      ],
      secret_payload: {
        kind: "encrypted",
        cipher: "fernet-v1",
        key_id: "sha256:test",
        entries: [],
      },
      models: [
        {
          ...buildV2ProfileBundle().models[0],
          connections: [
            {
              ...buildV2ProfileBundle().models[0].connections[0],
              endpoint_name: "public-endpoint",
            },
          ],
        },
      ],
    });

    expect(result.success).toBe(true);
  });

  it("accepts proxy models with empty proxy_targets in the v2 profile bundle", () => {
    const result = ConfigImportSchema.safeParse({
      ...buildV2ProfileBundle(),
      models: [
        {
          vendor_key: "openai",
          api_family: "openai",
          model_id: "gateway-proxy",
          display_name: "Gateway Proxy",
          model_type: "proxy",
          proxy_targets: [],
          loadbalance_strategy_name: null,
          is_enabled: true,
          connections: [],
        },
      ],
    });

    expect(result.success).toBe(true);
  });

  it("accepts adaptive routing_policy imports in the v2 profile bundle", () => {
    const result = ConfigImportSchema.safeParse({
      ...buildV2ProfileBundle(),
      loadbalance_strategies: [
        {
          name: "adaptive-primary",
          strategy_type: "adaptive",
          routing_policy: buildAdaptiveRoutingPolicy(),
        },
      ],
      models: [
        {
          ...buildV2ProfileBundle().models[0],
          loadbalance_strategy_name: "adaptive-primary",
        },
      ],
    });

    expect(result.success).toBe(true);
  });

  it("accepts endpoints that omit api_key_secret_ref for no-auth imports", () => {
    const payload = {
      ...buildV2ProfileBundle(),
      endpoints: [
        {
          name: "openai-main",
          base_url: "https://api.openai.com",
          position: 0,
        },
      ],
    };

    expect(ConfigImportSchema.safeParse(payload).success).toBe(true);
  });

  it("accepts a null profile_settings payload", () => {
    const result = ConfigImportSchema.safeParse({
      ...buildV2ProfileBundle(),
      profile_settings: null,
    });

    expect(result.success).toBe(true);
  });

  it("accepts backend-defaultable import fields when they are omitted", () => {
    const result = ConfigImportSchema.safeParse({
      version: 2,
      bundle_kind: "profile_config",
      vendor_refs: [{ key: "openai" }],
      endpoints: [{ name: "openai-main", base_url: "https://api.openai.com" }],
      pricing_templates: [
        {
          name: "Default Pricing",
          pricing_currency_code: "USD",
          input_price: "1.0",
          output_price: "2.0",
        },
      ],
      loadbalance_strategies: [
        {
          name: "single-primary",
          strategy_type: "legacy",
          legacy_strategy_type: "single",
          auto_recovery: buildAutoRecoveryDisabled(),
        },
      ],
      models: [
        {
          vendor_key: "openai",
          api_family: "openai",
          model_id: "gpt-4o",
        },
      ],
      secret_payload: {
        kind: "encrypted",
        cipher: "fernet-v1",
        key_id: "sha256:test",
        entries: [],
      },
    });

    expect(result.success).toBe(true);
  });

  it("requires secret_payload in the v2 profile bundle", () => {
    const payload = {
      ...buildV2ProfileBundle(),
    };
    delete (payload as { secret_payload?: unknown }).secret_payload;

    expect(getIssuePairs(payload)).toContainEqual({
      path: ["secret_payload"],
      message: "Invalid input: expected object, received undefined",
    });
  });
});
