import { useEffect, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { observe, type UsageErrorsResponse } from "@/lib/api/observability";
import { cn } from "@/lib/utils";
import { fragmentErrorFrom, type FragmentState } from "@/features/observe/useObserveFragments";
import { RetryAfterCallout } from "@/features/observe/RetryAfterCallout";
import {
  httpStatusSelection,
  streamErrorKindSelection,
  streamOutcomeSelection,
  type ObserveErrorSelection,
} from "@/features/observe/observeErrorSelection";

/**
 * Observe error analysis panel: HTTP failure ranking and stream diagnostics
 * over the finalized cohort. Every leaf carries a backend-built final_*
 * filter conjunction and deep-links into /observe/requests.
 */
export function ObserveErrorPanel({
  queryContext,
  onSelect,
  onContextResolved,
  selectedKey,
}: {
  queryContext: string | null;
  /** Selecting a leaf filters the adjacent stream instead of navigating away. */
  onSelect: (selection: ObserveErrorSelection) => void;
  onContextResolved: (requestsContext: UsageErrorsResponse["requests_context"]) => void;
  selectedKey: string | null;
}) {
  const { messages } = useLocale();
  const [fragment, setFragment] = useState<FragmentState<UsageErrorsResponse>>({
    phase: "loading",
    data: null,
    stale: false,
    error: null,
    retryAfterMs: null,
  });

  useEffect(() => {
    if (!queryContext) {
      return;
    }
    let cancelled = false;
    void observe
      .usageErrors(queryContext, { group_by: "none", limit: 10 })
      .then((data) => {
        if (cancelled) return;
        setFragment({ phase: "ready", data, stale: false, error: null, retryAfterMs: null });
        onContextResolved(data.requests_context);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const mapped = fragmentErrorFrom(err);
          setFragment((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: mapped.error, retryAfterMs: mapped.retryAfterMs }));
        }
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryContext]);

  if (!queryContext) {
    return <section role="status" className="rounded-lg border border-border bg-inset p-4 text-sm text-muted-foreground">{messages.observe.windowUnavailable}</section>;
  }
  if (fragment.phase === "loading") {
    return <section aria-busy="true" className="rounded-lg border border-border bg-inset p-4" />;
  }
  if (fragment.phase === "error" && fragment.data === null) {
    return (
      <section role="alert" className="flex flex-col gap-2 rounded-lg border border-destructive bg-destructive/5 p-3 text-sm text-destructive">
        {fragment.retryAfterMs !== null ? (
          <RetryAfterCallout retryAfterMs={fragment.retryAfterMs} />
        ) : null}
        <p>{fragment.error ?? messages.observe.windowUnavailable}</p>
      </section>
    );
  }

  if (fragment.data === null) {
    return <section role="status" className="rounded-lg border border-border bg-inset p-4 text-sm text-muted-foreground">{messages.observe.windowUnavailable}</section>;
  }

  const data = fragment.data;
  return (
    <section className="flex flex-col gap-4" data-testid="observe-error-panel">
      {fragment.stale ? (
        <div role="status" className="rounded-lg border border-degraded/40 bg-degraded/10 p-3 text-sm text-foreground">
          {messages.observe.staleDataNote}{fragment.error ? ` · ${fragment.error}` : ""}
        </div>
      ) : null}
      <div className="flex flex-wrap gap-2 text-sm">
        <span className="rounded-md bg-inset px-2 py-1 tabular-nums" data-testid="error-http-count">
          {messages.observe.httpFailedShort}: {data.summary.http_error_count}
        </span>
        <span className="rounded-md bg-inset px-2 py-1 tabular-nums" data-testid="error-stream-count">
          {messages.observe.streamFailures}: {data.summary.stream_error_count}
        </span>
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
                        messages.observe.workbenchSelectionStream(outcome.stream_outcome),
                        outcome.request_filters,
                      ),
                    )
                  }
                  data-testid={`error-stream-${outcome.stream_outcome}`}
                >
                  <span>{outcome.stream_outcome}</span>
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
                          messages.observe.workbenchSelectionKind(kind.stream_error_kind ?? "null"),
                          kind.request_filters,
                        ),
                      )
                    }
                    data-testid={`error-kind-${kind.stream_error_kind ?? "null"}`}
                  >
                    <span>{kind.stream_error_kind ?? "null"}</span>
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
