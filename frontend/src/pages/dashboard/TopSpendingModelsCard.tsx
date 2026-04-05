import { ArrowUpRight, DollarSign } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatMoneyMicros } from "@/lib/costing";
import type { SpendingTopModel } from "@/lib/types";

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
  const { locale, messages } = useLocale();

  return (
    <Card className="col-span-3">
      <CardHeader>
        <CardTitle>{messages.dashboard.topSpendingModels}</CardTitle>
        <CardDescription>{messages.dashboard.topSpendingModelsDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        {topSpendingModels.length === 0 ? (
          <EmptyState
            icon={<DollarSign className="h-6 w-6" />}
            title={messages.dashboard.noSpendingData}
            description={messages.dashboard.noSpendingDataDescription}
          />
        ) : (
          <div className="space-y-4">
            {topSpendingModels.map((model, index) => (
              <div key={model.model_id} className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted font-mono text-xs font-medium">
                    {index + 1}
                  </div>
                  <div className="space-y-1">
                    <div className="space-y-1">
                      <p className="text-sm font-medium leading-none">
                        {modelDisplayNames.get(model.model_id) || model.model_id}
                      </p>
                      <p className="text-xs text-muted-foreground">{model.model_id}</p>
                    </div>
                    <div className="h-1.5 w-24 overflow-hidden rounded-full bg-muted">
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
                    {formatMoneyMicros(model.total_cost_micros, "$", undefined, 2, 6, locale)}
                  </p>
                </div>
              </div>
            ))}
            <Button variant="outline" className="mt-4 w-full" onClick={onViewFullReport}>
              {messages.dashboard.viewFullReport}
              <ArrowUpRight className="ml-2 h-4 w-4" />
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
