import { Activity, CheckCircle2, XCircle } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { AnimatedListItem } from "@/components/AnimatedListItem";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { formatMoneyMicros, resolveSpendTrustState } from "@/lib/costing";
import { cn } from "@/lib/utils";
import type { DashboardRecentActivityItem } from "@/lib/types";
import { OperatorEmptyState, OperatorValueBadge } from "@/shared/design-system";

interface RecentActivityCardProps {
  clearRecentRequestHighlight: (requestId: number) => void;
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string;
  modelDisplayNames: Map<string, string>;
  onSelectRequest: (requestId: number) => void;
  recentNewIds: Set<number>;
  recentActivityItems: DashboardRecentActivityItem[];
}

export function RecentActivityCard({
  clearRecentRequestHighlight,
  formatTime,
  modelDisplayNames,
  onSelectRequest,
  recentNewIds,
  recentActivityItems,
}: RecentActivityCardProps) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, locale, messages } = useLocale();

  return (
    <Card className="md:col-span-2 lg:col-span-4">
      <CardHeader>
        <CardTitle>{messages.dashboard.recentActivity}</CardTitle>
        <CardDescription>{messages.dashboard.recentActivityDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        {recentActivityItems.length === 0 ? (
          <OperatorEmptyState
            icon={<ActivityEmptyIcon />}
            title={messages.dashboard.noRecentActivity}
            description={messages.dashboard.noRecentActivityDescription}
          />
        ) : (
          <div className="flex flex-col gap-4">
            {recentActivityItems.map((activity) => {
              const requestLogId = activity.request_log_id;
              const isSuccess = activity.status_code >= 200 && activity.status_code < 300;
              const spendTrust = resolveSpendTrustState(
                {
                  costMicros: activity.total_cost_user_currency_micros,
                  priced: activity.priced_flag,
                  unpricedReason: activity.unpriced_reason,
                },
                currencyState,
              );

              return (
                <AnimatedListItem
                  key={requestLogId}
                  isNew={recentNewIds.has(requestLogId)}
                  animation="left"
                  onAnimationEnd={() => clearRecentRequestHighlight(requestLogId)}
                  className="border-b pb-4 last:border-0 last:pb-0"
                >
                  <button
                    type="button"
                    className="flex w-full items-center justify-between rounded-lg px-1 py-1 text-left transition-colors hover:bg-surface-container-low focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                    onClick={() => onSelectRequest(requestLogId)}
                  >
                    <div className="flex items-center gap-4">
                      <div
                        className={cn(
                          "flex h-9 w-9 items-center justify-center rounded-full border",
                          isSuccess
                            ? "border-success/25 bg-success/10 text-success"
                            : "border-destructive/30 bg-destructive/10 text-destructive",
                        )}
                      >
                        {isSuccess ? (
                          <CheckCircle2 className="size-5" />
                        ) : (
                          <XCircle className="size-5" />
                        )}
                      </div>
                      <div className="flex flex-col gap-1">
                        <div className="flex flex-col gap-1">
                          <p className="text-sm font-medium leading-none">
                            {activity.model_label || modelDisplayNames.get(activity.model_id) || activity.model_id}
                          </p>
                          <p className="text-xs text-muted-foreground">{activity.model_id}</p>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {formatTime(activity.created_at, {
                            hour: "numeric",
                            minute: "numeric",
                            second: "numeric",
                          })}{" "}
                          - {formatNumber(activity.response_time_ms)}ms
                        </p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 text-right">
                      <div className="hidden sm:block">
                        <p className="text-sm font-medium">
                          {messages.requestLogs.totalTokens}: {formatNumber(activity.total_tokens ?? 0)}
                        </p>
                        {spendTrust === "unpriced" ? (
                          <p className="text-xs font-medium text-destructive">
                            {messages.spendTrust.unpriced}
                          </p>
                        ) : (
                          <p className="text-xs text-muted-foreground">
                            {formatMoneyMicros(
                              activity.total_cost_user_currency_micros,
                              activity.report_currency_symbol ?? undefined,
                              undefined,
                              2,
                              6,
                              locale,
                            )}
                          </p>
                        )}
                      </div>
                      <OperatorValueBadge
                        label={String(activity.status_code)}
                        intent={isSuccess ? "success" : "danger"}
                      />
                    </div>
                  </button>
                </AnimatedListItem>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ActivityEmptyIcon() {
  return <Activity className="h-6 w-6" />;
}
