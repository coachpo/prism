import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros } from "@/lib/costing";
import { Progress } from "@/components/ui/progress";

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
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground">{messages.statistics.noDataAvailable}</p>
        ) : (
          items.map((item) => {
            const percentage = totalCostMicros > 0 ? (item.costMicros / totalCostMicros) * 100 : 0;
            return (
              <div key={`${item.label}-${item.costMicros}`} className="space-y-1">
                <div className="flex items-center justify-between text-sm">
                  <span className="font-medium truncate max-w-[180px]">{item.label}</span>
                  <span className="text-muted-foreground tabular-nums">
                    {formatMoneyMicros(item.costMicros, currencySymbol, currencyCode, 2, 6, locale)}
                  </span>
                </div>
                <Progress value={percentage} className="h-2" />
              </div>
            );
          })
        )}
      </CardContent>
    </Card>
  );
}
