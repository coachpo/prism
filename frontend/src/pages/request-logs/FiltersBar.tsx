import { RefreshCw, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { FilterOptions } from "./useRequestLogsPageData";
import type { RequestLogPageActions } from "./useRequestLogPageState";
import { FiltersBarPrimaryFilters } from "./FiltersBarPrimaryFilters";

interface FiltersBarProps {
  actions: RequestLogPageActions;
  filterOptions: FilterOptions;
  filterOptionsLoaded: boolean;
  onRefresh: () => void;
  isRefreshing: boolean;
}

export function FiltersBar({ actions, filterOptions, filterOptionsLoaded, onRefresh, isRefreshing }: FiltersBarProps) {
  const { messages } = useLocale();
  const { state, hasActiveFilters } = actions;

  return (
    <Card className="border-border/70 bg-card/95 shadow-sm backdrop-blur supports-[backdrop-filter]:bg-card/85">
      <CardContent className="space-y-3 p-4">
        <FiltersBarPrimaryFilters
          actions={actions}
          filterOptions={filterOptions}
          filterOptionsLoaded={filterOptionsLoaded}
          state={state}
        />

        <div className="flex flex-wrap justify-end gap-2">
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
            <Button variant="ghost" size="sm" className="h-8 gap-1.5 text-xs" onClick={actions.clearFilters}>
              <X className="h-3 w-3" />
              {messages.statistics.clearFilters}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
