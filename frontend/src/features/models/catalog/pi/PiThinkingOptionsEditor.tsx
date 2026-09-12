import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { piThinkingLabel } from "./piCatalogPresentation";

const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

export function PiThinkingOptionsEditor({ raw, disabled, copy, onChange }: {
  raw: string;
  disabled: boolean;
  copy: Record<string, string>;
  onChange: (value: string) => void;
}) {
  let values: Record<string, string | null> = {};
  try { values = JSON.parse(raw || "{}"); } catch { /* New drafts start with no choices. */ }
  function update(level: string, value: string | null | undefined) {
    const next = { ...values };
    if (value === undefined) delete next[level];
    else next[level] = value;
    onChange(JSON.stringify(next));
  }
  return (
    <FieldGroup className="gap-2">
      <FieldDescription>{copy.thinkingValueHint}</FieldDescription>
      {THINKING_LEVELS.map((level) => {
        const value = values[level];
        const label = piThinkingLabel(level);
        return (
          <Field key={level}>
            <FieldLabel htmlFor={`pi-thinking-${level}`}>{label}</FieldLabel>
            <div className="grid gap-2 sm:grid-cols-2">
              <Select disabled={disabled} value={value === undefined ? "unset" : value === null ? "omit" : "value"} onValueChange={(mode) => update(level, mode === "unset" ? undefined : mode === "omit" ? null : "")}>
                <SelectTrigger id={`pi-thinking-${level}`}><SelectValue /></SelectTrigger>
                <SelectContent><SelectGroup>
                  <SelectItem value="unset">{copy.noOptions}</SelectItem>
                  <SelectItem value="value">{copy.overrideModeValue}</SelectItem>
                  <SelectItem value="omit">{copy.optionUnset}</SelectItem>
                </SelectGroup></SelectContent>
              </Select>
              {typeof value === "string" ? <Input aria-label={`${label} ${copy.optionValue}`} disabled={disabled} value={value} onChange={(event) => update(level, event.target.value)} /> : null}
            </div>
          </Field>
        );
      })}
    </FieldGroup>
  );
}
