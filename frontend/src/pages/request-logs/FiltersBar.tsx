import { ChevronDown, RefreshCw } from "lucide-react";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import type { RequestLogFilterOptions as FilterOptions } from "./requestLogQuery";
import type { RequestLogPageActions } from "./useRequestLogPageState";
import { countHiddenRequestLogFilters } from "./FiltersBar.constants";
import { FiltersBarPrimaryFilters } from "./FiltersBarPrimaryFilters";
import { RequestViewsPanel } from "./RequestViewsPanel";

interface FiltersBarProps {
  actions: RequestLogPageActions;
  filterOptions: FilterOptions;
  filterOptionsLoaded: boolean;
  onRefresh: () => void;
  isRefreshing: boolean;
}
function TriageChip({
  active,
  label,
  onClick,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "rounded-full border px-3 py-1 text-xs font-medium transition-colors",
        active
          ? "border-primary/40 bg-primary/10 text-primary"
          : "border-border bg-panel text-muted-foreground hover:bg-inset",
      )}
    >
      {label}
    </button>
  );
}

export function FiltersBar({ actions, filterOptions, filterOptionsLoaded, onRefresh, isRefreshing }: FiltersBarProps) {
  const { messages } = useLocale();
  const { state } = actions;
  const headingId = useId();
  const [moreOpen, setMoreOpen] = useState(false);
  const hiddenFilterCount = countHiddenRequestLogFilters(state);

  const finalFailedActive = state.ingress_final_result === "failed";
  const failoverActive = state.confirmed_failover;
  const unpricedActive = state.pricing_status === "unpriced";

  const anyTriageActive = finalFailedActive || failoverActive || unpricedActive;

  return (
    // 区块标题是长页的路标：卡片必须由一个真 h2 命名，否则读屏按 H 键
    // 从页标题直接跳过整张筛选卡。
    <Card className="operator-section-surface" aria-labelledby={headingId}>
      <CardContent className="flex flex-col gap-2 p-3">
        <h2 id={headingId} className="sr-only">
          {messages.requestLogs.filtersSectionTitle}
        </h2>
        {/* 分诊芯片与卡内动作并排：一个几乎不改的筛选表单不该拿走 339px，
            而真正要看的数据行只剩 4 行。 */}
        <div className="flex flex-wrap items-center gap-2">
          <div
            className="flex flex-wrap items-center gap-2"
            role="group"
            aria-label={messages.requestLogs.triageLabel}
          >
            <TriageChip
              active={!anyTriageActive}
              label={messages.requestLogs.triageAll}
              onClick={() => actions.clearTriage()}
            />
            <TriageChip
              active={finalFailedActive}
              label={messages.requestLogs.finalFailedChip}
              onClick={() => actions.setIngressFinalResult(finalFailedActive ? "" : "failed")}
            />
            <TriageChip
              active={failoverActive}
              label={messages.requestLogs.confirmedFailoverChip}
              onClick={() => actions.setConfirmedFailover(!failoverActive)}
            />
            <TriageChip
              active={unpricedActive}
              label={messages.requestLogs.unpricedOnly}
              onClick={() => actions.setPricingStatus(unpricedActive ? "all" : "unpriced")}
            />
          </div>

          <div className="ml-auto flex flex-wrap items-center gap-2">
            {/* 面板关着时这个计数是唯一的提示，深链带进来的条件不能悄悄筛。 */}
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              aria-expanded={moreOpen}
              aria-controls="request-logs-more-filters"
              data-testid="request-logs-more-filters-toggle"
              onClick={() => setMoreOpen((open) => !open)}
            >
              <ChevronDown className={cn("size-3.5 transition-transform", moreOpen && "rotate-180")} />
              {messages.requestLogs.moreFilters}
              {hiddenFilterCount > 0 ? (
                <span
                  data-testid="more-filters-count"
                  className="inline-flex h-4 min-w-4 items-center justify-center rounded-[4px] bg-primary px-1 font-mono text-[10px] tabular-nums text-on-primary"
                >
                  {hiddenFilterCount}
                </span>
              ) : null}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              onClick={onRefresh}
              disabled={isRefreshing}
              aria-label={messages.requestLogs.refreshRequestLogs}
              title={messages.requestLogs.refreshRequestLogs}
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>

        {/* 清除筛选只留生效筛选条那一处：同一个动作在同屏渲染两次，
            操作者要先判断这两个按钮是不是一回事。 */}
        <RequestViewsPanel actions={actions} filterOptions={filterOptions} filterOptionsLoaded={filterOptionsLoaded} />
        <FiltersBarPrimaryFilters
          actions={actions}
          filterOptions={filterOptions}
          filterOptionsLoaded={filterOptionsLoaded}
          moreOpen={moreOpen}
          state={state}
        />
      </CardContent>
    </Card>
  );
}
