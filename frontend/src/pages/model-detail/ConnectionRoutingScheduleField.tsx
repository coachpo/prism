import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { supportedTimezones, timezoneLabel } from "@/lib/ianaTimeZones";
import { OperatorInsetPanel } from "@/shared/design-system";
import {
  newRoutingScheduleWindowDraft,
  type RoutingScheduleDraft,
  type RoutingScheduleDraftError,
} from "./routingScheduleDraft";

interface ConnectionRoutingScheduleFieldProps {
  draft: RoutingScheduleDraft;
  onDraftChange: (draft: RoutingScheduleDraft) => void;
  error: RoutingScheduleDraftError | null;
}

/** ISO weekday bit positions: bit0 = Monday. */
const WEEKDAY_BITS = [0, 1, 2, 3, 4, 5, 6];

/**
 * Editor for a Terminal Target's routing windows.
 *
 * Purely presentational: it owns no validation verdict and no clock. The draft
 * is converted and validated by routingScheduleDraft, which mirrors the server
 * validator, so the form refuses exactly what the API refuses.
 */
export function ConnectionRoutingScheduleField({ draft, onDraftChange, error }: ConnectionRoutingScheduleFieldProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const weekdayNames = copy.routingScheduleWeekdays;
  const timezones = supportedTimezones();

  const setWindow = (id: string, patch: Partial<RoutingScheduleDraft["windows"][number]>) => {
    onDraftChange({ ...draft, windows: draft.windows.map((row) => (row.id === id ? { ...row, ...patch } : row)) });
  };

  return (
    <OperatorInsetPanel className="gap-3">
      <Field orientation="horizontal">
        <Checkbox
          id="routing-schedule-enabled"
          checked={draft.enabled}
          onCheckedChange={(checked) => {
            const enabled = checked === true;
            onDraftChange({
              ...draft,
              enabled,
              // Turning it on with no rows would immediately fail the
              // no_windows rule, so the first row comes for free.
              windows: enabled && draft.windows.length === 0 ? [newRoutingScheduleWindowDraft()] : draft.windows,
            });
          }}
        />
        <FieldLabel htmlFor="routing-schedule-enabled">{copy.routingScheduleEnableLabel}</FieldLabel>
      </Field>
      <FieldDescription>{copy.routingScheduleDescription}</FieldDescription>

      {draft.enabled ? (
        <FieldGroup className="gap-3">
          <Field>
            <FieldLabel htmlFor="routing-schedule-timezone">{copy.routingScheduleTimezoneLabel}</FieldLabel>
            <Select value={draft.timezone} onValueChange={(timezone) => onDraftChange({ ...draft, timezone })}>
              <SelectTrigger id="routing-schedule-timezone">
                <SelectValue placeholder={copy.routingScheduleTimezonePlaceholder} />
              </SelectTrigger>
              <SelectContent>
                {timezones.map((timezone) => (
                  <SelectItem key={timezone} value={timezone}>
                    {timezoneLabel(timezone)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>{copy.routingScheduleTimezoneDescription}</FieldDescription>
          </Field>

          {draft.windows.map((row, index) => (
            // Keyed by generated id, never the array index: removing a middle
            // row would otherwise shift its state onto its neighbour.
            <OperatorInsetPanel key={row.id} className="gap-2">
              <fieldset className="flex flex-wrap items-center gap-2">
                <legend className="sr-only">{copy.routingScheduleWeekdaysLegend}</legend>
                {WEEKDAY_BITS.map((bit) => {
                  const mask = 1 << bit;
                  const checked = (row.weekdayMask & mask) !== 0;
                  return (
                    <label key={bit} className="flex min-h-7 min-w-7 items-center gap-1 text-xs">
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(next) =>
                          setWindow(row.id, {
                            weekdayMask: next === true ? row.weekdayMask | mask : row.weekdayMask & ~mask,
                          })
                        }
                      />
                      {weekdayNames[bit]}
                    </label>
                  );
                })}
              </fieldset>
              <div className="flex flex-wrap items-center gap-2">
                <Input
                  type="time"
                  aria-label={copy.routingScheduleStartLabel}
                  value={row.start}
                  onChange={(event) => setWindow(row.id, { start: event.target.value })}
                  className="w-28"
                />
                <span className="text-muted-foreground">–</span>
                <Input
                  type="time"
                  aria-label={copy.routingScheduleEndLabel}
                  value={row.end}
                  onChange={(event) => setWindow(row.id, { end: event.target.value })}
                  className="w-28"
                />
                <label className="flex items-center gap-1 text-xs">
                  <Checkbox
                    checked={row.endsNextDay}
                    onCheckedChange={(next) => setWindow(row.id, { endsNextDay: next === true })}
                  />
                  {copy.routingScheduleEndsNextDay}
                </label>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => onDraftChange({ ...draft, windows: draft.windows.filter((item) => item.id !== row.id) })}
                >
                  {copy.routingScheduleRemoveWindow}
                </Button>
              </div>
              {error?.windowIndex === index ? <FieldError>{copy.routingScheduleError(error.reason)}</FieldError> : null}
            </OperatorInsetPanel>
          ))}

          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onDraftChange({ ...draft, windows: [...draft.windows, newRoutingScheduleWindowDraft()] })}
          >
            {copy.routingScheduleAddWindow}
          </Button>
          {error && error.windowIndex === undefined ? <FieldError>{copy.routingScheduleError(error.reason)}</FieldError> : null}
        </FieldGroup>
      ) : null}
    </OperatorInsetPanel>
  );
}
