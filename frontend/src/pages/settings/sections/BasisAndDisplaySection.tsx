import { supportedTimezones } from "@/lib/ianaTimeZones";
import React, { useState } from "react";
import type { ReactNode } from "react";
import { RefreshCcw, SlidersHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type { CostingSettingsUpdate } from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel, OperatorSectionCard } from "@/shared/design-system";
import { ReportingCurrencyCard } from "./billing-currency/ReportingCurrencyCard";
import { CurrencyMigrationDialog } from "./billing-currency/CurrencyMigrationDialog";
import { ArchiveUnusedFxDialog } from "./billing-currency/ArchiveUnusedFxDialog";

interface BasisAndDisplaySectionProps {
  costingDirty: boolean;
  renderSectionSaveState: (section: "costing", isDirty: boolean) => ReactNode;
  costingUnavailable: boolean;
  costingLoading: boolean;
  costingForm: CostingSettingsUpdate;
  setCostingForm: React.Dispatch<React.SetStateAction<CostingSettingsUpdate>>;
  normalizedCurrentCosting: CostingSettingsUpdate;
  onCurrencyMigrated: () => Promise<void>;
  timezonePreviewText: string;
  timezonePreviewZone: string;
  timezonePreviewOffset: string;
}

/**
 * Reporting currency and timezone are one card because they are one write.
 * The save action lives in the page header; this card only carries its own
 * secondary actions (currency migration, FX archive).
 *
 * The `timezone` anchor is kept so the previously shipped
 * `?section=timezone` deep link still resolves to the same content.
 */
export function BasisAndDisplaySection({
  costingDirty,
  costingLoading,
  costingForm,
  costingUnavailable,
  normalizedCurrentCosting,
  onCurrencyMigrated,
  renderSectionSaveState,
  setCostingForm,
  timezonePreviewOffset,
  timezonePreviewText,
  timezonePreviewZone,
}: BasisAndDisplaySectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  const pageCopy = messages.settingsPage;
  const [migrationOpen, setMigrationOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);

  return (
    <section id="billing-currency" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={(
          <span className="flex items-center gap-2">
            <SlidersHorizontal data-icon="inline-start" />
            {pageCopy.basisAndDisplay}
          </span>
        )}
        description={pageCopy.basisAndDisplayDescription}
        actions={renderSectionSaveState("costing", costingDirty)}
        contentClassName="flex flex-col gap-4"
      >
        {costingUnavailable ? (
          <OperatorCallout description={copy.costApiUnavailable} intent="warning" />
        ) : costingLoading ? (
          <div className="flex flex-col gap-2" aria-hidden="true">
            <Skeleton className="h-9 rounded" />
            <Skeleton className="h-9 rounded" />
            <Skeleton className="h-24 rounded" />
          </div>
        ) : (
          <>
            <ReportingCurrencyCard
              costingForm={costingForm}
              setCostingForm={setCostingForm}
              normalizedCurrentCosting={normalizedCurrentCosting}
            />

            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setMigrationOpen(true)}>
                <RefreshCcw data-icon="inline-start" />
                {copy.migrateCurrency}
              </Button>
              <span className="text-xs text-muted-foreground">{copy.migrateCurrencyHint}</span>
            </div>

            {normalizedCurrentCosting.pricing_migration_inventory?.archive_only_available ? (
              <div className="flex flex-wrap items-center gap-2">
                <Button type="button" variant="outline" size="sm" onClick={() => setArchiveOpen(true)}>
                  {messages.settingsCurrencyMigration.archiveButton}
                </Button>
                <span className="text-xs text-muted-foreground">
                  {messages.settingsCurrencyMigration.archiveDescription}
                </span>
              </div>
            ) : null}

            <div id="timezone" tabIndex={-1} className="scroll-mt-24">
              <OperatorInsetPanel title={copy.timezone} description={copy.timezoneAffectsTimestamps}>
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
                          {supportedTimezones().map((timezone) => (
                            <SelectItem key={timezone} value={timezone}>
                              {timezone}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                </div>

                <p className="text-xs text-muted-foreground">
                  {copy.exampleTimestamp(
                    timezonePreviewText,
                    timezonePreviewZone,
                    timezonePreviewOffset,
                    new Date().toISOString(),
                  )}
                </p>
              </OperatorInsetPanel>
            </div>
          </>
        )}
      </OperatorSectionCard>

      <CurrencyMigrationDialog
        open={migrationOpen}
        onOpenChange={setMigrationOpen}
        currentCosting={normalizedCurrentCosting}
        onMigrated={onCurrencyMigrated}
      />
      <ArchiveUnusedFxDialog
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
        currentCosting={normalizedCurrentCosting}
        onArchived={onCurrencyMigrated}
      />
    </section>
  );
}
