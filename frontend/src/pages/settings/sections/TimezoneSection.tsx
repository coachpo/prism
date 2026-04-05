import type { ReactNode } from "react";
import { Globe } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { CostingSettingsUpdate } from "@/lib/types";

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
      <Card>
        <CardHeader className="pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Globe className="h-4 w-4" />
                {copy.timezone}
              </CardTitle>
              <CardDescription className="text-xs">
                {copy.timezoneAffectsTimestamps}
              </CardDescription>
            </div>
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
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {costingUnavailable ? (
            <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
              {copy.settingsApiUnavailable}
            </div>
          ) : costingLoading ? (
            <div className="space-y-2">
              <div className="h-9 animate-pulse rounded bg-muted" />
            </div>
          ) : (
            <div className="min-w-0 space-y-3">
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <div className="min-w-0 space-y-2">
                  <Label>{copy.timezonePreference}</Label>
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
                      <SelectItem value="auto">
                        {copy.timezoneAuto(Intl.DateTimeFormat().resolvedOptions().timeZone)}
                      </SelectItem>
                      {Intl.supportedValuesOf("timeZone").map((timezone) => (
                        <SelectItem key={timezone} value={timezone}>
                          {timezone}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <p className="text-sm text-muted-foreground">
                {copy.exampleTimestamp(timezonePreviewText, timezonePreviewZone)}
              </p>
            </div>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
