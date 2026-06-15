import type { ReactNode } from "react";
import { Globe } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { useLocale } from "@/i18n/useLocale";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { CostingSettingsUpdate } from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel, OperatorSectionCard } from "@/shared/design-system";

interface TimezoneSectionProps {
  timezoneDirty: boolean;
  renderSectionSaveState: (section: "billing" | "timezone", isDirty: boolean) => ReactNode;
  handleSaveCostingSettings: (section: "billing" | "timezone") => Promise<void>;
  costingUnavailable: boolean;
  costingLoading: boolean;
  costingSaving: boolean;
  costingForm: CostingSettingsUpdate;
  setCostingForm: React.Dispatch<React.SetStateAction<CostingSettingsUpdate>>;
  timezonePreviewText: string;
  timezonePreviewZone: string;
}

export function TimezoneSection({
  timezoneDirty,
  renderSectionSaveState,
  handleSaveCostingSettings,
  costingUnavailable,
  costingLoading,
  costingSaving,
  costingForm,
  setCostingForm,
  timezonePreviewText,
  timezonePreviewZone,
}: TimezoneSectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  return (
    <section id="timezone" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={(
          <span className="flex items-center gap-2">
            <Globe data-icon="inline-start" />
            {copy.timezone}
          </span>
        )}
        description={copy.timezoneAffectsTimestamps}
        actions={(
          <div className="flex items-center gap-2">
            {renderSectionSaveState("timezone", timezoneDirty)}
            <Button
              type="button"
              size="sm"
              onClick={() => void handleSaveCostingSettings("timezone")}
              disabled={
                costingUnavailable ||
                costingLoading ||
                costingSaving ||
                !timezoneDirty
              }
            >
              {costingSaving ? messages.pricingTemplateDialog.saving : copy.saveTimezone}
            </Button>
          </div>
        )}
        contentClassName="flex flex-col gap-4"
      >
          {costingUnavailable ? (
            <OperatorCallout description={copy.settingsApiUnavailable} intent="warning" />
          ) : costingLoading ? (
            <div className="flex flex-col gap-2" aria-hidden="true">
              <Skeleton className="h-9 rounded" />
            </div>
          ) : (
            <OperatorInsetPanel>
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <Field className="min-w-0">
                  <FieldLabel>{copy.timezonePreference}</FieldLabel>
                  <Select
                    value={costingForm.timezone_preference || "auto"}
                    onValueChange={(value) =>
                      setCostingForm((prev) => ({
                        ...prev,
                        timezone_preference: value === "auto" ? null : value,
                      }))
                    }
                  >
                    <SelectTrigger className="w-full min-w-0 max-w-full">
                      <SelectValue placeholder={copy.selectTimezone} />
                    </SelectTrigger>
                    <SelectContent
                      position="popper"
                      className="min-w-[var(--radix-select-trigger-width)] max-w-[var(--radix-select-trigger-width)]"
                    >
                      <SelectGroup>
                        <SelectItem value="auto">
                          {copy.timezoneAuto(Intl.DateTimeFormat().resolvedOptions().timeZone)}
                        </SelectItem>
                        {Intl.supportedValuesOf("timeZone").map((timezone) => (
                          <SelectItem key={timezone} value={timezone}>
                            {timezone}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>

              <p className="text-sm text-muted-foreground">
                {copy.exampleTimestamp(timezonePreviewText, timezonePreviewZone)}
              </p>
            </OperatorInsetPanel>
          )}
      </OperatorSectionCard>
    </section>
  );
}
