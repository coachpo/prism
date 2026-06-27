import { z } from "zod"
import type { ModelFormData } from "@/pages/models/modelFormState"
import {
  validateModelFormData,
} from "@/pages/models/modelFormState"

export const modelAuthoringSchema = z.object({
  api_family: z.enum(["openai", "anthropic", "gemini"]),
  model_id: z.string().trim().min(1, "Model ID is required."),
  display_name: z.string(),
  openai_accepted_format: z.union([
    z.enum(["responses_only", "chat_completions_only", "dual_native"]),
    z.literal(""),
  ]),
  loadbalance_strategy_id: z.number().int().positive().nullable(),
  is_enabled: z.boolean(),
  last_auto_display_name: z.string().nullable().optional(),
}) satisfies z.ZodType<ModelFormData>

export type ModelAuthoringValues = z.input<typeof modelAuthoringSchema>

export function validateModelAuthoringValues(
  values: ModelFormData,
) {
  const parsed = modelAuthoringSchema.safeParse(values)
  if (!parsed.success) {
    return parsed.error.issues[0]?.message ?? "Model form is invalid."
  }
  return validateModelFormData(parsed.data)
}
