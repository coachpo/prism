import { ArrowUpRight, DollarSign } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { SpendTrustNote } from "@/components/SpendTrustIndicator";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { formatMoneyMicros } from "@/lib/costing";
import type { SpendingTopModel } from "@/lib/types";
import { OperatorEmptyState } from "@/shared/design-system";

interface TopSpendingModelsCardProps {
  modelDisplayNames: Map<string, string>;
  onViewFullReport: () => void;
  topSpendingModels: SpendingTopModel[];
}

export function TopSpendingModelsCard({
  modelDisplayNames,
  onViewFullReport,
  topSpendingModels,
}: TopSpendingModelsCardProps) {
  const { currencyState } = useReportingCurrencyContext();
  const { locale, messages } = useLocale();

  return (
    <Card className="md:col-span-2 lg:col-span-3">
      <CardHeader>
        <CardTitle>{messages.dashboard.topSpendingModels}</CardTitle>
        <div className="flex flex-col gap-1">
          <CardDescription>{messages.dashboard.topSpendingModelsDescription}</CardDescription>
          {currencyState.trust !== "verified" ? (
            <SpendTrustNote spendTrust={currencyState.trust} />
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        {topSpendingModels.length === 0 ? (
          <OperatorEmptyState
            icon={<DollarSign className="size-6" />}
            title={messages.dashboard.noSpendingData}
            description={messages.dashboard.noSpendingDataDescription}
          />
        ) : (
          <div className="flex flex-col gap-4">
            {topSpendingModels.map((model, index) => (
              <div key={model.model_id} className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="flex size-[var(--density-control-h-sm)] items-center justify-center rounded-lg bg-surface-container font-mono text-xs font-medium">
                    {index + 1}
                  </div>
                  <div className="flex flex-col gap-1">
                    <div className="flex flex-col gap-1">
                      <p className="text-sm font-medium leading-none">
                        {model.model_label || modelDisplayNames.get(model.model_id) || model.model_id}
                      </p>
                      <p className="text-xs text-muted-foreground">{model.model_id}</p>
                    </div>
                    <div className="h-1.5 w-24 overflow-hidden rounded-full bg-surface-container">
                      <div
                        className="h-full bg-primary"
                        style={{
                          width: `${(model.total_cost_micros / (topSpendingModels[0]?.total_cost_micros || 1)) * 100}%`,
                        }}
                      />
                    </div>
                  </div>
                </div>
                <div className="text-right">
                  <p className="text-sm font-medium">
                    {formatMoneyMicros(model.total_cost_micros, undefined, undefined, 2, 6, locale)}
                  </p>
                </div>
              </div>
            ))}
            <Button variant="outline" className="mt-4 w-full" onClick={onViewFullReport}>
              {messages.dashboard.viewFullReport}
              <ArrowUpRight data-icon="inline-end" />
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
