import { z } from "zod";

const EndpointImportSchema = z.strictObject({
  name: z.string(),
  base_url: z.string(),
  api_key_secret_ref: z.string().nullable().optional(),
  position: z.number().int().min(0).nullable().optional(),
});

const PricingTemplateImportSchema = z.strictObject({
  name: z.string(),
  description: z.string().nullable().optional(),
  pricing_unit: z.literal("PER_1M").optional(),
  pricing_currency_code: z.string(),
  input_price: z.string(),
  output_price: z.string(),
  cached_input_price: z.string().nullable().optional(),
  cache_creation_price: z.string().nullable().optional(),
  reasoning_price: z.string().nullable().optional(),
  missing_special_token_price_policy: z
    .enum(["MAP_TO_OUTPUT", "ZERO_COST"])
    .optional(),
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
      jitter_ratio: z.number().min(0).max(1),
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
  deadline_budget_ms: z.number().int().min(1),
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
    jitter_ratio: z.number().min(0).max(1),
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
  qps_limit: z.number().int().min(1).nullable().optional(),
  max_in_flight_non_stream: z.number().int().min(1).nullable().optional(),
  max_in_flight_stream: z.number().int().min(1).nullable().optional(),
});

const ProxyTargetImportSchema = z.strictObject({
  target_model_id: z.string(),
  position: z.number().int().min(0),
});

const ModelImportSchema = z.strictObject({
  vendor_key: z.string(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string(),
  display_name: z.string().nullable().optional(),
  model_type: z.enum(["native", "proxy"]).optional(),
  proxy_targets: z.array(ProxyTargetImportSchema).optional(),
  loadbalance_strategy_name: z.string().nullable().optional(),
  is_enabled: z.boolean().optional(),
  connections: z.array(ConnectionImportSchema).optional(),
});

const HeaderBlocklistRuleExportSchema = z.strictObject({
  name: z.string(),
  match_type: z.enum(["exact", "prefix"]),
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

export const ConfigImportSchema = z.strictObject({
  version: z.literal(2),
  bundle_kind: z.literal("profile_config"),
  exported_at: z.string().optional(),
  vendor_refs: z.array(VendorRefImportSchema),
  endpoints: z.array(EndpointImportSchema),
  pricing_templates: z.array(PricingTemplateImportSchema),
  loadbalance_strategies: z.array(LoadbalanceStrategyImportSchema),
  models: z.array(ModelImportSchema),
  profile_settings: ProfileSettingsImportSchema.nullable().optional(),
  header_blocklist_rules: z.array(HeaderBlocklistRuleExportSchema).optional(),
  secret_payload: SecretPayloadSchema,
});

export type ConfigImportSchemaType = z.infer<typeof ConfigImportSchema>;
