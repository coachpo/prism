import { z } from "zod"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { PricingTemplate, PricingTemplateConnectionUsageItem, PricingTemplateCreate, PricingTemplateUpdate } from "@/lib/types"

export const priceFields = ["input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"] as const
export type PriceField = (typeof priceFields)[number]

// Form inputs are always strings; empty specialty prices mean
// "unconfigured" and become explicit null on the wire (SPEC 4.1).
export type PricingTemplateFormState = {
  name: string
  description: string
} & Record<PriceField, string>

export type PricingTemplateFormValues = PricingTemplateFormState



export const DEFAULT_PRICING_TEMPLATE_FORM: PricingTemplateFormState = {
  name: "",
  description: "",
  input_price: "",
  output_price: "",
  cached_input_price: "",
  cache_creation_price: "",
  reasoning_price: "",
}

// Display helper: null (unconfigured) renders as its own label by callers;
// this helper only normalizes whitespace for presentation.
export const normalizeTemplatePrice = (value: string | null | undefined): string => {
  const trimmed = value?.trim() ?? ""
  return trimmed.length > 0 ? trimmed : ""
}

const requiredDecimalSchema = z.string()
  .trim()
  .regex(/^\d+(\.\d+)?$/, "must be a non-negative decimal string")

// Empty specialty price means "unconfigured" (explicit null on the wire);
// base prices are required.
const optionalDecimalSchema = z.string()
  .trim()
  .refine((value) => value.length === 0 || /^\d+(\.\d+)?$/.test(value), "must be a non-negative decimal string")

export const pricingTemplateFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  description: z.string(),
  input_price: requiredDecimalSchema,
  output_price: requiredDecimalSchema,
  cached_input_price: optionalDecimalSchema,
  cache_creation_price: optionalDecimalSchema,
  reasoning_price: optionalDecimalSchema,
})

export const isNonNegativeDecimalString = (value: string): boolean => /^\d+(\.\d+)?$/.test(value.trim()) && Number(value.trim()) >= 0

export const pricingTemplateFormStateFromTemplate = (template: PricingTemplate): PricingTemplateFormState => ({
  name: template.name,
  description: template.description ?? "",
  input_price: template.input_price,
  output_price: template.output_price,
  cached_input_price: template.cached_input_price ?? "",
  cache_creation_price: template.cache_creation_price ?? "",
  reasoning_price: template.reasoning_price ?? "",
})

export const normalizePricingTemplateFormPrices = (parsed: PricingTemplateFormState) => ({
  input_price: parsed.input_price.trim(),
  output_price: parsed.output_price.trim(),
  cached_input_price: parsed.cached_input_price.trim() || null,
  cache_creation_price: parsed.cache_creation_price.trim() || null,
  reasoning_price: parsed.reasoning_price.trim() || null,
})

export function buildPricingTemplateCreatePayload(values: PricingTemplateFormState): PricingTemplateCreate {
  const parsed = pricingTemplateFormSchema.parse(values)
  return { name: parsed.name, description: parsed.description.trim() || null, ...normalizePricingTemplateFormPrices(parsed) }
}

export function buildPricingTemplateUpdatePayload(template: PricingTemplate, values: PricingTemplateFormState): PricingTemplateUpdate {
  return { expected_updated_at: template.updated_at, ...buildPricingTemplateCreatePayload(values) }
}

export function isPricingTemplateDeleteBlocked(options: { deleting: boolean; usageLoading: boolean; usageError: boolean; dependencyCount: number }) {
  return options.deleting || options.usageLoading || options.usageError || options.dependencyCount > 0
}

export const parsePricingTemplateUsageRows = (detail: unknown): PricingTemplateConnectionUsageItem[] => {
  if (!detail || typeof detail !== "object") return []
  const payload = detail as { connections?: unknown; detail?: unknown }
  const maybeConnections = payload.connections ?? (payload.detail && typeof payload.detail === "object" && "connections" in payload.detail ? (payload.detail as { connections?: unknown }).connections : undefined)
  if (!Array.isArray(maybeConnections)) return []
  const rows: PricingTemplateConnectionUsageItem[] = []
  for (const connection of maybeConnections) {
    if (!connection || typeof connection !== "object") continue
    const entry = connection as Record<string, unknown>
    const connectionId = typeof entry.connection_id === "number" ? entry.connection_id : null
    const modelConfigId = typeof entry.model_config_id === "number" ? entry.model_config_id : null
    const endpointId = typeof entry.endpoint_id === "number" ? entry.endpoint_id : null
    if (connectionId === null || modelConfigId === null || endpointId === null) continue
    const modelId = typeof entry.model_id === "string" && entry.model_id.trim().length > 0 ? entry.model_id : getStaticMessages().pricingTemplatesData.unknownModel
    const endpointName = typeof entry.endpoint_name === "string" && entry.endpoint_name.trim().length > 0 ? entry.endpoint_name : getStaticMessages().pricingTemplatesData.endpointWithId(String(endpointId))
    rows.push({ connection_id: connectionId, connection_name: typeof entry.connection_name === "string" ? entry.connection_name : null, model_config_id: modelConfigId, model_id: modelId, endpoint_id: endpointId, endpoint_name: endpointName })
  }
  return rows
}
