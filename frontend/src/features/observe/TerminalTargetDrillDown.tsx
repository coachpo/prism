import { useEffect, useState } from "react";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import type { TerminalTargetStatistic, TerminalTargetStatisticsResponse } from "@/lib/api/observability";
import type { Endpoint } from "@/lib/types";
import type { ObservePreset } from "@/features/observe/observeSearch";
import { OperatorEmptyState, OperatorMissingValue, OperatorValueBadge } from "@/shared/design-system";

/**
 * Bounded Terminal Target drill-down (OB-28..33): expanding an Endpoint row
 * lazily loads the Terminal Target detail; unexpanded rows never run
 * high-cardinality queries. A 503 here degrades only this expansion area with
 * a retry action — the rest of the dashboard stays intact. Results are Top-N +
 * pagination; the same definitions as the model/endpoint tables apply.
 *
 * The window follows the page preset rather than a hard-coded 24h, and the
 * card header says which window that is. The caller keys this component on the
 * preset so a window change drops rows loaded under the previous basis instead
 * of leaving them on screen under a new label.
 */
export function TerminalTargetDrillDown({ preset }: { preset: ObservePreset }) {
  const { messages } = useLocale();
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [selectedEndpointId, setSelectedEndpointId] = useState<number | null>(null);
  const [response, setResponse] = useState<TerminalTargetStatisticsResponse | null>(null);
  const [phase, setPhase] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void api.endpoints
      .list()
      .then((items) => {
        if (!cancelled) setEndpoints(items);
      })
      .catch(() => {
        // Endpoint list failure leaves the selector empty; the drill-down stays idle.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const load = (endpointId: number) => {
    setPhase("loading");
    setError(null);
    setResponse(null);
    void api.stats
      .endpointTerminalTargets(endpointId, { preset })
      .then((data) => {
        setResponse(data);
        setPhase("ready");
      })
      .catch((err: unknown) => {
        const retryAfter = err instanceof ApiError ? err.retryAfterMs : null;
        setError(err instanceof Error ? err.message : String(err));
        setPhase("error");
        void retryAfter;
      });
  };

  const toggleEndpoint = (endpointId: number) => {
    if (selectedEndpointId === endpointId && response !== null) {
      setSelectedEndpointId(null);
      setResponse(null);
      setPhase("idle");
      return;
    }
    setSelectedEndpointId(endpointId);
    load(endpointId);
  };

  return (
    <section className="flex flex-col gap-1.5" data-testid="terminal-target-drill-down">
      <div className="flex flex-col gap-1.5">
        {endpoints.length === 0 ? (
          <p className="text-xs text-muted-foreground">{messages.observe.ttDrillDownNoEndpoints}</p>
        ) : null}
        {endpoints.map((endpoint) => {
          const isOpen = selectedEndpointId === endpoint.id && phase !== "idle";
          return (
            <div key={endpoint.id} className="rounded-lg border border-border">
              <button
                type="button"
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-inset"
                onClick={() => toggleEndpoint(endpoint.id)}
                aria-expanded={isOpen}
                data-testid={`tt-endpoint-${endpoint.id}`}
              >
                {isOpen ? <ChevronDown className="size-4 shrink-0" /> : <ChevronRight className="size-4 shrink-0" />}
                <span className="min-w-0 flex-1 truncate">{endpoint.name ?? endpoint.base_url}</span>
                <span className="font-mono text-xs text-muted-foreground">{endpoint.base_url}</span>
              </button>
              {isOpen ? (
                <div className="border-t border-border px-3 py-2">
                  {phase === "loading" ? (
                    <p className="text-xs text-muted-foreground" aria-busy="true">{messages.observe.ttDrillDownLoading}</p>
                  ) : phase === "error" ? (
                    <div className="flex items-center gap-2" data-testid="tt-drilldown-error">
                      <p className="text-xs text-destructive">{error}</p>
                      <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => selectedEndpointId !== null && load(selectedEndpointId)}>
                        <RefreshCw className="size-3" />
                        {messages.observe.retry}
                      </Button>
                    </div>
                  ) : response ? (
                    <TerminalTargetTable response={response} />
                  ) : null}
                </div>
              ) : null}
            </div>
          );
        })}
      </div>
    </section>
  );
}

function TerminalTargetTable({ response }: { response: TerminalTargetStatisticsResponse }) {
  const { messages } = useLocale();
  if (response.items.length === 0) {
    return (
      <OperatorEmptyState
        title={messages.observe.ttDrillDownEmpty}
        className="py-4"
      />
    );
  }
  return (
    <div className="flex flex-col gap-1" data-testid="tt-table">
      {response.items.map((item) => (
        <TerminalTargetRow key={item.connection_id} item={item} />
      ))}
      {response.total > response.items.length ? (
        <p className="text-xs text-muted-foreground">
          {messages.observe.ttDrillDownMore(response.items.length, response.total)}
        </p>
      ) : null}
    </div>
  );
}

function TerminalTargetRow({ item }: { item: TerminalTargetStatistic }) {
  const { messages } = useLocale();
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md px-2 py-1.5 text-xs hover:bg-inset" data-testid={`tt-row-${item.connection_id}`}>
      <span className="min-w-0 flex-1 truncate font-medium">{item.connection_label}</span>
      <span className="tabular-nums text-muted-foreground">{messages.observe.requestsShort}: {item.request_count}</span>
      <OperatorValueBadge label={String(item.http_success_count)} intent="healthy" className="text-[10px]" />
      <OperatorValueBadge label={String(item.http_failed_count)} intent="failing" className="text-[10px]" />
      {item.final_failed_count > 0 ? (
        <OperatorValueBadge label={messages.observe.finalFailedShort(item.final_failed_count)} intent="failing" className="text-[10px]" />
      ) : null}
      {item.client_disconnected_count > 0 ? (
        <OperatorValueBadge label={messages.observe.clientDisconnectedShort(item.client_disconnected_count)} intent="degraded" className="text-[10px]" />
      ) : null}
      <span className="font-mono tabular-nums">
        {item.p50_ttft_ms !== null && item.p50_ttft_ms !== undefined ? (
          messages.observe.ttftP50(item.p50_ttft_ms)
        ) : (
          <OperatorMissingValue reason={messages.honesty.noValue} />
        )}
      </span>
      {item.ban_event_count > 0 ? (
        <OperatorValueBadge label={messages.observe.banEvents(item.ban_event_count)} intent="degraded" className="text-[10px]" />
      ) : null}
      {item.admission_rejection_count > 0 ? (
        <OperatorValueBadge label={messages.observe.admissionRejections(item.admission_rejection_count)} intent="degraded" className="text-[10px]" />
      ) : null}
    </div>
  );
}
