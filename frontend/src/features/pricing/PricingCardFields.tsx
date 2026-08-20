import type { Control, FieldPath } from "react-hook-form";
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
import type {
  PriceField,
  PricingTemplateFormValues,
} from "./pricingSchemas";

const fields: PriceField[] = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
];

type CardPath = "base" | "tier" | "peak_card" | "offpeak_card";

function formPath(path: CardPath, field: PriceField) {
  return (path === "base" ? field : `${path}.${field}`) as FieldPath<PricingTemplateFormValues>;
}

export function PricingCardFields({
  control,
  path,
  title,
}: {
  control: Control<PricingTemplateFormValues>;
  path: CardPath;
  title: string;
}) {
  const copy = useLocale().messages.pricingTemplateDialog;
  const labels: Record<PriceField, string> = {
    input_price: copy.inputPriceLabel,
    output_price: copy.outputPriceLabel,
    cached_input_price: copy.cachedInputPriceLabel,
    cache_creation_price: copy.cacheCreationPriceLabel,
    reasoning_price: copy.reasoningPriceLabel,
  };
  return (
    <OperatorInsetPanel>
      <p className="text-sm font-medium text-foreground">{title}</p>
      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {fields.map((name) => (
          <FormField
            key={name}
            control={control}
            name={formPath(path, name)}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{labels[name]}</FormLabel>
                <FormControl>
                  <Input
                    inputMode="decimal"
                    autoComplete="off"
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={typeof field.value === "string" ? field.value : ""}
                    onChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        ))}
      </div>
    </OperatorInsetPanel>
  );
}
