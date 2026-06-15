import { Progress } from "@/components/ui/progress";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros } from "@/lib/costing";

interface TopSpendingItem {
  label: string;
  costMicros: number;
}

interface TopSpendingCardProps {
  title: string;
  items: TopSpendingItem[];
  totalCostMicros: number;
  currencySymbol: string;
  currencyCode: string;
}

export function TopSpendingCard({
  title,
  items,
  totalCostMicros,
  currencySymbol,
  currencyCode,
}: TopSpendingCardProps) {
  const { locale, messages } = useLocale();

  return (
    <Card className="operator-section-surface">
      <CardHeader className="gap-1 border-b">
        <CardTitle className="text-sm font-medium">
          <h4>{title}</h4>
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 pt-4 sm:pt-5">
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground">{messages.statistics.noDataAvailable}</p>
        ) : (
          items.map((item) => {
            const percentage = totalCostMicros > 0 ? (item.costMicros / totalCostMicros) * 100 : 0;
            return (
              <div key={`${item.label}-${item.costMicros}`} className="flex flex-col gap-2">
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="max-w-[16rem] truncate font-medium">{item.label}</span>
                  <span className="text-muted-foreground tabular-nums">
                    {formatMoneyMicros(item.costMicros, currencySymbol, currencyCode, 2, 6, locale)}
                  </span>
                </div>
                <Progress className="h-1.5 bg-primary/12" value={percentage} />
              </div>
            );
          })
        )}
      </CardContent>
    </Card>
  );
}
