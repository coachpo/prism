import { useMemo } from "react";
import { useProfileContext } from "@/context/ProfileContext";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { useRequestLogPageState } from "./request-logs/useRequestLogPageState";
import { useRequestLogDetail } from "./request-logs/useRequestLogDetail";
import { useRequestLogsPageData } from "./request-logs/useRequestLogsPageData";
import { RequestFocusBanner } from "./request-logs/RequestFocusBanner";
import { FiltersBar } from "./request-logs/FiltersBar";
import { RequestLogsTable } from "./request-logs/RequestLogsTable";
import { RequestLogDetailSheet } from "./request-logs/RequestLogDetailSheet";
import { SearchX } from "lucide-react";
import { Button } from "@/components/ui/button";
import { OperatorCallout, OperatorEmptyState, OperatorPageHeader, OperatorPageShell } from "@/shared/design-system";

export function RequestLogsPage() {
  const { revision } = useProfileContext();
  const { format } = useTimezone();
  const { messages } = useLocale();
  const actions = useRequestLogPageState();
  const { state, isExactMode } = actions;

  const { items, total, loading, error, filterOptions, filterOptionsLoaded, refresh } =
    useRequestLogsPageData({ revision, state, enabled: !isExactMode });

  const selectedRequestId = useMemo(() => {
    if (isExactMode) {
      const parsedRequestId = Number(state.request_id);
      return Number.isFinite(parsedRequestId) ? parsedRequestId : null;
    }

    const parsedSelectedRequestId = Number(state.selected_request_id);
    return Number.isFinite(parsedSelectedRequestId) && parsedSelectedRequestId > 0 ? parsedSelectedRequestId : null;
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

  const surfaceError = error ?? detailError;
  const showExactNotFound = isExactMode && !detailLoading && detailNotFound;
  const listVisibleRequestId = useMemo(
    () => items.find((item) => item.id === selectedRequestId)?.id ?? selectedRequestId,
    [items, selectedRequestId],
  );

  const sheetOpen = selectedRequest !== null;

  const handleSelectRequest = (id: number) => {
    actions.selectRequest(id);
  };

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
      />

      {isExactMode && (
        <RequestFocusBanner
          requestId={state.request_id}
          onExit={actions.clearRequest}
        />
      )}

      {!isExactMode && (
        <FiltersBar
          actions={actions}
          filterOptions={filterOptions}
          filterOptionsLoaded={filterOptionsLoaded}
          onRefresh={refresh}
          isRefreshing={loading}
        />
      )}

      {surfaceError && (
        <OperatorCallout intent="danger" description={surfaceError} />
      )}

      {showExactNotFound ? (
        <OperatorEmptyState
          className="rounded-xl border border-outline-variant bg-surface py-24 shadow-operator-panel"
          testId="request-log-not-found"
          icon={<SearchX className="h-6 w-6" />}
          title={messages.requestLogs.requestNotFound}
          description={messages.requestLogs.requestNotFoundDescription(state.request_id)}
          action={(
            <Button variant="outline" onClick={actions.clearRequest}>
              {messages.requestLogs.returnToRequestList}
            </Button>
          )}
        />
      ) : (
        <RequestLogsTable
          items={items}
          total={total}
          loading={loading}
          limit={state.limit}
          offset={state.offset}
          activeRequestId={listVisibleRequestId ?? null}
          onSelectRequest={handleSelectRequest}
          onSetLimit={actions.setLimit}
          onNextPage={() => actions.goToNextPage(total)}
          onPreviousPage={actions.goToPreviousPage}
          formatTimestamp={format}
        />
      )}

      <RequestLogDetailSheet
        request={selectedRequest}
        open={sheetOpen}
        onClose={handleCloseRequest}
        formatTimestamp={format}
      />
    </OperatorPageShell>
  );
}
