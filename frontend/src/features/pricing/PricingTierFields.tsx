import { useWatch, type Control } from "react-hook-form";
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import { OperatorInsetPanel } from "@/shared/design-system";
import { PricingCardFields } from "./PricingCardFields";
import type { PricingTemplateFormValues } from "./pricingSchemas";

export function PricingTierFields({
  control,
}: {
  control: Control<PricingTemplateFormValues>;
}) {
  const copy = useLocale().messages.pricingTierFields;
  const kind = useWatch({ control, name: "template_kind" });
  if (kind !== "tiered") return null;
  return (
    <>
      <OperatorInsetPanel>
        <div className="flex flex-col gap-1">
          <p className="text-sm font-medium text-foreground">
            {copy.enabledDescription}
          </p>
          <p className="text-xs text-muted-foreground">
            {copy.thresholdDescription}
          </p>
        </div>
        <FormField
          control={control}
          name="tier.input_tokens_above"
          render={({ field }) => (
            <FormItem className="mt-3">
              <FormLabel>{copy.thresholdLabel}</FormLabel>
              <FormControl>
                <Input inputMode="numeric" autoComplete="off" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </OperatorInsetPanel>
      <PricingCardFields control={control} path="tier" title={copy.enabledLabel} />
      <p className="text-xs text-muted-foreground">{copy.parityDescription}</p>
    </>
  );
}
