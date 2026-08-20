import {
  useFieldArray,
  type Control,
  type UseFormRegister,
} from "react-hook-form";
import { Button } from "@/components/ui/button";
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
import type { PricingTemplateFormValues, PriceField } from "./pricingSchemas";

const priceFields: PriceField[] = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
];

function CardFields({
  control,
  path,
  title,
  labels,
}: {
  control: Control<PricingTemplateFormValues>;
  path: "peak_card" | "offpeak_card";
  title: string;
  labels: Record<PriceField, string>;
}) {
  return (
    <OperatorInsetPanel>
      <p className="text-sm font-medium text-foreground">{title}</p>
      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {priceFields.map((name) => (
          <FormField
            key={name}
            control={control}
            name={`${path}.${name}` as "peak_card.input_price"}
            render={({ field }) => (
              <FormItem>
                <FormLabel>{labels[name]}</FormLabel>
                <FormControl>
                  <Input inputMode="decimal" autoComplete="off" {...field} />
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

export function PricingPeakValleyFields({
  control,
  register,
}: {
  control: Control<PricingTemplateFormValues>;
  register: UseFormRegister<PricingTemplateFormValues>;
}) {
  const windows = useFieldArray({ control, name: "schedule_windows" as never });
  const { messages } = useLocale();
  const copy = messages.pricingTemplateDialog;
  const labels: Record<PriceField, string> = {
    input_price: copy.inputPriceLabel,
    output_price: copy.outputPriceLabel,
    cached_input_price: copy.cachedInputPriceLabel,
    cache_creation_price: copy.cacheCreationPriceLabel,
    reasoning_price: copy.reasoningPriceLabel,
  };
  return (
    <div className="flex flex-col gap-4">
      <CardFields
        control={control}
        path="peak_card"
        title={copy.peakCardLabel}
        labels={labels}
      />
      <CardFields
        control={control}
        path="offpeak_card"
        title={copy.offpeakCardLabel}
        labels={labels}
      />
      <OperatorInsetPanel>
        <p className="text-sm font-medium text-foreground">
          {copy.scheduleSectionTitle}
        </p>
        <p className="mt-1 text-xs text-muted-foreground">
          {copy.scheduleDescription}
        </p>
        <FormField
          control={control}
          name="schedule_timezone"
          render={({ field }) => (
            <FormItem className="mt-3">
              <FormLabel>{copy.timezoneLabel}</FormLabel>
              <FormControl>
                <Input
                  placeholder="Asia/Shanghai"
                  autoComplete="off"
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="mt-4 flex flex-col gap-2">
          {windows.fields.map((field, index) => (
            <div
              key={field.id}
              className="grid gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]"
            >
              <Input
                aria-label={copy.windowWeekdayMask(index + 1)}
                type="number"
                min={1}
                max={127}
                {...register(
                  `schedule_windows.${index}.weekday_mask` as never,
                  { valueAsNumber: true },
                )}
              />
              <Input
                aria-label={copy.windowStartMinute(index + 1)}
                type="number"
                min={0}
                max={1439}
                {...register(
                  `schedule_windows.${index}.start_minute` as never,
                  { valueAsNumber: true },
                )}
              />
              <Input
                aria-label={copy.windowEndMinute(index + 1)}
                type="number"
                min={1}
                max={2880}
                {...register(`schedule_windows.${index}.end_minute` as never, {
                  valueAsNumber: true,
                })}
              />
              <Button
                type="button"
                variant="outline"
                onClick={() => windows.remove(index)}
                aria-label={copy.removeWindow(index + 1)}
              >
                {copy.removeWindow(index + 1)}
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              windows.append({
                weekday_mask: 1,
                start_minute: 0,
                end_minute: 60,
              })
            }
          >
            {copy.addWindow}
          </Button>
        </div>
      </OperatorInsetPanel>
    </div>
  );
}
