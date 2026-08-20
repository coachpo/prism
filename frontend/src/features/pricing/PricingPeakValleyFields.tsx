import { useFieldArray, useFormContext, useWatch, type Control, type FieldPath } from "react-hook-form";
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
import { PricingCardFields } from "./PricingCardFields";
import type { PricingTemplateFormValues } from "./pricingSchemas";
import {
  PRICING_MINUTES_PER_DAY,
  pricingMinuteToTime,
  pricingTimeToMinute,
  pricingWindowEndMinute,
  togglePricingWeekday,
} from "./pricingWindowDraft";

function windowPath(index: number, field: "weekday_mask" | "start_minute" | "end_minute") {
  return `schedule_windows.${index}.${field}` as FieldPath<PricingTemplateFormValues>;
}

export function PricingPeakValleyFields({
  control,
}: {
  control: Control<PricingTemplateFormValues>;
}) {
  const windows = useFieldArray({ control, name: "schedule_windows" as never });
  const { setValue } = useFormContext<PricingTemplateFormValues>();
  const watchedWindows = useWatch({ control, name: "schedule_windows" });
  const copy = useLocale().messages.pricingTemplateDialog;

  return (
    <div className="flex flex-col gap-4">
      <PricingCardFields control={control} path="peak_card" title={copy.peakCardLabel} />
      <PricingCardFields control={control} path="offpeak_card" title={copy.offpeakCardLabel} />
      <OperatorInsetPanel>
        <p className="text-sm font-medium text-foreground">{copy.scheduleSectionTitle}</p>
        <p className="mt-1 text-xs text-muted-foreground">{copy.scheduleDescription}</p>
        <FormField
          control={control}
          name="schedule_timezone"
          render={({ field }) => (
            <FormItem className="mt-3">
              <FormLabel>{copy.timezoneLabel}</FormLabel>
              <FormControl>
                <Input placeholder={copy.timezonePlaceholder} autoComplete="off" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="mt-4 flex flex-col gap-3">
          {windows.fields.map((field, index) => {
            const current = watchedWindows[index] ?? { weekday_mask: 0, start_minute: -1, end_minute: -1 };
            const endsNextDay = current.end_minute >= PRICING_MINUTES_PER_DAY;
            return (
              <div key={field.id} className="border-t border-border pt-3 first:border-t-0 first:pt-0">
                <FormField
                  control={control}
                  name={windowPath(index, "weekday_mask")}
                  render={({ field: weekdayField }) => (
                    <FormItem>
                      <FormLabel>{copy.windowWeekdays(index + 1)}</FormLabel>
                      <div className="flex flex-wrap gap-1.5">
                        {copy.weekdayLabels.map((label, bit) => {
                          const selected = (Number(weekdayField.value) & (1 << bit)) !== 0;
                          return (
                            <Button
                              key={label}
                              type="button"
                              size="sm"
                              variant={selected ? "default" : "outline"}
                              aria-pressed={selected}
                              onClick={() => weekdayField.onChange(togglePricingWeekday(Number(weekdayField.value), bit, !selected))}
                            >
                              {label}
                            </Button>
                          );
                        })}
                      </div>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <div className="mt-3 grid gap-3 sm:grid-cols-2">
                  <FormField
                    control={control}
                    name={windowPath(index, "start_minute")}
                    render={({ field: startField }) => (
                      <FormItem>
                        <FormLabel>{copy.windowStartTime}</FormLabel>
                        <FormControl>
                          <Input type="time" value={pricingMinuteToTime(Number(startField.value))} onChange={(event) => startField.onChange(pricingTimeToMinute(event.target.value))} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={control}
                    name={windowPath(index, "end_minute")}
                    render={({ field: endField }) => (
                      <FormItem>
                        <FormLabel>{copy.windowEndTime}</FormLabel>
                        <FormControl>
                          <Input type="time" value={pricingMinuteToTime(Number(endField.value))} onChange={(event) => endField.onChange(pricingWindowEndMinute(event.target.value, endsNextDay))} />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
                <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant={endsNextDay ? "default" : "outline"}
                    aria-pressed={endsNextDay}
                    onClick={() => {
                      const base = current.end_minute < 0 ? -1 : current.end_minute % PRICING_MINUTES_PER_DAY;
                      const next = base < 0 ? -1 : base + (endsNextDay ? 0 : PRICING_MINUTES_PER_DAY);
                      setValue(windowPath(index, "end_minute"), next, { shouldDirty: true, shouldValidate: true });
                    }}
                  >
                    {copy.windowEndsNextDay}
                  </Button>
                  <Button type="button" variant="outline" size="sm" onClick={() => windows.remove(index)}>
                    {copy.removeWindow(index + 1)}
                  </Button>
                </div>
              </div>
            );
          })}
          <FormField control={control} name="schedule_windows" render={() => <FormMessage />} />
          <Button
            type="button"
            variant="outline"
            onClick={() => windows.append({ weekday_mask: 0, start_minute: -1, end_minute: -1 })}
          >
            {copy.addWindow}
          </Button>
        </div>
      </OperatorInsetPanel>
    </div>
  );
}
