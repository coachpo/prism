import { z, type RefinementCtx } from "zod"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { PricingTemplate, PricingTemplateTier } from "@/lib/types"

export const pricingTierPriceFields = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
] as const
export type PricingTierPriceField = (typeof pricingTierPriceFields)[number]

export type PricingTierFormState = {
  enabled: boolean
  input_tokens_above: string
} & Record<PricingTierPriceField, string>

export const DEFAULT_PRICING_TIER_FORM: PricingTierFormState = {
  enabled: false,
  input_tokens_above: "272000",
  input_price: "",
  output_price: "",
  cached_input_price: "",
  cache_creation_price: "",
  reasoning_price: "",
}

const tierValidationMessages = (getStaticMessages() as { pricingTierFields?: { invalidRequiredPrice: string; invalidOptionalPrice: string; invalidThreshold: string; parityError: string } }).pricingTierFields ?? {
  invalidRequiredPrice: "Invalid price",
  invalidOptionalPrice: "Invalid optional price",
  invalidThreshold: "Invalid threshold",
  parityError: "Tier price configuration must match the base card",
}
const decimal = z.string().trim().regex(/^\d+(\.\d+)?$/, tierValidationMessages.invalidRequiredPrice)
const optionalDecimal = z.string().trim().refine((value) => value === "" || /^\d+(\.\d+)?$/.test(value), tierValidationMessages.invalidOptionalPrice)

export const pricingTierFormSchema = z.object({
  enabled: z.boolean(),
  input_tokens_above: z.string(),
  input_price: z.string(),
  output_price: z.string(),
  cached_input_price: z.string(),
  cache_creation_price: z.string(),
  reasoning_price: z.string(),
}).superRefine((tier, ctx) => {
  if (!tier.enabled) return
  if (!/^\d+$/.test(tier.input_tokens_above.trim()) || Number(tier.input_tokens_above.trim()) < 1 || Number(tier.input_tokens_above.trim()) > 2147483647 || !Number.isSafeInteger(Number(tier.input_tokens_above.trim()))) {
    ctx.addIssue({ code: "custom", path: ["input_tokens_above"], message: tierValidationMessages.invalidThreshold })
  }
  for (const field of ["input_price", "output_price"] as const) {
    if (!decimal.safeParse(tier[field]).success) {
      ctx.addIssue({ code: "custom", path: [field], message: tierValidationMessages.invalidRequiredPrice })
    }
  }
  for (const field of ["cached_input_price", "cache_creation_price", "reasoning_price"] as const) {
    if (!optionalDecimal.safeParse(tier[field]).success) {
      ctx.addIssue({ code: "custom", path: [field], message: tierValidationMessages.invalidOptionalPrice })
    }
  }
})

/** Adds the bidirectional specialty-price parity rule to a parent form. */
export function addPricingTierParityIssues(
  tier: PricingTierFormState,
  base: Record<"cached_input_price" | "cache_creation_price" | "reasoning_price", string>,
  ctx: RefinementCtx,
) {
  if (!tier.enabled) return
  for (const field of ["cached_input_price", "cache_creation_price", "reasoning_price"] as const) {
    const baseConfigured = base[field].trim().length > 0
    const tierConfigured = tier[field].trim().length > 0
    if (baseConfigured !== tierConfigured) {
      ctx.addIssue({ code: "custom", path: ["tier", field], message: tierValidationMessages.parityError })
    }
  }
}

export function pricingTierFormStateFromTemplate(template: PricingTemplate): PricingTierFormState {
  const tier = template.tier
  if (!tier) return { ...DEFAULT_PRICING_TIER_FORM }
  return {
    enabled: true,
    input_tokens_above: String(tier.input_tokens_above),
    input_price: tier.input_price,
    output_price: tier.output_price,
    cached_input_price: tier.cached_input_price ?? "",
    cache_creation_price: tier.cache_creation_price ?? "",
    reasoning_price: tier.reasoning_price ?? "",
  }
}

export function normalizePricingTierPayload(
  tier: PricingTierFormState,
): PricingTemplateTier | null {
  if (!tier.enabled) return null
  return {
    input_tokens_above: Number(tier.input_tokens_above.trim()),
    input_price: tier.input_price.trim(),
    output_price: tier.output_price.trim(),
    cached_input_price: tier.cached_input_price.trim() || null,
    cache_creation_price: tier.cache_creation_price.trim() || null,
    reasoning_price: tier.reasoning_price.trim() || null,
  }
}
