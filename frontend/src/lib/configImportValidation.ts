import { z } from "zod";

const EndpointImportSchema = z.strictObject({
  name: z.string(),
  base_url: z.string(),
  api_key_secret_ref: z.string().nullable().optional(),
  position: z.number().int().min(0).nullable().optional(),
});

const OpenAIProbeEndpointVariantImportSchema = z.enum([
  "responses_minimal",
  "responses_reasoning_none",
  "chat_completions_minimal",
  "chat_completions_reasoning_none",
]);

const componentPricingDecimalPattern = /^\d+(\.\d+)?$/;

// Bundle v1 keeps all component prices as concrete strings.
// Missing, null, blank, and whitespace-only inputs normalize to "0" before decimal validation.
const ComponentPricingImportSchema = z.preprocess(
  (value) => {
    if (value === null || value === undefined) {
      return "0";
    }

    if (typeof value !== "string") {
      return value;
    }

    const trimmed = value.trim();
    return trimmed === "" ? "0" : trimmed;
  },
  z.string().regex(componentPricingDecimalPattern, "must be a non-negative decimal string"),
);

const normalizeBasePricingImportSchema = z.preprocess(
  (value) => {
    if (value === null || value === undefined) {
      return "0";
    }

    if (typeof value !== "string") {
      return value;
    }

    const trimmed = value.trim();
    return trimmed === "" ? "0" : trimmed;
  },
  z.string().regex(componentPricingDecimalPattern, "must be a non-negative decimal string"),
);

const PricingTemplateImportSchema = z.strictObject({
  name: z.string(),
  description: z.string().nullable().optional(),
  pricing_unit: z.literal("PER_1M").optional(),
  pricing_currency_code: z.string(),
  input_price: normalizeBasePricingImportSchema,
  output_price: normalizeBasePricingImportSchema,
  cached_input_price: ComponentPricingImportSchema,
  cache_creation_price: ComponentPricingImportSchema,
  reasoning_price: ComponentPricingImportSchema,
  version: z.number().int().min(1).optional(),
});

const AutoRecoveryImportSchema = z.union([
  z.strictObject({ mode: z.literal("disabled") }),
  z.strictObject({
    mode: z.literal("enabled"),
    status_codes: z.array(z.number().int().min(100).max(599)).min(1),
    cooldown: z.strictObject({
      base_seconds: z.number().int().min(0),
      failure_threshold: z.number().int().min(1).max(50),
      backoff_multiplier: z.number().min(1).max(10),
      max_cooldown_seconds: z.number().int().min(1).max(86_400),
    }),
    ban: z.union([
      z.strictObject({
        mode: z.literal("off"),
        max_cooldown_strikes_before_ban: z.literal(0).optional(),
        ban_duration_seconds: z.literal(0).optional(),
      }),
      z.strictObject({
        mode: z.literal("manual"),
        max_cooldown_strikes_before_ban: z.number().int().min(1).max(100),
        ban_duration_seconds: z.literal(0).optional(),
      }),
      z.strictObject({
        mode: z.literal("temporary"),
        max_cooldown_strikes_before_ban: z.number().int().min(1).max(100),
        ban_duration_seconds: z.number().int().min(1).max(86_400),
      }),
    ]),
  }),
]);

const AdaptiveRoutingPolicyImportSchema = z.strictObject({
  kind: z.literal("adaptive"),
  routing_objective: z.enum(["maximize_availability", "minimize_latency"]),
  hedge: z.strictObject({
    enabled: z.boolean(),
    delay_ms: z.number().int().min(0),
    max_additional_attempts: z.number().int().min(1),
  }),
  circuit_breaker: z.strictObject({
    failure_status_codes: z.array(z.number().int().min(100).max(599)).min(1),
    base_open_seconds: z.number().min(0),
    failure_threshold: z.number().int().min(1),
    backoff_multiplier: z.number().min(1),
    max_open_seconds: z.number().int().min(1),
    ban_mode: z.enum(["off", "temporary", "manual"]),
    max_open_strikes_before_ban: z.number().int().min(0),
    ban_duration_seconds: z.number().int().min(0),
  }),
  admission: z.strictObject({
    respect_qps_limit: z.boolean(),
    respect_in_flight_limits: z.boolean(),
  }),
});

const LoadbalanceStrategyImportSchema = z.discriminatedUnion("strategy_type", [
  z.strictObject({
    name: z.string(),
    strategy_type: z.literal("legacy"),
    legacy_strategy_type: z.enum(["single", "fill-first", "round-robin"]),
    auto_recovery: AutoRecoveryImportSchema,
    routing_policy: z.null().optional(),
  }),
  z.strictObject({
    name: z.string(),
    strategy_type: z.literal("adaptive"),
    routing_policy: AdaptiveRoutingPolicyImportSchema,
    legacy_strategy_type: z.null().optional(),
    auto_recovery: z.null().optional(),
  }),
]);

const ConnectionImportSchema = z.strictObject({
  endpoint_name: z.string(),
  pricing_template_name: z.string().nullable().optional(),
  is_active: z.boolean().optional(),
  priority: z.number().int().min(0).optional(),
  name: z.string().nullable().optional(),
  auth_type: z.enum(["openai", "anthropic", "gemini"]).nullable().optional(),
  custom_headers: z.record(z.string(), z.string()).nullable().optional(),
  openai_probe_endpoint_variant: OpenAIProbeEndpointVariantImportSchema.nullable().optional(),
  qps_limit: z.number().int().min(1).nullable().optional(),
  max_in_flight_non_stream: z.number().int().min(1).nullable().optional(),
  max_in_flight_stream: z.number().int().min(1).nullable().optional(),
});

const ProxyTargetImportSchema = z.strictObject({
  target_model_id: z.string(),
  position: z.number().int().min(0),
  weight: z.number().int().min(1),
  target_priority: z.number().int().min(0),
});

const NativeModelImportSchema = z.strictObject({
  vendor_key: z.string().nullable().optional(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string(),
  display_name: z.string().nullable().optional(),
  model_type: z.literal("native"),
  proxy_selection_strategy: z.null().optional(),
  proxy_targets: z.tuple([]),
  loadbalance_strategy_name: z.string(),
  is_enabled: z.boolean().optional(),
  connections: z.array(ConnectionImportSchema),
});

const ProxyModelImportSchema = z.strictObject({
  vendor_key: z.string().nullable().optional(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string(),
  display_name: z.string().nullable().optional(),
  model_type: z.literal("proxy"),
  proxy_selection_strategy: z.enum(["ordered_fallback", "weighted_static", "priority_static"]),
  proxy_targets: z.array(ProxyTargetImportSchema),
  loadbalance_strategy_name: z.null(),
  is_enabled: z.boolean().optional(),
  connections: z.tuple([]),
}).superRefine((model, context) => {
  const seenTargetModelIds = new Set();

  for (const [index, target] of model.proxy_targets.entries()) {
    if (target.position !== index) {
      context.addIssue({
        code: "custom",
        path: ["proxy_targets", index, "position"],
        message: "proxy_targets positions must be contiguous starting at 0",
      });
    }

    if (seenTargetModelIds.has(target.target_model_id)) {
      context.addIssue({
        code: "custom",
        path: ["proxy_targets", index, "target_model_id"],
        message: "proxy_targets must contain unique target_model_id values",
      });
    }

    seenTargetModelIds.add(target.target_model_id);
  }
});

const ModelImportSchema = z.discriminatedUnion("model_type", [
  NativeModelImportSchema,
  ProxyModelImportSchema,
]);

const HeaderBlocklistRuleExportSchema = z.strictObject({
  name: z.string(),
  match_type: z.enum(["exact", "prefix"]),
  pattern: z.string(),
  enabled: z.boolean(),
});

const UserAgentRuleTransportSchema = z.strictObject({
  name: z.string(),
  pattern: z.string(),
  enabled: z.boolean(),
});

const EndpointFxRateImportSchema = z.strictObject({
  model_id: z.string(),
  endpoint_name: z.string(),
  fx_rate: z.string(),
});

const ProfileSettingsImportSchema = z.strictObject({
  report_currency_code: z.string().optional(),
  report_currency_symbol: z.string().optional(),
  timezone_preference: z.string().nullable().optional(),
  endpoint_fx_mappings: z.array(EndpointFxRateImportSchema).optional(),
});

const VendorRefImportSchema = z.strictObject({
  key: z.string(),
  name_hint: z.string().nullable().optional(),
  description_hint: z.string().nullable().optional(),
  icon_key_hint: z.string().nullable().optional(),
});

const SecretPayloadEntrySchema = z.strictObject({
  ref: z.string(),
  ciphertext: z.string(),
});

const SecretPayloadSchema = z.strictObject({
  kind: z.literal("encrypted"),
  cipher: z.literal("fernet-v1"),
  key_id: z.string(),
  entries: z.array(SecretPayloadEntrySchema),
});

const VendorCatalogRowSchema = z.strictObject({
  key: z.string(),
  name: z.string(),
  description: z.string().nullable(),
  icon_key: z.string().nullable(),
  audit_enabled: z.boolean(),
  audit_capture_bodies: z.boolean(),
});

export const ConfigImportSchema = z.strictObject({
  version: z.literal(1),
  bundle_kind: z.literal("profile_config"),
  exported_at: z.string().optional(),
  vendor_refs: z.array(VendorRefImportSchema),
  endpoints: z.array(EndpointImportSchema),
  pricing_templates: z.array(PricingTemplateImportSchema),
  loadbalance_strategies: z.array(LoadbalanceStrategyImportSchema),
  models: z.array(ModelImportSchema),
  profile_settings: ProfileSettingsImportSchema.nullable().optional(),
  header_blocklist_rules: z.array(HeaderBlocklistRuleExportSchema).optional(),
  user_agent_client_rules: z.array(UserAgentRuleTransportSchema).optional(),
  secret_payload: SecretPayloadSchema,
});

export const VendorCatalogImportSchema = z.strictObject({
  version: z.literal(1),
  bundle_kind: z.literal("vendor_catalog"),
  exported_at: z.string().optional(),
  vendors: z.array(VendorCatalogRowSchema),
});

export type ConfigImportSchemaType = z.infer<typeof ConfigImportSchema>;
export type VendorCatalogImportSchemaType = z.infer<typeof VendorCatalogImportSchema>;
