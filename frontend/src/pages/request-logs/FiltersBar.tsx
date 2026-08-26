import { Bookmark, RefreshCw, X } from "lucide-react";
import { useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import type { RequestLogFilterOptions as FilterOptions } from "./requestLogQuery";
import type { RequestLogPageActions } from "./useRequestLogPageState";
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
  const { state, hasActiveFilters } = actions;
  const [savedViews, setSavedViews] = useState<SavedRequestLogView[]>(() => loadSavedViews());
  const [viewsOpen, setViewsOpen] = useState(false);
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
    <Card className="operator-section-surface">
      <CardContent className="flex flex-col gap-3 p-4">
        <div className="flex flex-wrap items-center gap-2" aria-label={messages.requestLogs.triageLabel ?? "快捷筛选"}>
          <TriageChip
            active={!anyTriageActive}
            label={messages.requestLogs.allColumns}
            onClick={() => actions.clearTriage()}
          />
          <TriageChip
            active={finalFailedActive}
            label={messages.requestLogs.finalFailedChip ?? "最终失败"}
            onClick={() => actions.setIngressFinalResult(finalFailedActive ? "" : "failed")}
          />
          <TriageChip
            active={failoverActive}
            label={messages.requestLogs.confirmedFailoverChip ?? "确认故障转移"}
            onClick={() => actions.setConfirmedFailover(!failoverActive)}
          />
          <TriageChip
            active={unpricedActive}
            label={messages.requestLogs.unpricedOnly}
            onClick={() => actions.setPricingStatus(unpricedActive ? "all" : "unpriced")}
          />
        </div>

        <FiltersBarPrimaryFilters
          actions={actions}
          filterOptions={filterOptions}
          filterOptionsLoaded={filterOptionsLoaded}
          state={state}
        />

        <div className="flex flex-wrap items-center justify-end gap-2">
          <div className="relative mr-auto">
            <Button
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              aria-expanded={viewsOpen}
              aria-haspopup="menu"
              onClick={() => setViewsOpen((open) => !open)}
            >
              <Bookmark className="h-3.5 w-3.5" />
              {messages.requestLogs.savedViewsLabel ?? "保存视图"}
            </Button>
            {viewsOpen ? (
              <>
                <button
                  type="button"
                  aria-label="关闭保存视图"
                  className="fixed inset-0 z-40 cursor-default"
                  onClick={() => setViewsOpen(false)}
                />
                <div
                  role="menu"
                  data-testid="request-log-saved-views"
                  className="absolute left-0 z-50 mt-1 w-72 rounded-lg border border-border bg-panel p-2"
                >
                  <div className="flex gap-1.5 pb-2">
                    <Input
                      ref={viewNameRef}
                      value={viewName}
                      placeholder={messages.requestLogs.savedViewNamePlaceholder ?? "视图名称"}
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
                      {messages.requestLogs.saveView ?? "保存"}
                    </Button>
                  </div>
                  <div className="max-h-64 overflow-auto">
                    {savedViews.length === 0 ? (
                      <p className="px-2 py-3 text-center text-xs text-muted-foreground">
                        {messages.requestLogs.noSavedViews ?? "尚无保存视图"}
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
                            aria-label={`删除视图 ${view.name}`}
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
          {hasActiveFilters && (
            <Button
              variant="ghost"
              size="sm"
              className="h-8 gap-1.5 text-xs"
              onClick={actions.clearFilters}
            >
              <X className="h-3 w-3" />
              {messages.statistics.clearFilters}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
