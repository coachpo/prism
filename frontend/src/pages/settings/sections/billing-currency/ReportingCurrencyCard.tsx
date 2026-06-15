import type { Dispatch, SetStateAction } from "react";
import { useLocale } from "@/i18n/useLocale";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { CostingSettingsUpdate } from "@/lib/types";
import { OperatorInsetPanel } from "@/shared/design-system";

interface ReportingCurrencyCardProps {
  costingForm: CostingSettingsUpdate;
  normalizedCurrentCosting: CostingSettingsUpdate;
  setCostingForm: Dispatch<SetStateAction<CostingSettingsUpdate>>;
}

export function ReportingCurrencyCard({
  costingForm,
  normalizedCurrentCosting,
  setCostingForm,
}: ReportingCurrencyCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  return (
    <OperatorInsetPanel title={copy.reportingCurrency}>
      <FieldGroup className="gap-3">
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-28">
            <FieldLabel htmlFor="report-currency-code">{copy.code}</FieldLabel>
            <Input
              id="report-currency-code"
              name="report_currency_code"
              autoComplete="off"
              maxLength={3}
              value={costingForm.report_currency_code}
              onChange={(event) =>
                setCostingForm((prev) => ({
                  ...prev,
                  report_currency_code: event.target.value.toUpperCase(),
                }))
              }
              placeholder={copy.currencyCodePlaceholder}
            />
          </Field>
          <Field className="w-24">
            <FieldLabel htmlFor="report-currency-symbol">{copy.symbol}</FieldLabel>
            <Input
              id="report-currency-symbol"
              name="report_currency_symbol"
              autoComplete="off"
              maxLength={5}
              value={costingForm.report_currency_symbol}
              onChange={(event) =>
                setCostingForm((prev) => ({
                  ...prev,
                  report_currency_symbol: event.target.value,
                }))
              }
              placeholder={copy.currencySymbolPlaceholder}
            />
          </Field>
          <p className="pb-2 text-sm font-medium">
            {copy.reportingCurrencySummary(
              normalizedCurrentCosting.report_currency_code || "---",
              normalizedCurrentCosting.report_currency_symbol || "-",
            )}
          </p>
        </div>
        <FieldDescription>
          {copy.usedForSpendingReports}
        </FieldDescription>
      </FieldGroup>
    </OperatorInsetPanel>
  );
}
