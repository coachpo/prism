import { ObserveFragmentStamp } from "./ObserveFragmentStamp";
import { useObserveReadCycle } from "./observeReadCycleContext";
import { useEffect, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { describeRequestFailure } from "@/pages/request-logs/requestFailurePresentation";
import { getStreamOutcomeLabel } from "@/pages/request-logs/streamTelemetry";
import type { StreamOutcome } from "@/lib/types";
import { observe, type UsageErrorsResponse } from "@/lib/api/observability";
import { cn } from "@/lib/utils";
import { fragmentErrorFrom, type FragmentState } from "@/features/observe/useObserveFragments";
import { RetryAfterCallout } from "@/features/observe/RetryAfterCallout";
import {
  OperatorErrorState,
  OperatorRetryButton,
  OperatorStalenessBadge,
} from "@/shared/design-system";
import {
  httpStatusSelection,
  streamErrorKindSelection,
  streamOutcomeSelection,
  type ObserveErrorSelection,
} from "@/features/observe/observeErrorSelection";
import type { ObserveGroupBy } from "@/features/observe/observeSearch";

/**
 * Observe error analysis panel: HTTP failure ranking and stream diagnostics
 * over the finalized cohort. Every leaf carries a backend-built final_*
 * filter conjunction and deep-links into /observe/requests.
 */
export function ObserveErrorPanel({
  groupBy,
  queryContext,
  onSelect,
  onContextResolved,
  selectedKey,
  basisKey,
  contextError,
}: {
  groupBy: ObserveGroupBy;
  basisKey?: string;
  contextError?: string | null;
  queryContext: string | null;
  /** Selecting a leaf filters the adjacent stream instead of navigating away. */
  onSelect: (selection: ObserveErrorSelection) => void;
  onContextResolved: (requestsContext: UsageErrorsResponse["requests_context"], data: UsageErrorsResponse) => void;
  selectedKey: string | null;
}) {
  const { track } = useObserveReadCycle();
  const { messages } = useLocale();
  // A failed read is recovered here, next to the failure. The page's freshness
  // bar can also revive it, but it sits a screen above this block.
  const [attempt, setAttempt] = useState(0);
  const requestKey = `${basisKey ?? queryContext ?? ""}:${groupBy}`;
  const [snapshot, setSnapshot] = useState<{
    key: string;
    fragment: FragmentState<UsageErrorsResponse>;
  }>(() => ({ key: requestKey, fragment: loadingErrorsFragment() }));
  const fragment =
    snapshot.key === requestKey
      ? snapshot.fragment
      : loadingErrorsFragment();

  useEffect(() => {
    if (!queryContext) {
      return;
    }
    setSnapshot(previous => ({ key: requestKey, fragment: { ...(previous.key === requestKey ? previous.fragment : loadingErrorsFragment()), phase: "loading" } }));
    const controller = new AbortController();
    let cancelled = false;
    void track(observe
      .usageErrors(queryContext, { group_by: groupBy, limit: 10 }, controller.signal))
      .then((data) => {
        if (cancelled) return;
        setSnapshot({
          key: requestKey,
          fragment: {
            phase: "ready",
            data,
            stale: false,
            error: null,
            retryAfterMs: null,
          },
        });
        onContextResolved(data.requests_context, data);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const mapped = fragmentErrorFrom(err);
          setSnapshot((previous) => {
            const priorFragment =
              previous.key === requestKey
                ? previous.fragment
                : loadingErrorsFragment();
            return {
              key: requestKey,
              fragment: {
                ...priorFragment,
                phase: "error",
                stale: priorFragment.data !== null,
                error: mapped.error,
                retryAfterMs: mapped.retryAfterMs,
              },
            };
          });
        }
      });
    return () => {
      cancelled = true;
      controller.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupBy, queryContext, requestKey, attempt, track]);

  if (!queryContext) {
    return <section role="status" className="rounded-lg border border-border bg-inset p-4 text-sm text-muted-foreground">{messages.observe.windowUnavailable}</section>;
  }
  if (fragment.phase === "loading" && !fragment.data) {
    return <section aria-busy="true" className="rounded-lg border border-border bg-inset p-4" />;
  }
  if (fragment.phase === "error" && fragment.data === null) {
    // 读取失败：标题、下一步、状态码折进「查看详情」，并带自己的重试。
    return (
      <OperatorErrorState
        testId="observe-error-panel-error"
        title={messages.observe.windowUnavailable}
        description={
          <>
            {fragment.retryAfterMs !== null ? (
              <RetryAfterCallout retryAfterMs={fragment.retryAfterMs} />
            ) : null}
            {messages.honesty.readFailedDescription}
          </>
        }
        action={
          <OperatorRetryButton
            onClick={() => setAttempt((current) => current + 1)}
          >
            {messages.observe.retry}
          </OperatorRetryButton>
        }
      />
    );
  }

  if (fragment.data === null) {
    return <section role="status" className="rounded-lg border border-border bg-inset p-4 text-sm text-muted-foreground">{messages.observe.windowUnavailable}</section>;
  }

  const data = fragment.data;
  return (
    <section className="flex flex-col gap-4" data-testid="observe-error-panel">
      <ObserveFragmentStamp generatedAt={data.generated_at} from={data.coverage.from_time} to={data.coverage.to_time} />
      {(fragment.stale || contextError) && <OperatorStalenessBadge label={messages.observe.staleDataNote} reason={fragment.error ?? contextError ?? undefined} />}
      <div className="flex flex-wrap gap-2 text-sm">
        <span className="rounded-md bg-inset px-2 py-1 tabular-nums" data-testid="error-http-count">
          {messages.observe.httpFailedShort}: {data.summary.http_error_count}
        </span>
        <span className="rounded-md bg-inset px-2 py-1 tabular-nums" data-testid="error-stream-count">
          {messages.observe.streamFailures}: {data.summary.stream_error_count}
        </span>
        {data.summary.transport_error_count > 0 ? (
          <span
            className="rounded-md bg-inset px-2 py-1 tabular-nums"
            data-testid="error-transport-count"
          >
            {messages.requestLogs.attemptResultTransportError}:{" "}
            {data.summary.transport_error_count}
          </span>
        ) : null}
        <span className="rounded-md bg-inset px-2 py-1 tabular-nums" data-testid="error-client-count">
          {messages.observe.clientDisconnected}: {data.summary.client_disconnected_count}
        </span>
        {data.summary.diagnostic_stream_anomaly_count > 0 ? (
          <span className="rounded-md bg-inset px-2 py-1 tabular-nums" data-testid="error-anomaly-count">
            {messages.observe.streamAnomaly}: {data.summary.diagnostic_stream_anomaly_count}
          </span>
        ) : null}
      </div>
      {data.http_statuses.length > 0 ? (
        <div>
          <h3 className="mb-2 text-sm font-medium">{messages.observe.httpFailuresTitle}</h3>
          <ul className="flex flex-col gap-1">
            {data.http_statuses.map((status) => (
              <li key={status.status_code}>
                <button
                  type="button"
                  aria-pressed={selectedKey === `http:${status.status_code}`}
                  className={cn(
                    "flex w-full items-center justify-between rounded-md px-2 py-1 text-left text-sm hover:bg-inset",
                    selectedKey === `http:${status.status_code}` && "bg-primary-soft/40",
                  )}
                  onClick={() =>
                    onSelect(
                      httpStatusSelection(
                        status.status_code,
                        messages.observe.workbenchSelectionHttp(String(status.status_code)),
                        status.request_filters,
                      ),
                    )
                  }
                  data-testid={`error-status-${status.status_code}`}
                >
                  <span className="tabular-nums">HTTP {status.status_code}</span>
                  <span className="tabular-nums text-muted-foreground">
                    {status.count} · {status.percentage === null ? "—" : `${status.percentage.toFixed(1)}%`}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {data.stream_outcomes.length > 0 ? (
        <div>
          <h3 className="mb-2 text-sm font-medium">{messages.observe.streamDiagnosticsTitle}</h3>
          <ul className="flex flex-col gap-1">
            {data.stream_outcomes.map((outcome) => (
              <li key={outcome.stream_outcome}>
                <button
                  type="button"
                  aria-pressed={selectedKey === `stream:${outcome.stream_outcome}`}
                  className={cn(
                    "flex w-full items-center justify-between rounded-md px-2 py-1 text-left text-sm hover:bg-inset",
                    selectedKey === `stream:${outcome.stream_outcome}` && "bg-primary-soft/40",
                  )}
                  onClick={() =>
                    onSelect(
                      streamOutcomeSelection(
                        outcome.stream_outcome,
                        messages.observe.workbenchSelectionStream(getStreamOutcomeLabel(outcome.stream_outcome as StreamOutcome, messages.requestLogs) ?? messages.requestLogs.streamUnknown),
                        outcome.request_filters,
                      ),
                    )
                  }
                  data-testid={`error-stream-${outcome.stream_outcome}`}
                >
                  <span>{getStreamOutcomeLabel(outcome.stream_outcome as StreamOutcome, messages.requestLogs) ?? messages.requestLogs.streamUnknown}</span>
                  <span className="tabular-nums text-muted-foreground">
                    {outcome.count} · {outcome.percentage === null ? "—" : `${outcome.percentage.toFixed(1)}%`}
                  </span>
                </button>
                {outcome.error_kinds.map((kind) => (
                  <button
                    key={kind.stream_error_kind ?? "__null__"}
                    type="button"
                    aria-pressed={selectedKey === `kind:${outcome.stream_outcome}:${kind.stream_error_kind ?? "__null__"}`}
                    className={cn(
                      "ml-4 flex w-[calc(100%-1rem)] items-center justify-between rounded-md px-2 py-1 text-left text-xs hover:bg-inset",
                      selectedKey === `kind:${outcome.stream_outcome}:${kind.stream_error_kind ?? "__null__"}` && "bg-primary-soft/40",
                    )}
                    onClick={() =>
                      onSelect(
                        streamErrorKindSelection(
                          outcome.stream_outcome,
                          kind.stream_error_kind,
                          messages.observe.workbenchSelectionKind(describeRequestFailure({ statusCode: null, streamOutcome: outcome.stream_outcome, streamErrorKind: kind.stream_error_kind, errorPresent: true })!.title),
                          kind.request_filters,
                        ),
                      )
                    }
                    data-testid={`error-kind-${kind.stream_error_kind ?? "null"}`}
                  >
                    <span>{describeRequestFailure({ statusCode: null, streamOutcome: outcome.stream_outcome, streamErrorKind: kind.stream_error_kind, errorPresent: true })!.title}</span>
                    <span className="tabular-nums text-muted-foreground">{kind.count}</span>
                  </button>
                ))}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {data.summary.request_count === 0 ? (
        <div className="rounded-md border border-border bg-inset p-3 text-sm text-muted-foreground">{messages.observe.noErrors}</div>
      ) : null}
    </section>
  );
}

function loadingErrorsFragment(): FragmentState<UsageErrorsResponse> {
  return {
    phase: "loading",
    data: null,
    stale: false,
    error: null,
    retryAfterMs: null,
  };
}
