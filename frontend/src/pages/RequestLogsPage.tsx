import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { useRequestLogPageState } from "./request-logs/useRequestLogPageState";
import { useRequestLogDetail } from "./request-logs/useRequestLogDetail";
import { useRequestLogsPageData } from "./request-logs/useRequestLogsPageData";
import { downloadRequestLogsCsv } from "./request-logs/requestLogsCsv";
import {
  DEFAULT_COLUMN_PREFERENCES,
  loadColumnPreferences,
  saveColumnPreferences,
} from "./request-logs/requestLogColumnPreferences";
import { RequestFocusBanner } from "./request-logs/RequestFocusBanner";
import { FiltersBar } from "./request-logs/FiltersBar";
import { RequestLogsTable } from "./request-logs/RequestLogsTable";
import { IngressChainsTable } from "./request-logs/IngressChainsTable";
import { RequestLogDetailSheet } from "./request-logs/RequestLogDetailSheet";
import { Download, SearchX } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  OperatorCallout,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
  OperatorStalenessBadge,
} from "@/shared/design-system";
import { ActiveFilterChips } from "./request-logs/ActiveFilterChips";
import { ColumnToggleMenu } from "./request-logs/RequestLogsTable";
import { RequestLogsViewToolbar } from "./request-logs/RequestLogsViewToolbar";
import {
  decodeObserveReturn,
  observeReturnToSearch,
} from "@/lib/observeReturn";

export function RequestLogsPage() {
  const { format } = useTimezone();
  const { messages } = useLocale();
  const actions = useRequestLogPageState();
  const { state, isExactMode } = actions;
  const [columnPreferences, setColumnPreferences] = useState(() =>
    loadColumnPreferences(),
  );

  const handleToggleColumn = useCallback((key: string) => {
    setColumnPreferences((current) => {
      const nextKeys = current.visibleKeys.includes(key)
        ? current.visibleKeys.filter((visibleKey) => visibleKey !== key)
        : [...current.visibleKeys, key];
      const next = { version: 4 as const, visibleKeys: nextKeys };
      saveColumnPreferences(next);
      return next;
    });
  }, []);

  const handleResetColumns = useCallback(() => {
    const defaults = DEFAULT_COLUMN_PREFERENCES;
    saveColumnPreferences(defaults);
    setColumnPreferences(defaults);
  }, []);

  const {
    items,
    total,
    totalIsExact,
    hasMoreRows,
    loading,
    error,
    stale,
    lastLoadedAt,
    filterOptions,
    filterOptionsLoaded,
    refresh,
    loadMoreChainRows,
    nextChainCursor,
    previousChainCursor,
    chainPageStart,
    hasMoreChains,
    chains,
    chainPageCounts,
    coverage,
    readKind,
    chainRowReads,
  } = useRequestLogsPageData({ revision: 0, state, enabled: !isExactMode });

  // A page turn withdraws the old rows for skeletons; a same-scope refresh
  // keeps them rendered until its own read resolves.
  const replacingRows = loading && readKind === "replace";

  const selectedRequestId = useMemo(() => {
    if (isExactMode) {
      return /^[0-9]+$/.test(state.request_id) ? state.request_id : null;
    }
    return /^[0-9]+$/.test(state.selected_request_id)
      ? state.selected_request_id
      : null;
  }, [isExactMode, state.request_id, state.selected_request_id]);

  const {
    request: selectedRequest,
    loading: detailLoading,
    error: detailError,
    notFound: detailNotFound,
  } = useRequestLogDetail({
    requestId: selectedRequestId,
    enabled: selectedRequestId !== null,
  });

  // A failed list read owns the table area (Honesty Contract): it replaces the
  // table instead of letting an empty list and a zero total speak for it. When
  // the retained rows are still the ones this query asked for, the read keeps
  // them and the staleness badge carries the failure instead.
  const listReadFailed = error !== null && !stale;
  const showExactNotFound = isExactMode && !detailLoading && detailNotFound;
  const listVisibleRequestId = useMemo(() => {
    const parsed = items.find(
      (item) => item.request_log_id === selectedRequestId,
    );
    return parsed ? selectedRequestId : selectedRequestId;
  }, [items, selectedRequestId]);

  const sheetOpen = selectedRequest !== null;
  const selectedAttemptIndex =
    state.view === "attempts" && selectedRequestId !== null
      ? items.findIndex((item) => item.request_log_id === selectedRequestId)
      : -1;
  const observeReturn = decodeObserveReturn(state.observe_return);

  const handleSelectRequest = useCallback(
    (id: string) => {
      actions.selectRequest(id);
    },
    [actions],
  );

  useEffect(() => {
    if (!sheetOpen || state.view !== "attempts") return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        event.key === "ArrowDown" &&
        selectedAttemptIndex >= 0 &&
        selectedAttemptIndex < items.length - 1
      ) {
        event.preventDefault();
        handleSelectRequest(items[selectedAttemptIndex + 1].request_log_id);
      } else if (event.key === "ArrowUp" && selectedAttemptIndex > 0) {
        event.preventDefault();
        handleSelectRequest(items[selectedAttemptIndex - 1].request_log_id);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [handleSelectRequest, items, selectedAttemptIndex, sheetOpen, state.view]);

  const handleCloseRequest = () => {
    if (isExactMode) {
      actions.clearRequest();
      return;
    }

    actions.clearSelectedRequest();
  };

  return (
    <OperatorPageShell className="pb-8">
      <OperatorPageHeader
        title={messages.requestLogs.requestLogsTitle}
        description={messages.requestLogs.requestLogsDescription}
        actions={
          <Button
            type="button"
            variant="outline"
            onClick={() => void downloadRequestLogsCsv(state)}
            data-testid="request-logs-export-csv"
          >
            <Download data-icon="inline-start" />
            {messages.requestLogs.exportCsv}
          </Button>
        }
      />

      {isExactMode && (
        <RequestFocusBanner
          requestId={state.request_id}
          onExit={actions.clearRequest}
        />
      )}

      {observeReturn ? (
        <OperatorCallout
          intent="info"
          description={messages.requestLogs.investigationReturnDescription}
          action={
            <Link
              to="/observe"
              search={observeReturnToSearch(observeReturn) as never}
              className="inline-flex min-h-8 items-center rounded-md px-2 text-sm font-medium text-primary hover:bg-inset"
            >
              {messages.requestLogs.returnToRoutingHealth}
            </Link>
          }
        />
      ) : null}

      <FiltersBar
        actions={actions}
        filterOptions={filterOptions}
        filterOptionsLoaded={filterOptionsLoaded}
        onRefresh={refresh}
        isRefreshing={loading}
      />

      <ActiveFilterChips actions={actions} />

      {detailError && (
        <OperatorCallout intent="danger" description={detailError} />
      )}

      {coverage && coverage.complete === false ? (
        <OperatorCallout
          intent="warning"
          title={messages.requestLogs.retentionCoverageTitle}
        >
          <p>{messages.requestLogs.retentionCoverageDescription}</p>
          <Link
            className="mt-2 inline-flex text-sm font-medium text-primary underline-offset-4 hover:underline"
            to="/system/settings?scope=instance&section=retention"
          >
            {messages.requestLogs.retentionCoverageLink}
          </Link>
        </OperatorCallout>
      ) : null}

      {showExactNotFound ? (
        <OperatorEmptyState
          className="rounded-lg border border-border bg-panel py-24"
          testId="request-log-not-found"
          icon={<SearchX className="h-6 w-6" />}
          title={messages.requestLogs.requestNotFound}
          description={messages.requestLogs.requestNotFoundDescription(
            state.request_id,
          )}
          action={
            <Button variant="outline" onClick={actions.clearRequest}>
              {messages.requestLogs.returnToRequestList}
            </Button>
          }
        />
      ) : (
        <>
          <RequestLogsViewToolbar
            view={state.view}
            onViewChange={actions.setView}
            summary={
              // No successful read stands behind these counts, so the toolbar
              // states nothing rather than reporting a fabricated zero.
              listReadFailed
                ? undefined
                : state.view === "ingress_chains"
                  ? messages.requestLogs.chainCounts(
                      String(chainPageCounts.ingress),
                      String(chainPageCounts.attempts),
                      String(chainPageCounts.rows),
                    )
                  : messages.requestLogs.totalRowsSummary(String(total))
            }
          >
            <ColumnToggleMenu
              visibleColumns={columnPreferences.visibleKeys}
              onToggleColumn={handleToggleColumn}
              onResetColumns={handleResetColumns}
            />
          </RequestLogsViewToolbar>
          {stale && lastLoadedAt ? (
            <OperatorStalenessBadge
              className="self-start"
              data-testid="request-logs-stale-badge"
              label={messages.honesty.lastSuccessful(format(lastLoadedAt))}
              reason={error ?? undefined}
            />
          ) : null}
          {listReadFailed ? (
            <OperatorErrorState
              testId="request-logs-load-error"
              title={messages.requestLogs.loadFailed}
              description={messages.honesty.readFailedDescription}
              details={error}
              detailsLabel={messages.honesty.viewDetails}
              action={
                <OperatorRetryButton onClick={refresh}>
                  {messages.common.retry}
                </OperatorRetryButton>
              }
            />
          ) : state.view === "ingress_chains" ? (
            <IngressChainsTable
              chains={chains}
              total={total}
              hasPreviousChains={previousChainCursor !== null}
              hasMoreChains={hasMoreChains}
              chainPageStart={chainPageStart}
              chainPageCounts={chainPageCounts}
              replacing={replacingRows}
              chainRowReads={chainRowReads}
              onLoadPreviousChains={() =>
                actions.setChainCursor(previousChainCursor ?? "")
              }
              onLoadNextChains={() => {
                if (nextChainCursor) actions.setChainCursor(nextChainCursor);
              }}
              onLoadMoreRows={loadMoreChainRows}
              onSelectRow={handleSelectRequest}
              loading={loading}
            />
          ) : (
            <RequestLogsTable
              items={items}
              total={total}
              totalIsExact={totalIsExact}
              hasMoreRows={hasMoreRows}
              loading={loading}
              replacing={replacingRows}
              limit={state.limit}
              offset={state.offset}
              activeRequestId={listVisibleRequestId ?? null}
              onSelectRequest={handleSelectRequest}
              onSetLimit={actions.setLimit}
              visibleColumns={columnPreferences.visibleKeys}
              sortBy={state.sort_by}
              sortOrder={state.sort_order}
              onSortChange={(key) => {
                const nextOrder =
                  state.sort_by === key && state.sort_order === "desc"
                    ? "asc"
                    : "desc";
                actions.setSort(key, nextOrder);
              }}
              onNextPage={() => {
                if (
                  state.view === "ingress_chains" &&
                  hasMoreChains &&
                  nextChainCursor
                ) {
                  actions.setChainCursor(nextChainCursor);
                } else if (hasMoreRows) {
                  actions.goToNextPage(total);
                }
              }}
              onPreviousPage={() => {
                if (state.view === "ingress_chains") {
                  actions.setChainCursor("");
                } else {
                  actions.goToPreviousPage();
                }
              }}
              formatTimestamp={format}
            />
          )}
        </>
      )}

      <RequestLogDetailSheet
        request={selectedRequest}
        open={sheetOpen}
        onClose={handleCloseRequest}
        formatTimestamp={format}
        canPrevious={selectedAttemptIndex > 0}
        canNext={
          selectedAttemptIndex >= 0 && selectedAttemptIndex < items.length - 1
        }
        onPrevious={() => {
          if (selectedAttemptIndex > 0)
            handleSelectRequest(items[selectedAttemptIndex - 1].request_log_id);
        }}
        onNext={() => {
          if (
            selectedAttemptIndex >= 0 &&
            selectedAttemptIndex < items.length - 1
          )
            handleSelectRequest(items[selectedAttemptIndex + 1].request_log_id);
        }}
      />
    </OperatorPageShell>
  );
}
