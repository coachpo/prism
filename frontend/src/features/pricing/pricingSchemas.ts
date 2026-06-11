import { z } from "zod"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { PricingTemplate, PricingTemplateConnectionUsageItem, PricingTemplateCreate, PricingTemplateUpdate } from "@/lib/types"

export const priceFields = ["input_price", "output_price", "cached_input_price", "cache_creation_price", "reasoning_price"] as const
export type PriceField = (typeof priceFields)[number]

export type PricingTemplateFormState = {
  name: string
  description: string
  pricing_currency_code: string
} & Record<PriceField, string>

export const DEFAULT_PRICING_TEMPLATE_FORM: PricingTemplateFormState = {
  name: "",
  description: "",
  pricing_currency_code: "USD",
  input_price: "0",
  output_price: "0",
  cached_input_price: "0",
  cache_creation_price: "0",
  reasoning_price: "0",
}

export const normalizeTemplatePrice = (value: string | null | undefined): string => {
  const trimmed = value?.trim() ?? ""
  return trimmed.length > 0 ? trimmed : "0"
}

function isValidCurrencyCode(value: string): boolean {
  return /^[A-Z]{3}$/.test(value.trim().toUpperCase())
}

const nonNegativeDecimalSchema = z.string()
  .transform((value) => normalizeTemplatePrice(value))
  .pipe(z.string().regex(/^\d+(\.\d+)?$/, "must be a non-negative decimal string"))

export const pricingTemplateFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  description: z.string(),
  pricing_currency_code: z.string().trim().toUpperCase().refine(isValidCurrencyCode, "Pricing currency must be a valid 3-letter code"),
  input_price: nonNegativeDecimalSchema,
  output_price: nonNegativeDecimalSchema,
  cached_input_price: nonNegativeDecimalSchema,
  cache_creation_price: nonNegativeDecimalSchema,
  reasoning_price: nonNegativeDecimalSchema,
}) satisfies z.ZodType<PricingTemplateFormState>

export type PricingTemplateFormValues = PricingTemplateFormState

export const isNonNegativeDecimalString = (value: string): boolean => /^\d+(\.\d+)?$/.test(value.trim()) && Number(value.trim()) >= 0

export const pricingTemplateFormStateFromTemplate = (template: PricingTemplate): PricingTemplateFormState => ({
  name: template.name,
  description: template.description ?? "",
  pricing_currency_code: template.pricing_currency_code,
  input_price: normalizeTemplatePrice(template.input_price),
  output_price: normalizeTemplatePrice(template.output_price),
  cached_input_price: normalizeTemplatePrice(template.cached_input_price),
  cache_creation_price: normalizeTemplatePrice(template.cache_creation_price),
  reasoning_price: normalizeTemplatePrice(template.reasoning_price),
})

export const normalizePricingTemplateFormPrices = (form: PricingTemplateFormValues): Record<PriceField, string> => ({
  input_price: normalizeTemplatePrice(form.input_price),
  output_price: normalizeTemplatePrice(form.output_price),
  cached_input_price: normalizeTemplatePrice(form.cached_input_price),
  cache_creation_price: normalizeTemplatePrice(form.cache_creation_price),
  reasoning_price: normalizeTemplatePrice(form.reasoning_price),
})

export function buildPricingTemplateCreatePayload(values: PricingTemplateFormValues): PricingTemplateCreate {
  const parsed = pricingTemplateFormSchema.parse(values)
  return { name: parsed.name, description: parsed.description.trim() || null, pricing_currency_code: parsed.pricing_currency_code, ...normalizePricingTemplateFormPrices(parsed) }
}

export function buildPricingTemplateUpdatePayload(template: PricingTemplate, values: PricingTemplateFormValues): PricingTemplateUpdate {
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
