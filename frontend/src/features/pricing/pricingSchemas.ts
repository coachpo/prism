import { z } from "zod";
import type {
  PricingCard,
  PricingTemplate,
  PricingTemplateCreate,
  PricingTemplateKind,
  PricingTemplateUpdate,
  PricingTemplateWindow,
} from "@/lib/types";

export const priceFields = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
] as const;
export type PriceField = (typeof priceFields)[number];

export type PriceCardFormState = Record<PriceField, string>;
export type PricingTierFormState = {
  input_tokens_above: string;
} & PriceCardFormState;
export type PricingTemplateFormState = {
  name: string;
  description: string;
  template_kind: PricingTemplateKind;
  tier: PricingTierFormState;
  peak_card: PriceCardFormState;
  offpeak_card: PriceCardFormState;
  schedule_timezone: string;
  schedule_windows: PricingTemplateWindow[];
} & PriceCardFormState;
export type PricingTemplateFormValues = PricingTemplateFormState;

const blankCard = (): PriceCardFormState => ({
  input_price: "",
  output_price: "",
  cached_input_price: "",
  cache_creation_price: "",
  reasoning_price: "",
});

export const DEFAULT_PRICING_TEMPLATE_FORM: PricingTemplateFormState = {
  name: "",
  description: "",
  template_kind: "standard",
  ...blankCard(),
  tier: { input_tokens_above: "", ...blankCard() },
  peak_card: blankCard(),
  offpeak_card: blankCard(),
  schedule_timezone: "",
  schedule_windows: [],
};

export const normalizeTemplatePrice = (
  value: string | null | undefined,
): string => {
  const trimmed = value?.trim() ?? "";
  return trimmed.length > 0 ? trimmed : "";
};

const requiredDecimalSchema = z
  .string()
  .trim()
  .regex(/^\d+(\.\d+)?$/, "must be a non-negative decimal string");
const optionalDecimalSchema = z
  .string()
  .trim()
  .refine(
    (value: string) => value.length === 0 || /^\d+(\.\d+)?$/.test(value),
    "must be a non-negative decimal string",
  );
const cardSchema = z.object({
  input_price: requiredDecimalSchema,
  output_price: requiredDecimalSchema,
  cached_input_price: optionalDecimalSchema,
  cache_creation_price: optionalDecimalSchema,
  reasoning_price: optionalDecimalSchema,
});

export const pricingTemplateFormSchema = z
  .object({
    name: z.string().trim().min(1, "Name is required"),
    description: z.string(),
    template_kind: z.enum(["standard", "tiered", "peak_valley"]),
    input_price: z.string(),
    output_price: z.string(),
    cached_input_price: z.string(),
    cache_creation_price: z.string(),
    reasoning_price: z.string(),
    tier: z.object({
      input_tokens_above: z.string(),
      input_price: z.string(),
      output_price: z.string(),
      cached_input_price: z.string(),
      cache_creation_price: z.string(),
      reasoning_price: z.string(),
    }),
    peak_card: z.object({
      input_price: z.string(),
      output_price: z.string(),
      cached_input_price: z.string(),
      cache_creation_price: z.string(),
      reasoning_price: z.string(),
    }),
    offpeak_card: z.object({
      input_price: z.string(),
      output_price: z.string(),
      cached_input_price: z.string(),
      cache_creation_price: z.string(),
      reasoning_price: z.string(),
    }),
    schedule_timezone: z.string(),
    schedule_windows: z.array(
      z.object({
        weekday_mask: z.number(),
        start_minute: z.number(),
        end_minute: z.number(),
      }),
    ),
  })
  .superRefine((values: PricingTemplateFormState, ctx: z.RefinementCtx) => {
    const checkCard = (path: string, card: PriceCardFormState) => {
      const parsed = cardSchema.safeParse(card);
      if (!parsed.success)
        for (const issue of parsed.error.issues)
          ctx.addIssue({ ...issue, path: [path, ...issue.path] });
    };
    if (values.template_kind === "standard") checkCard("card", values);
    if (values.template_kind === "tiered") checkCard("card", values);
    if (values.template_kind === "tiered") {
      if (
        !/^\d+$/.test(values.tier.input_tokens_above.trim()) ||
        Number(values.tier.input_tokens_above) < 1 ||
        !Number.isSafeInteger(Number(values.tier.input_tokens_above))
      )
        ctx.addIssue({
          code: "custom",
          path: ["tier", "input_tokens_above"],
          message: "threshold must be a positive integer",
        });
      checkCard("tier", values.tier);
      for (const field of [
        "cached_input_price",
        "cache_creation_price",
        "reasoning_price",
      ] as const) {
        if (
          values[field].trim().length > 0 !==
          values.tier[field].trim().length > 0
        )
          ctx.addIssue({
            code: "custom",
            path: ["tier", field],
            message: "card specialty configuration must match",
          });
      }
    }
    if (values.template_kind === "peak_valley") {
      checkCard("peak_card", values.peak_card);
      checkCard("offpeak_card", values.offpeak_card);
      if (
        !values.schedule_timezone.trim() ||
        values.schedule_timezone.trim() === "Local"
      )
        ctx.addIssue({
          code: "custom",
          path: ["schedule_timezone"],
          message: "an IANA timezone is required",
        });
      if (values.schedule_windows.length === 0)
        ctx.addIssue({
          code: "custom",
          path: ["schedule_windows"],
          message: "at least one peak window is required",
        });
      if (values.schedule_windows.length > 32)
        ctx.addIssue({
          code: "custom",
          path: ["schedule_windows"],
          message: "at most 32 peak windows are allowed",
        });
      const seenWindows = new Set<string>();
      values.schedule_windows.forEach((window, index) => {
        if (!Number.isInteger(window.weekday_mask) || window.weekday_mask < 1 || window.weekday_mask > 127)
          ctx.addIssue({ code: "custom", path: ["schedule_windows", index, "weekday_mask"], message: "select at least one weekday" });
        if (!Number.isInteger(window.start_minute) || window.start_minute < 0 || window.start_minute > 1439)
          ctx.addIssue({ code: "custom", path: ["schedule_windows", index, "start_minute"], message: "choose a valid start time" });
        if (!Number.isInteger(window.end_minute) || window.end_minute < 1 || window.end_minute > 2880 || window.end_minute <= window.start_minute || window.end_minute - window.start_minute > 1440)
          ctx.addIssue({ code: "custom", path: ["schedule_windows", index, "end_minute"], message: "end must be later than start and no more than one day away" });
        const key = `${window.weekday_mask}:${window.start_minute}:${window.end_minute}`;
        if (seenWindows.has(key))
          ctx.addIssue({ code: "custom", path: ["schedule_windows", index], message: "duplicate peak window" });
        seenWindows.add(key);
      });
      for (const field of [
        "cached_input_price",
        "cache_creation_price",
        "reasoning_price",
      ] as const) {
        if (
          values.peak_card[field].trim().length > 0 !==
          values.offpeak_card[field].trim().length > 0
        )
          ctx.addIssue({
            code: "custom",
            path: ["offpeak_card", field],
            message: "card specialty configuration must match",
          });
      }
    }
  });

export const isNonNegativeDecimalString = (value: string): boolean =>
  /^\d+(\.\d+)?$/.test(value.trim()) && Number(value.trim()) >= 0;

const formCardFromCard = (card?: PricingCard | null): PriceCardFormState =>
  card
    ? {
        input_price: card.input_price,
        output_price: card.output_price,
        cached_input_price: card.cached_input_price ?? "",
        cache_creation_price: card.cache_creation_price ?? "",
        reasoning_price: card.reasoning_price ?? "",
      }
    : blankCard();

export const pricingTemplateFormStateFromTemplate = (
  template: PricingTemplate,
): PricingTemplateFormState => {
  const standard = formCardFromCard(template.card ?? template.base_card);
  const tierCard = formCardFromCard(template.tier?.card);
  return {
    name: template.name,
    description: template.description ?? "",
    template_kind: template.template_kind,
    ...standard,
    tier: {
      input_tokens_above: template.tier
        ? String(template.tier.input_tokens_above)
        : "",
      ...tierCard,
    },
    peak_card: formCardFromCard(template.peak_card),
    offpeak_card: formCardFromCard(template.offpeak_card),
    schedule_timezone: template.schedule?.timezone ?? "",
    schedule_windows: template.schedule?.windows ?? [],
  };
};

export const normalizePricingTemplateFormPrices = (
  parsed: Pick<PriceCardFormState, PriceField>,
) => ({
  input_price: parsed.input_price.trim(),
  output_price: parsed.output_price.trim(),
  cached_input_price: parsed.cached_input_price.trim() || null,
  cache_creation_price: parsed.cache_creation_price.trim() || null,
  reasoning_price: parsed.reasoning_price.trim() || null,
});

const wireCard = (card: PriceCardFormState): PricingCard =>
  normalizePricingTemplateFormPrices(card);

export function buildPricingTemplateCreatePayload(
  values: PricingTemplateFormState,
): PricingTemplateCreate {
  const parsed = pricingTemplateFormSchema.parse(values);
  if (parsed.template_kind === "standard")
    return {
      name: parsed.name,
      description: parsed.description.trim() || null,
      template_kind: "standard",
      card: wireCard(parsed),
    };
  if (parsed.template_kind === "tiered")
    return {
      name: parsed.name,
      description: parsed.description.trim() || null,
      template_kind: "tiered",
      base_card: wireCard(parsed),
      tier: {
        input_tokens_above: Number(parsed.tier.input_tokens_above.trim()),
        card: wireCard(parsed.tier),
      },
    };
  return {
    name: parsed.name,
    description: parsed.description.trim() || null,
    template_kind: "peak_valley",
    peak_card: wireCard(parsed.peak_card),
    offpeak_card: wireCard(parsed.offpeak_card),
    schedule: {
      timezone: parsed.schedule_timezone.trim(),
      windows: parsed.schedule_windows,
    },
  };
}

export function buildPricingTemplateUpdatePayload(
  template: PricingTemplate,
  values: PricingTemplateFormState,
): PricingTemplateUpdate {
  return {
    expected_updated_at: template.updated_at,
    ...buildPricingTemplateCreatePayload(values),
  };
}
