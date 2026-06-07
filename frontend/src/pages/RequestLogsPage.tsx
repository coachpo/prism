import { useMemo, useState } from "react";
import { EmptyState } from "@/components/EmptyState";
import { PageHeader } from "@/components/PageHeader";
import { SemanticCallout } from "@/components/SemanticCallout";
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
import type { DetailTab } from "./request-logs/queryParams";

export function RequestLogsPage() {
  const { revision } = useProfileContext();
  const { format } = useTimezone();
  const { messages } = useLocale();
  const [tableSelectedRequestId, setTableSelectedRequestId] = useState<number | null>(null);
  const [tableSelectedTab, setTableSelectedTab] = useState<DetailTab>("overview");
  const actions = useRequestLogPageState();
  const { state, isExactMode } = actions;

  const { items, total, loading, error, filterOptions, filterOptionsLoaded, refresh } =
    useRequestLogsPageData({ revision, state, enabled: !isExactMode });

  const selectedRequestId = useMemo(() => {
    if (isExactMode) {
      const parsedRequestId = Number(state.request_id);
      return Number.isFinite(parsedRequestId) ? parsedRequestId : null;
    }

    return tableSelectedRequestId;
  }, [isExactMode, state.request_id, tableSelectedRequestId]);

  const {
    request: selectedRequest,
    loading: detailLoading,
    error: detailError,
    notFound: detailNotFound,
  } = useRequestLogDetail({
    requestId: selectedRequestId,
    enabled: selectedRequestId !== null,
  });

  const currentActiveTab = isExactMode ? state.detail_tab : tableSelectedTab;
  const surfaceError = error ?? detailError;
  const showExactNotFound = isExactMode && !detailLoading && detailNotFound;
  const listVisibleRequestId = useMemo(
    () => items.find((item) => item.id === selectedRequestId)?.id ?? selectedRequestId,
    [items, selectedRequestId],
  );

  const sheetOpen = selectedRequest !== null;

  const handleSelectRequest = (id: number) => {
    setTableSelectedRequestId(id);
    setTableSelectedTab("overview");
  };

  const handleCloseRequest = () => {
    if (isExactMode) {
      actions.clearRequest();
      return;
    }

    setTableSelectedRequestId(null);
    setTableSelectedTab("overview");
  };

  const handleTabChange = (tab: DetailTab) => {
    if (isExactMode) {
      actions.setDetailTab(tab);
      return;
    }

    setTableSelectedTab(tab);
  };

  return (
    <div className="space-y-6 pb-8">
      <PageHeader
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
        <SemanticCallout intent="danger" description={surfaceError} />
      )}

      {showExactNotFound ? (
        <EmptyState
          className="rounded-xl border border-border/70 bg-card py-24 shadow-sm"
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
        activeTab={currentActiveTab}
        onTabChange={handleTabChange}
        onClose={handleCloseRequest}
        formatTimestamp={format}
      />
    </div>
  );
}
