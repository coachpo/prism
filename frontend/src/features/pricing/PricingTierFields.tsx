import { useWatch, type Control } from "react-hook-form"
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/i18n/useLocale"
import { OperatorInsetPanel, OperatorSwitchField } from "@/shared/design-system"
import type { PricingTemplateFormValues, PriceField } from "./pricingSchemas"

const tierPriceFields: Array<{ name: PriceField; labelKey: "inputPriceLabel" | "outputPriceLabel" | "cachedInputPriceLabel" | "cacheCreationPriceLabel" | "reasoningPriceLabel" }> = [
  { name: "input_price", labelKey: "inputPriceLabel" },
  { name: "output_price", labelKey: "outputPriceLabel" },
  { name: "cached_input_price", labelKey: "cachedInputPriceLabel" },
  { name: "cache_creation_price", labelKey: "cacheCreationPriceLabel" },
  { name: "reasoning_price", labelKey: "reasoningPriceLabel" },
]

function TierRateField({ control, field, label }: { control: Control<PricingTemplateFormValues>; field: PriceField; label: string }) {
  const name = `tier.${field}` as const
  return (
    <FormField control={control} name={name} render={({ field: input }) => (
      <FormItem>
        <FormLabel>{label}</FormLabel>
        <FormControl><Input inputMode="decimal" autoComplete="off" {...input} /></FormControl>
        <FormMessage />
      </FormItem>
    )} />
  )
}

export function PricingTierFields({ control }: { control: Control<PricingTemplateFormValues> }) {
  const { messages } = useLocale()
  const copy = messages.pricingTierFields
  const tierEnabled = useWatch({ control, name: "tier.enabled" })

  return (
    <OperatorInsetPanel>
      <FormField control={control} name="tier.enabled" render={({ field }) => (
        <OperatorSwitchField
          checked={field.value}
          label={copy.enabledLabel}
          description={copy.enabledDescription}
          onCheckedChange={field.onChange}
        />
      )} />
      {tierEnabled ? (
        <div className="mt-3 flex flex-col gap-3 border-t border-border pt-3">
          <div className="grid gap-2 sm:max-w-xs">
            <FormField control={control} name="tier.input_tokens_above" render={({ field }) => (
              <FormItem>
                <FormLabel>{copy.thresholdLabel}</FormLabel>
                <FormControl><Input inputMode="numeric" autoComplete="off" {...field} /></FormControl>
                <FormMessage />
              </FormItem>
            )} />
          </div>
          <p className="text-xs text-muted-foreground">{copy.thresholdDescription}</p>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {tierPriceFields.map(({ name, labelKey }) => (
              <TierRateField key={name} control={control} field={name} label={copy[labelKey]} />
            ))}
          </div>
          <p className="text-xs text-muted-foreground">{copy.parityDescription}</p>
        </div>
      ) : null}
    </OperatorInsetPanel>
  )
}
