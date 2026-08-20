import { z, type RefinementCtx } from "zod";
import type { PricingTemplate, PricingTemplateTier } from "@/lib/types";
import type {
  PriceCardFormState,
  PricingTemplateFormState,
} from "./pricingSchemas";

// Tier card fields are used only by the explicit `template_kind="tiered"` branch.
export const pricingTierPriceFields = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
] as const;
export type PricingTierPriceField = (typeof pricingTierPriceFields)[number];
export type PricingTierFormState = {
  input_tokens_above: string;
} & PriceCardFormState;

export const DEFAULT_PRICING_TIER_FORM: PricingTierFormState = {
  input_tokens_above: "",
  input_price: "",
  output_price: "",
  cached_input_price: "",
  cache_creation_price: "",
  reasoning_price: "",
};

const decimal = z
  .string()
  .trim()
  .regex(/^\d+(\.\d+)?$/, "must be a non-negative decimal string");
const optionalDecimal = z
  .string()
  .trim()
  .refine(
    (value: string) => value === "" || /^\d+(\.\d+)?$/.test(value),
    "must be a non-negative decimal string",
  );

export const pricingTierFormSchema = z.object({
  input_tokens_above: z.string(),
  input_price: z.string(),
  output_price: z.string(),
  cached_input_price: z.string(),
  cache_creation_price: z.string(),
  reasoning_price: z.string(),
});

export function addPricingTierParityIssues(
  tier: PricingTierFormState,
  base: PricingTemplateFormState,
  ctx: RefinementCtx,
) {
  if (
    !/^\d+$/.test(tier.input_tokens_above.trim()) ||
    Number(tier.input_tokens_above) < 1 ||
    !Number.isSafeInteger(Number(tier.input_tokens_above))
  )
    ctx.addIssue({
      code: "custom",
      path: ["tier", "input_tokens_above"],
      message: "threshold must be a positive integer",
    });
  for (const field of ["input_price", "output_price"] as const)
    if (!decimal.safeParse(tier[field]).success)
      ctx.addIssue({
        code: "custom",
        path: ["tier", field],
        message: "required price is invalid",
      });
  for (const field of [
    "cached_input_price",
    "cache_creation_price",
    "reasoning_price",
  ] as const) {
    if (!optionalDecimal.safeParse(tier[field]).success)
      ctx.addIssue({
        code: "custom",
        path: ["tier", field],
        message: "optional price is invalid",
      });
    if (base[field].trim().length > 0 !== tier[field].trim().length > 0)
      ctx.addIssue({
        code: "custom",
        path: ["tier", field],
        message: "card specialty configuration must match",
      });
  }
}

export function pricingTierFormStateFromTemplate(
  template: PricingTemplate,
): PricingTierFormState {
  const tier = template.tier;
  if (!tier) return { ...DEFAULT_PRICING_TIER_FORM };
  const card = tier.card;
  return {
    input_tokens_above: String(tier.input_tokens_above),
    input_price: card.input_price,
    output_price: card.output_price,
    cached_input_price: card.cached_input_price ?? "",
    cache_creation_price: card.cache_creation_price ?? "",
    reasoning_price: card.reasoning_price ?? "",
  };
}

export function normalizePricingTierPayload(
  tier: PricingTierFormState,
): PricingTemplateTier {
  return {
    input_tokens_above: Number(tier.input_tokens_above.trim()),
    card: {
      input_price: tier.input_price.trim(),
      output_price: tier.output_price.trim(),
      cached_input_price: tier.cached_input_price.trim() || null,
      cache_creation_price: tier.cache_creation_price.trim() || null,
      reasoning_price: tier.reasoning_price.trim() || null,
    },
  };
}
