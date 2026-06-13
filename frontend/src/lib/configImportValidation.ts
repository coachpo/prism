import { z } from "zod";

type ValidationIssueLike = {
  path: readonly PropertyKey[];
  message: string;
};

export function formatValidationIssuePath(path: readonly PropertyKey[]) {
  let formatted = "";

  path.forEach((segment) => {
    if (typeof segment === "number") {
      formatted += `[${segment}]`;
      return;
    }

    const key = String(segment);
    formatted += formatted.length === 0 ? key : `.${key}`;
  });

  return formatted;
}

export function formatValidationIssues(issues: readonly ValidationIssueLike[]) {
  return issues
    .map((issue) => {
      const formattedPath = formatValidationIssuePath(issue.path);
      return formattedPath.length > 0 ? `${formattedPath}: ${issue.message}` : issue.message;
    })
    .join(", ");
}

export function formatRoutingPlanValidationIssues(detail: unknown): string | null {
  if (!detail || typeof detail !== "object") {
    return null;
  }

  const routingPlanIssues = (detail as { routing_plan_issues?: unknown }).routing_plan_issues;
  if (!Array.isArray(routingPlanIssues) || routingPlanIssues.length === 0) {
    return null;
  }

  const formattedIssues = routingPlanIssues.flatMap((issue) => {
    if (!issue || typeof issue !== "object") {
      return [];
    }

    const path = (issue as { path?: unknown }).path;
    const message = (issue as { message?: unknown }).message;
    if (typeof message !== "string" || message.trim().length === 0) {
      return [];
    }

    const normalizedMessage = message.trim();
    if (typeof path === "string" && path.trim().length > 0) {
      return [`${path.trim()}: ${normalizedMessage}`];
    }

    return [normalizedMessage];
  });

  return formattedIssues.length > 0 ? formattedIssues.join(", ") : null;
}

const EndpointImportSchema = z.strictObject({
  name: z.string(),
  base_url: z.string(),
  api_key_secret_ref: z.string().nullable().optional(),
  position: z.number().int().min(0).nullable().optional(),
});

const OpenAITextCapabilityImportSchema = z.enum([
  "responses_only",
  "chat_completions_only",
  "dual_native",
]);

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
  legacy_strategy_type: z.enum(["single", "fill-first", "round-robin", "cheapest_eligible_context"]).nullable(),
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

const ContextWindowTokensImportSchema = z.number().int().min(1).nullable().optional();
const DefaultOutputTokenReserveImportSchema = z.number().int().min(1).nullable().optional();
const MaxContextUtilizationImportSchema = z.number().gt(0).max(1).nullable().optional();
const PreferredContextUtilizationThresholdImportSchema = z.number().gt(0).max(1).nullable().optional();
const FacadeSelectionPolicyImportSchema = z.literal("weighted_eligible_context").nullable().optional();
const FacadeFallbackPolicyImportSchema = z.literal("redistribute_ineligible_weight").nullable().optional();

function addPreferredThresholdIssue(
  context: z.RefinementCtx,
  maxContextUtilization: number | null | undefined,
  preferredContextUtilizationThreshold: number | null | undefined,
  fieldPrefix: readonly (string | number)[] = [],
) {
  if (
    typeof maxContextUtilization === "number"
    && typeof preferredContextUtilizationThreshold === "number"
    && preferredContextUtilizationThreshold > maxContextUtilization
  ) {
    context.addIssue({
      code: "custom",
      path: [...fieldPrefix, "preferred_context_utilization_threshold"],
      message: "preferred_context_utilization_threshold must be less than or equal to max_context_utilization",
    });
  }
}

const ConnectionImportSchema = z.strictObject({
  ref: z.string(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  endpoint_name: z.string(),
  context_window_tokens: ContextWindowTokensImportSchema,
  default_output_token_reserve: DefaultOutputTokenReserveImportSchema,
  max_context_utilization: MaxContextUtilizationImportSchema,
  preferred_context_utilization_threshold: PreferredContextUtilizationThresholdImportSchema,
  pricing_template_name: z.string().nullable().optional(),
  is_active: z.boolean().optional(),
  priority: z.number().int().min(0).optional(),
  name: z.string().nullable().optional(),
  auth_type: z.enum(["openai", "anthropic", "gemini"]).nullable().optional(),
  custom_headers: z.record(z.string(), z.string()).nullable().optional(),
  openai_text_capability: OpenAITextCapabilityImportSchema.nullable().optional(),
  openai_probe_endpoint_variant: OpenAIProbeEndpointVariantImportSchema.nullable().optional(),
  qps_limit: z.number().int().min(1).nullable().optional(),
  max_in_flight_non_stream: z.number().int().min(1).nullable().optional(),
  max_in_flight_stream: z.number().int().min(1).nullable().optional(),
}).superRefine((connection, context) => {
  addPreferredThresholdIssue(
    context,
    connection.max_context_utilization,
    connection.preferred_context_utilization_threshold,
  );

  if (connection.api_family === "openai") {
    if (!connection.openai_text_capability) {
      context.addIssue({
        code: "custom",
        path: ["openai_text_capability"],
        message: "OpenAI connections must include openai_text_capability",
      });
    }
    return;
  }

  if (connection.openai_text_capability != null) {
    context.addIssue({
      code: "custom",
      path: ["openai_text_capability"],
      message: "openai_text_capability is only valid for OpenAI connections",
    });
  }

  if (connection.openai_probe_endpoint_variant != null) {
    context.addIssue({
      code: "custom",
      path: ["openai_probe_endpoint_variant"],
      message: "openai_probe_endpoint_variant is only valid for OpenAI connections",
    });
  }
});

const AccessTargetImportSchema = z.strictObject({
  position: z.number().int().min(0),
  is_enabled: z.boolean(),
  target_type: z.enum(["model", "connection"]),
  connection_ref: z.string().nullable().optional(),
  target_model_id: z.string().nullable().optional(),
  weight: z.number().int().nullable().optional(),
  target_priority: z.number().int().nullable().optional(),
});

const ModelImportSchema = z.strictObject({
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string(),
  display_name: z.string().nullable().optional(),
  loadbalance_strategy_name: z.string(),
  context_window_tokens: ContextWindowTokensImportSchema,
  default_output_token_reserve: DefaultOutputTokenReserveImportSchema,
  max_context_utilization: MaxContextUtilizationImportSchema,
  preferred_context_utilization_threshold: PreferredContextUtilizationThresholdImportSchema,
  facade_enabled: z.boolean().optional(),
  facade_selection_policy: FacadeSelectionPolicyImportSchema,
  facade_fallback_policy: FacadeFallbackPolicyImportSchema,
  context_overflow_promotion_target_id: z.string().nullable().optional(),
  is_enabled: z.boolean().optional(),
  access_targets: z.array(AccessTargetImportSchema),
}).superRefine((model, context) => {
  addPreferredThresholdIssue(
    context,
    model.max_context_utilization,
    model.preferred_context_utilization_threshold,
  );
  const seenPositions = new Set<number>();
  const seenTargets = new Set<string>();

  for (const [index, target] of model.access_targets.entries()) {
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
        context.addIssue({
          code: "custom",
          path: ["access_targets", index, "connection_ref"],
          message: `Model '${model.model_id}' connection access target must include connection_ref`,
        });
      }
      if (target.target_model_id) {
        context.addIssue({
          code: "custom",
          path: ["access_targets", index, "target_model_id"],
          message: `Model '${model.model_id}' connection access target must not include target_model_id`,
        });
      }
      if (target.weight !== null && target.weight !== undefined) {
        context.addIssue({
          code: "custom",
          path: ["access_targets", index, "weight"],
          message: "weight must be omitted for terminal targets",
        });
      }
      if (target.target_priority !== null && target.target_priority !== undefined) {
        context.addIssue({
          code: "custom",
          path: ["access_targets", index, "target_priority"],
          message: "target_priority must be omitted for terminal targets",
        });
      }
      if (target.connection_ref) {
        const targetKey = `connection:${target.connection_ref}`;
        if (seenTargets.has(targetKey)) {
          context.addIssue({
            code: "custom",
            path: ["access_targets", index, "connection_ref"],
            message: `Model '${model.model_id}' has duplicate connection_ref access target '${target.connection_ref}'`,
          });
        }
        seenTargets.add(targetKey);
      }
      continue;
    }

    if (!target.target_model_id) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "target_model_id"],
        message: `Model '${model.model_id}' model access target must include target_model_id`,
      });
    }
    if (target.connection_ref) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "connection_ref"],
        message: `Model '${model.model_id}' model access target must not include connection_ref`,
      });
    }
    if (target.weight !== null && target.weight !== undefined && target.weight <= 0) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "weight"],
        message: "weight must be greater than 0",
      });
    }
    if (
      target.target_priority !== null
      && target.target_priority !== undefined
      && target.target_priority < 0
    ) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "target_priority"],
        message: "target_priority must be greater than or equal to 0",
      });
    }
    if (target.target_model_id === model.model_id) {
      context.addIssue({
        code: "custom",
        path: ["access_targets", index, "target_model_id"],
        message: `Model '${model.model_id}' access target cannot target itself`,
      });
    }
    if (target.target_model_id) {
      const targetKey = `model:${target.target_model_id}`;
      if (seenTargets.has(targetKey)) {
        context.addIssue({
          code: "custom",
          path: ["access_targets", index, "target_model_id"],
          message: `Model '${model.model_id}' has duplicate model access target '${target.target_model_id}'`,
        });
      }
      seenTargets.add(targetKey);
    }
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
  version: z.literal(3),
  bundle_kind: z.literal("profile_config"),
  exported_at: z.string().optional(),
  endpoints: z.array(EndpointImportSchema),
  pricing_templates: z.array(PricingTemplateImportSchema),
  connections: z.array(ConnectionImportSchema),
  loadbalance_strategies: z.array(LoadbalanceStrategyImportSchema),
  models: z.array(ModelImportSchema),
  profile_settings: ProfileSettingsImportSchema.nullable().optional(),
  header_blocklist_rules: z.array(HeaderBlocklistRuleExportSchema).optional(),
  user_agent_client_rules: z.array(UserAgentRuleTransportSchema).optional(),
  secret_payload: SecretPayloadSchema,
}).superRefine((bundle, context) => {
  const connectionOwners = new Map<string, string>();

  for (const [modelIndex, model] of bundle.models.entries()) {
    for (const [targetIndex, target] of model.access_targets.entries()) {
      if (target.target_type !== "connection" || !target.connection_ref) {
        continue;
      }

      const existingOwner = connectionOwners.get(target.connection_ref);
      const targetPath = ["models", modelIndex, "access_targets", targetIndex, "connection_ref"];
      if (existingOwner && existingOwner !== model.model_id) {
        context.addIssue({
          code: "custom",
          path: targetPath,
          message: `connection_ref '${target.connection_ref}' is owned by multiple models: model_id '${existingOwner}' and model_id '${model.model_id}'`,
        });
        continue;
      }

      connectionOwners.set(target.connection_ref, model.model_id);
    }
  }

  for (const [connectionIndex, connection] of bundle.connections.entries()) {
    if (connectionOwners.has(connection.ref)) {
      continue;
    }

    context.addIssue({
      code: "custom",
      path: ["connections", connectionIndex, "ref"],
      message: `Connection ref '${connection.ref}' must be owned by exactly one model access target`,
    });
  }
});

export type ConfigImportSchemaType = z.infer<typeof ConfigImportSchema>;
