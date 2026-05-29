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

// Bundle v3/current contract keeps all component prices as concrete strings.
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

const LoadbalanceStrategyImportSchema = z.strictObject({
  name: z.string(),
  legacy_strategy_type: z.enum(["single", "fill-first", "round-robin"]).nullable(),
  failure_status_codes: z.array(z.number().int().min(100).max(599)),
  ban_mode: z.enum(["off", "temporary", "until_reset"]).nullable(),
  retry_base_delay_ms: z.number().int().min(0).nullable(),
  retry_backoff_multiplier: z.number().min(1).nullable(),
  retry_jitter_ratio: z.number().min(0).max(1).nullable(),
  retry_max_delay_ms: z.number().int().min(1).nullable(),
  cycle_retry_attempt_limit: z.number().int().min(1).nullable(),
  ban_cumulative_retry_attempt_threshold: z.number().int().min(0).nullable(),
  ban_duration_seconds: z.number().int().min(0).nullable(),
});

const ConnectionImportSchema = z.strictObject({
  ref: z.string(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
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

const AccessTargetImportSchema = z.strictObject({
  position: z.number().int().min(0),
  is_enabled: z.boolean(),
  target_type: z.enum(["model", "connection"]),
  connection_ref: z.string().nullable().optional(),
  target_model_id: z.string().nullable().optional(),
});

const ModelImportSchema = z.strictObject({
  vendor_key: z.string().nullable().optional(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string(),
  display_name: z.string().nullable().optional(),
  loadbalance_strategy_name: z.string(),
  is_enabled: z.boolean().optional(),
  access_targets: z.array(AccessTargetImportSchema),
}).superRefine((model, context) => {
  const seenPositions = new Set<number>();
  const seenTargets = new Set<string>();

  for (const [index, target] of model.access_targets.entries()) {
    if (target.position !== index) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "position"],
        message: "access_targets positions must be contiguous starting at 0",
      });
    }
    if (seenPositions.has(target.position)) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "position"],
        message: "access_targets must contain unique position values",
      });
    }
    seenPositions.add(target.position);

    if (target.target_type === "connection") {
      if (!target.connection_ref) {
        context.addIssue({ code: "custom", path: ["access_targets", index, "connection_ref"], message: "connection_ref is required for connection targets" });
      }
      if (target.target_model_id) {
        context.addIssue({ code: "custom", path: ["access_targets", index, "target_model_id"], message: "target_model_id must be omitted for connection targets" });
      }
    }
    if (target.target_type === "model") {
      if (!target.target_model_id) {
        context.addIssue({ code: "custom", path: ["access_targets", index, "target_model_id"], message: "target_model_id is required for model targets" });
      }
      if (target.connection_ref) {
        context.addIssue({ code: "custom", path: ["access_targets", index, "connection_ref"], message: "connection_ref must be omitted for model targets" });
      }
      if (target.target_model_id === model.model_id) {
        context.addIssue({ code: "custom", path: ["access_targets", index, "target_model_id"], message: "access target cannot target itself" });
      }
    }

    const targetKey = `${target.target_type}:${target.connection_ref ?? target.target_model_id ?? ""}`;
    if (seenTargets.has(targetKey)) {
      context.addIssue({ code: "custom", path: ["access_targets", index], message: "access_targets must contain unique target references" });
    }
    seenTargets.add(targetKey);
  }
});

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
  connection_ref: z.string(),
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
  version: z.literal(3),
  bundle_kind: z.literal("profile_config"),
  exported_at: z.string().optional(),
  vendor_refs: z.array(VendorRefImportSchema),
  endpoints: z.array(EndpointImportSchema),
  pricing_templates: z.array(PricingTemplateImportSchema),
  connections: z.array(ConnectionImportSchema),
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
