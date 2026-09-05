import type { Dispatch, SetStateAction } from "react";
import { useLocale } from "@/i18n/useLocale";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { CostingSettingsUpdate } from "@/lib/types";
import { OperatorHelpHint, OperatorInsetPanel } from "@/shared/design-system";

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
  // 代码框被锁的唯一理由。禁用 input 不可聚焦，理由既要绑到它的
  // aria-describedby，也要有一个能 Tab 到的 28×28 帮助按钮承载。
  const codeLocked = costingForm.reporting_currency_epoch !== undefined
    && String(costingForm.reporting_currency_epoch) !== "0";
  return (
    <OperatorInsetPanel title={copy.reportingCurrency}>
      <FieldGroup className="gap-3">
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-28">
            <FieldLabel htmlFor="report-currency-code">
              {copy.code}
              {codeLocked ? <OperatorHelpHint className="-my-1" label={copy.codeLocked} /> : null}
            </FieldLabel>
            <Input
              id="report-currency-code"
              name="report_currency_code"
              autoComplete="off"
              maxLength={3}
              value={costingForm.report_currency_code}
              aria-describedby={codeLocked ? "report-currency-code-locked" : undefined}
              disabled={normalizedCurrentCosting.reporting_currency_epoch === undefined || codeLocked}
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
              disabled={normalizedCurrentCosting.reporting_currency_epoch === undefined}
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
        {codeLocked ? (
          <FieldDescription id="report-currency-code-locked">
            {copy.codeLocked}
          </FieldDescription>
        ) : null}
        <FieldDescription>
          {copy.usedForSpendingReports}
        </FieldDescription>
        {normalizedCurrentCosting.reporting_currency_epoch !== undefined ? (
          <p className="text-xs text-muted-foreground">
            {copy.activeEpoch(String(normalizedCurrentCosting.reporting_currency_epoch))}
          </p>
        ) : <p className="text-xs font-medium text-degraded">{copy.migrationRequired}</p>}
      </FieldGroup>
    </OperatorInsetPanel>
  );
}
