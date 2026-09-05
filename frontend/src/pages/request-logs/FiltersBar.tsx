import { Bookmark, ChevronDown, RefreshCw, X } from "lucide-react";
import { useId, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import type { RequestLogFilterOptions as FilterOptions } from "./requestLogQuery";
import type { RequestLogPageActions } from "./useRequestLogPageState";
import { countHiddenRequestLogFilters } from "./FiltersBar.constants";
import { FiltersBarPrimaryFilters } from "./FiltersBarPrimaryFilters";
import {
  applySavedView,
  deleteRequestLogView,
  loadSavedViews,
  saveRequestLogView,
  type SavedRequestLogView,
} from "./requestLogSavedViews";

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
  const [savedViews, setSavedViews] = useState<SavedRequestLogView[]>(() => loadSavedViews());
  const [viewsOpen, setViewsOpen] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const hiddenFilterCount = countHiddenRequestLogFilters(state);
  const [viewName, setViewName] = useState("");
  const [savingView, setSavingView] = useState(false);
  const viewNameRef = useRef<HTMLInputElement>(null);

  const refreshViews = () => setSavedViews(loadSavedViews());

  const handleSaveView = () => {
    const name = viewName.trim();
    if (!name) {
      viewNameRef.current?.focus();
      return;
    }
    saveRequestLogView(name, state);
    setViewName("");
    setSavingView(false);
    refreshViews();
  };

  const handleApplyView = (view: SavedRequestLogView) => {
    const next = applySavedView(view, state);
    actions.replaceState(next);
    setViewsOpen(false);
  };

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
            <div className="relative">
              <Button
                variant="outline"
                size="sm"
                className="h-8 gap-1.5 text-xs"
                aria-expanded={viewsOpen}
                aria-haspopup="menu"
                onClick={() => setViewsOpen((open) => !open)}
              >
                <Bookmark className="h-3.5 w-3.5" />
                {messages.requestLogs.savedViewsLabel}
              </Button>
              {viewsOpen ? (
                <>
                  <button
                    type="button"
                    aria-label={messages.requestLogs.closeSavedViews}
                    className="fixed inset-0 z-40 cursor-default"
                    onClick={() => setViewsOpen(false)}
                  />
                  <div
                    role="menu"
                    data-testid="request-log-saved-views"
                    className="absolute right-0 z-50 mt-1 w-72 rounded-lg border border-border bg-panel p-2"
                  >
                    <div className="flex gap-1.5 pb-2">
                      <Input
                        ref={viewNameRef}
                        value={viewName}
                        aria-label={messages.requestLogs.savedViewNamePlaceholder}
                        placeholder={messages.requestLogs.savedViewNamePlaceholder}
                        className="h-8 text-xs"
                        onChange={(event) => setViewName(event.target.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") {
                            event.preventDefault();
                            handleSaveView();
                          }
                        }}
                      />
                      <Button
                        variant="secondary"
                        size="sm"
                        className="h-8 shrink-0 text-xs"
                        onClick={() => {
                          setSavingView(true);
                          handleSaveView();
                        }}
                        disabled={savingView}
                      >
                        {messages.requestLogs.saveView}
                      </Button>
                    </div>
                    <div className="max-h-64 overflow-auto">
                      {savedViews.length === 0 ? (
                        <p className="px-2 py-3 text-center text-xs text-muted-foreground">
                          {messages.requestLogs.noSavedViews}
                        </p>
                      ) : (
                        savedViews.map((view) => (
                          <div
                            key={view.id}
                            role="menuitem"
                            className="group flex items-center gap-1 rounded-md px-2 py-1.5 text-xs hover:bg-inset"
                          >
                            <button
                              type="button"
                              className="min-w-0 flex-1 truncate text-left text-foreground"
                              onClick={() => handleApplyView(view)}
                            >
                              {view.name}
                            </button>
                            <button
                              type="button"
                              aria-label={messages.requestLogs.deleteSavedView(view.name)}
                              className="shrink-0 rounded p-1 text-muted-foreground opacity-0 hover:bg-destructive/10 hover:text-destructive group-hover:opacity-100"
                              onClick={() => {
                                deleteRequestLogView(view.id);
                                refreshViews();
                              }}
                            >
                              <X className="size-3" />
                            </button>
                          </div>
                        ))
                      )}
                    </div>
                  </div>
                </>
              ) : null}
            </div>
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
