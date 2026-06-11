import { z } from "zod"
import type { ModelFormData } from "@/pages/models/modelFormState"
import {
  getAccessTargetOptionKeys,
  validateModelFormData,
} from "@/pages/models/modelFormState"
import type { ModelAccessTargetMutation, ModelConfigListItem } from "@/lib/types"

export const modelAuthoringSchema = z.object({
  vendor_id: z.number().int().positive().nullable(),
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string().trim().min(1, "Model ID is required."),
  display_name: z.string(),
  loadbalance_strategy_id: z.number().int().positive().nullable(),
  context_window_tokens: z.string(),
  default_output_token_reserve: z.string(),
  max_context_utilization: z.string(),
  preferred_context_utilization_threshold: z.string(),
  context_overflow_promotion_target_id: z.string(),
  access_targets: z.custom<ModelAccessTargetMutation[]>((value) => Array.isArray(value)),
  is_enabled: z.boolean(),
  last_auto_display_name: z.string().nullable().optional(),
}) satisfies z.ZodType<ModelFormData>

export type ModelAuthoringValues = z.input<typeof modelAuthoringSchema>

export function validateModelAuthoringValues(
  values: ModelFormData,
  availableTargets: Pick<ModelConfigListItem, "model_id">[] = [],
) {
  const parsed = modelAuthoringSchema.safeParse(values)
  if (!parsed.success) {
    return parsed.error.issues[0]?.message ?? "Model form is invalid."
  }
  return validateModelFormData(parsed.data, getAccessTargetOptionKeys(availableTargets))
}
