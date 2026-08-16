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
import { formatMoneyMicros } from "@/lib/costing";
import type { SpendingSummary } from "@/lib/types";
import {
  OperatorClippedBadge,
  OperatorErrorState,
  OperatorKpiCard,
  OperatorRetryButton,
  OperatorSectionCard,
} from "@/shared/design-system";

/** Windows the spending endpoint actually supports, named for what they are. */
export type ModelCostWindow = "today" | "last_7_days" | "all";

interface ModelCostCardsProps {
  currencyCode: string;
  currencySymbol: string;
  failed: boolean;
  loading: boolean;
  onRetry: () => void;
  onWindowChange: (window: ModelCostWindow) => void;
  spending: SpendingSummary | null;
  window: ModelCostWindow;
}

export function ModelCostCards({
  currencyCode,
  currencySymbol,
  failed,
  loading,
  onRetry,
  onWindowChange,
  spending,
  window,
}: ModelCostCardsProps) {
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.modelDetail;
  const windowLabel =
    window === "today" ? copy.costWindowToday : window === "last_7_days" ? copy.costWindow7d : copy.costWindowAll;

  return (
    <OperatorSectionCard
      title={copy.costOverview}
      description={copy.costWindowBasis(windowLabel)}
      actions={
        <Select value={window} onValueChange={(value) => onWindowChange(value as ModelCostWindow)}>
          <SelectTrigger size="sm" aria-label={copy.costWindowLabel} className="w-32">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="today">{copy.costWindowToday}</SelectItem>
              <SelectItem value="last_7_days">{copy.costWindow7d}</SelectItem>
              <SelectItem value="all">{copy.costWindowAll}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      }
      contentClassName="flex flex-col gap-3"
      data-testid="model-cost-cards"
    >
      {failed && !spending ? (
        <OperatorErrorState
          title={copy.costUnavailable}
          description={copy.costUnavailableReason}
          action={<OperatorRetryButton onClick={onRetry}>{messages.common.retry}</OperatorRetryButton>}
        />
      ) : loading && !spending ? (
        <div className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-20 rounded-lg" />
          ))}
        </div>
      ) : spending ? (
        // KPI 卡默认是 panel 底色的页面级卡片；这里它们嵌在同为 panel 的卡片内，
        // 降到 inset 底色才不是同色套同色的四个方块浮在一个方块里。
        <div className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-4 [&>[data-slot=kpi-card]]:bg-inset">
          <OperatorKpiCard
            label={copy.kpiKnownCost}
            value={formatMoneyMicros(spending.total_cost_micros, currencySymbol, currencyCode, 2, 6, locale)}
            detail={windowLabel}
            badges={
              spending.unpriced_request_count > 0 ? (
                <OperatorClippedBadge
                  label={copy.unpricedClipped}
                  reason={copy.unpricedClippedReason(formatNumber(spending.unpriced_request_count))}
                />
              ) : null
            }
          />
          <OperatorKpiCard
            label={copy.kpiSuccessfulRequests}
            value={formatNumber(spending.successful_request_count)}
            detail={windowLabel}
          />
          <OperatorKpiCard
            label={copy.kpiTotalTokens}
            value={formatNumber(spending.total_tokens)}
            detail={windowLabel}
          />
          <OperatorKpiCard
            label={copy.kpiAvgCost}
            value={formatMoneyMicros(
              spending.avg_cost_per_successful_request_micros,
              currencySymbol,
              currencyCode,
              4,
              6,
              locale,
            )}
            detail={windowLabel}
          />
        </div>
      ) : (
        <div className="grid gap-[var(--density-card-gap)] sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-20 rounded-lg" />
          ))}
        </div>
      )}
    </OperatorSectionCard>
  );
}
