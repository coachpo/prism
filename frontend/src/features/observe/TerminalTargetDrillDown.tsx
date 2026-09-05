import { useEffect, useId, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/request";
import type { TerminalTargetStatistic, TerminalTargetStatisticsResponse } from "@/lib/api/observability";
import type { Endpoint } from "@/lib/types";
import type { ObservePreset } from "@/features/observe/observeSearch";
import {
  OperatorClippedBadge,
  OperatorMissingValue,
  OperatorValueBadge,
} from "@/shared/design-system";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { formatMoneyMicros } from "@/lib/costing";

type TerminalTargetScope = "final_execution" | "route_attempt";

type EndpointDetail = {
  phase: "loading" | "ready" | "error";
  error: string | null;
  response: TerminalTargetStatisticsResponse | null;
};

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
 *
 * Several endpoints stay open at once: comparing two exits is the reason this
 * view exists, and a single-open accordion made that comparison impossible.
 * A collapsed row keeps the summary of what it already loaded.
 */
export function TerminalTargetDrillDown({
  preset,
  scope,
  onScopeChange,
}: {
  preset: ObservePreset;
  /** 口径由 URL 承载：刷新与分享出去的链接要落在同一张表上。 */
  scope: TerminalTargetScope;
  onScopeChange: (scope: TerminalTargetScope) => void;
}) {
  const { messages } = useLocale();
  const scopeLabelId = useId();
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(
    () => new Set(),
  );
  const [details, setDetails] = useState<ReadonlyMap<number, EndpointDetail>>(
    () => new Map(),
  );
  // Every detail belongs to one attribution basis. A scope change invalidates
  // the whole set, so in-flight reads from the previous basis are discarded.
  const scopeGenerationRef = useRef(0);
  // A failed endpoint read is not "there are no endpoints". Collapsing the two
  // told the operator a fact about their deployment that was never established.
  const [endpointsFailed, setEndpointsFailed] = useState(false);
  const [endpointsReloadKey, setEndpointsReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    void api.endpoints
      .list()
      .then((items) => {
        if (!cancelled) {
          setEndpoints(items);
          setEndpointsFailed(false);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setEndpoints([]);
          setEndpointsFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [endpointsReloadKey]);

  const putDetail = (endpointId: number, detail: EndpointDetail) => {
    setDetails((current) => new Map(current).set(endpointId, detail));
  };

  const load = (endpointId: number, requestedScope: TerminalTargetScope) => {
    const generation = scopeGenerationRef.current;
    putDetail(endpointId, { phase: "loading", error: null, response: null });
    void api.stats
      .endpointTerminalTargets(endpointId, { preset, scope: requestedScope })
      .then((data) => {
        if (generation !== scopeGenerationRef.current) return;
        putDetail(endpointId, {
          phase: "ready",
          error: null,
          response: data,
        });
      })
      .catch((err: unknown) => {
        if (generation !== scopeGenerationRef.current) return;
        const retryAfter = err instanceof ApiError ? err.retryAfterMs : null;
        putDetail(endpointId, {
          phase: "error",
          error: err instanceof Error ? err.message : String(err),
          response: null,
        });
        void retryAfter;
      });
  };

  const changeScope = (nextScope: TerminalTargetScope) => {
    if (nextScope === scope) return;
    scopeGenerationRef.current += 1;
    onScopeChange(nextScope);
    setDetails(new Map());
    for (const endpointId of expanded) load(endpointId, nextScope);
  };

  const toggleEndpoint = (endpointId: number) => {
    const nextExpanded = new Set(expanded);
    if (nextExpanded.has(endpointId)) {
      nextExpanded.delete(endpointId);
      setExpanded(nextExpanded);
      return;
    }
    nextExpanded.add(endpointId);
    setExpanded(nextExpanded);
    const detail = details.get(endpointId);
    // A row already read under this basis re-opens without another read.
    if (!detail || detail.phase === "error") load(endpointId, scope);
  };

  return (
    <section
      className="flex flex-col gap-1.5"
      data-testid="terminal-target-drill-down"
    >
      {/* 口径开关与它重新定基的表并置，并带可见标签：这两枚 pill 与上方
          「趋势 / 错误 / 活动 / 终端目标」视觉同构、语义不同，靠 aria-label
          区分对看得见的人不成立。 */}
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span
          id={scopeLabelId}
          className="shrink-0 text-xs text-muted-foreground"
        >
          {messages.observe.terminalTargetScopeLabel}
        </span>
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          spacing={0}
          value={scope}
          aria-labelledby={scopeLabelId}
          onValueChange={(value) => {
            if (value) changeScope(value as TerminalTargetScope);
          }}
        >
          <ToggleGroupItem value="final_execution">
            {messages.observe.analysisScopeName("final_execution")}
          </ToggleGroupItem>
          <ToggleGroupItem value="route_attempt">
            {messages.observe.analysisScopeName("route_attempt")}
          </ToggleGroupItem>
        </ToggleGroup>
      </div>
      <p className="text-xs text-muted-foreground">
        {messages.observe.terminalTargetScopeBasis(scope)}
      </p>
      <div className="flex flex-col gap-1.5">
        {endpointsFailed ? (
          <div className="flex items-center gap-2">
            <p className="text-xs text-destructive">
              {messages.observe.ttDrillDownEndpointsFailed}
            </p>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setEndpointsFailed(false);
                setEndpoints([]);
                setEndpointsReloadKey((key) => key + 1);
              }}
            >
              <RefreshCw className="mr-1 h-3 w-3" />
              {messages.observe.retry}
            </Button>
          </div>
        ) : endpoints.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            {messages.observe.ttDrillDownNoEndpoints}
          </p>
        ) : null}
        {endpoints.map((endpoint) => {
          const isOpen = expanded.has(endpoint.id);
          const detail = details.get(endpoint.id);
          const endpointLabel = endpoint.name ?? endpoint.base_url;
          // 展开的面板要声明自己是被这个触发器控制的区域，并借端点名做名字：
          // 否则读屏用户知道「已展开」，却没法从触发器跳到被展开的内容。
          const panelId = `tt-panel-${endpoint.id}`;
          const panelLabelId = `tt-panel-label-${endpoint.id}`;
          return (
            <div key={endpoint.id} className="rounded-lg border border-border">
              <button
                type="button"
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-inset"
                onClick={() => toggleEndpoint(endpoint.id)}
                aria-expanded={isOpen}
                aria-controls={isOpen ? panelId : undefined}
                data-testid={`tt-endpoint-${endpoint.id}`}
              >
                {isOpen ? (
                  <ChevronDown className="size-4 shrink-0" />
                ) : (
                  <ChevronRight className="size-4 shrink-0" />
                )}
                <span id={panelLabelId} className="min-w-0 flex-1 truncate">
                  {endpointLabel}
                </span>
                {detail?.response ? (
                  <EndpointSummary response={detail.response} scope={scope} />
                ) : null}
                <span className="font-mono text-xs text-muted-foreground">
                  {endpoint.base_url}
                </span>
              </button>
              {isOpen ? (
                <div
                  id={panelId}
                  role="region"
                  aria-labelledby={panelLabelId}
                  className="border-t border-border px-3 py-2"
                >
                  {!detail || detail.phase === "loading" ? (
                    <p
                      className="text-xs text-muted-foreground"
                      aria-busy="true"
                    >
                      {messages.observe.ttDrillDownLoading}
                    </p>
                  ) : detail.phase === "error" ? (
                    <div
                      className="flex items-center gap-2"
                      data-testid="tt-drilldown-error"
                    >
                      <p className="text-xs text-destructive">{detail.error}</p>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => load(endpoint.id, scope)}
                      >
                        <RefreshCw className="size-3" />
                        {messages.observe.retry}
                      </Button>
                    </div>
                  ) : detail.response ? (
                    <TerminalTargetTable
                      endpointLabel={endpointLabel}
                      response={detail.response}
                      scope={scope}
                    />
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

/**
 * The collapsed row's state summary. Only rows that have actually been read
 * carry one: claiming `0 个终端目标` for a row nobody opened would report a
 * fact about the deployment that no read established.
 */
function EndpointSummary({
  response,
  scope,
}: {
  response: TerminalTargetStatisticsResponse;
  scope: TerminalTargetScope;
}) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.observe;
  const observations = response.items.reduce(
    (total, item) => total + item.request_count,
    0,
  );
  const knownCost = response.items.reduce<number | null>((total, item) => {
    if (item.known_cost_micros === null) return total;
    return (total ?? 0) + item.known_cost_micros;
  }, null);

  return (
    <span className="flex shrink-0 items-center gap-1.5 text-xs font-normal text-muted-foreground">
      <span className="tabular-nums">
        {copy.ttEndpointSummary(
          formatNumber(response.total),
          scope === "final_execution"
            ? copy.finalRequestsShort
            : copy.attemptsShort,
          formatNumber(observations),
        )}
      </span>
      {scope === "final_execution" ? (
        <span className="tabular-nums">
          {` · ${copy.cost} `}
          {knownCost === null ? (
            <OperatorMissingValue reason={copy.noTrustedCostSample} />
          ) : (
            formatMoneyMicros(
              knownCost,
              currencyState.currency.symbol,
              undefined,
              2,
              2,
              locale,
            )
          )}
        </span>
      ) : null}
      {response.total > response.items.length ? (
        <OperatorClippedBadge
          label={copy.seriesTruncatedLabel}
          reason={copy.ttDrillDownMore(response.items.length, response.total)}
        />
      ) : null}
    </span>
  );
}

function TerminalTargetTable({
  endpointLabel,
  response,
  scope,
}: {
  endpointLabel: string;
  response: TerminalTargetStatisticsResponse;
  scope: TerminalTargetScope;
}) {
  const { messages } = useLocale();
  const copy = messages.observe;
  if (response.items.length === 0) {
    // 一行文案就够：一句「没有统计」不值得 238px 的居中空态，
    // 它会让空端点和有 1885 次请求的端点占一样的视觉权重。
    return (
      <p className="text-xs text-muted-foreground" data-testid="tt-empty">
        {copy.ttDrillDownEmpty}
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-1" data-testid="tt-table">
      {response.coverage.complete === false ? (
        <OperatorClippedBadge
          className="self-start"
          label={messages.honesty.coverageIncomplete}
          reason={messages.honesty.coverageIncompleteReason}
        />
      ) : null}
      {/* 这个视图存在的理由就是横向比较；散文式的芯片行让同名字段的横坐标
          相差 200px，比 P95 或成本时只能逐行找标签再在脑中对齐。 */}
      {/* sticky 表头只对最近的滚动祖先生效：横竖两轴都交给表格自己的容器。 */}
      <Table
        aria-label={copy.ttTableLabelFor(endpointLabel)}
        scrollAreaClassName="max-h-[60vh]"
      >
        <TableHeader>
          <TableRow>
            <TableHead>{messages.routingHealth.targetColumn}</TableHead>
            <TableHead className="text-right">
              {scope === "final_execution"
                ? copy.finalRequestsShort
                : copy.attemptsShort}
            </TableHead>
            <TableHead className="text-right">
              {copy.httpSuccessShort}
            </TableHead>
            <TableHead className="text-right">
              {copy.httpFailedShort}
            </TableHead>
            <TableHead className="text-right">
              {copy.finalFailedColumn}
            </TableHead>
            <TableHead className="text-right">
              {copy.clientDisconnected}
            </TableHead>
            <TableHead className="text-right">
              {copy.latencyP95Column}
            </TableHead>
            <TableHead className="text-right">{copy.cost}</TableHead>
            <TableHead>{copy.pricingEvidenceColumn}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {response.items.map((item) => (
            <TerminalTargetRow
              key={item.connection_id}
              item={item}
              scope={scope}
            />
          ))}
        </TableBody>
      </Table>
      {response.total > response.items.length ? (
        <p className="text-xs text-muted-foreground">
          {copy.ttDrillDownMore(response.items.length, response.total)}
        </p>
      ) : null}
    </div>
  );
}

function TerminalTargetRow({
  item,
  scope,
}: {
  item: TerminalTargetStatistic;
  scope: TerminalTargetScope;
}) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.observe;
  return (
    <TableRow data-testid={`tt-row-${item.connection_id}`}>
      <TableCell>
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="min-w-0 truncate font-medium">
            {item.connection_label}
          </span>
          {/* 活动表的出口列也印这个 #id：两个视图各给半个身份时，
              操作者没有任何公共字段能把「终端目标 #25」对上「B.ai」。 */}
          <span className="shrink-0 font-mono text-xs text-muted-foreground">
            #{item.connection_id}
          </span>
          {item.ban_event_count > 0 ? (
            <OperatorValueBadge
              label={copy.banEvents(item.ban_event_count)}
              intent="degraded"
              className="text-[10px]"
            />
          ) : null}
          {item.admission_rejection_count > 0 ? (
            <OperatorValueBadge
              label={copy.admissionRejections(item.admission_rejection_count)}
              intent="degraded"
              className="text-[10px]"
            />
          ) : null}
          {/* 展开的面板里此前一个焦点停靠点都没有，下钻链在这里断掉：
              每一行给一个到该目标路由健康的链接，键盘也能继续往下走。 */}
          <Link
            to="/observe/routing-health"
            search={{
              event_terminal_target_id: String(item.connection_id),
              runtime_terminal_target_id: String(item.connection_id),
            }}
            className="inline-flex min-h-7 shrink-0 items-center rounded-md px-1.5 text-xs font-medium text-primary underline-offset-4 hover:underline"
          >
            {copy.ttOpenRoutingHealth}
          </Link>
        </div>
      </TableCell>
      {/* 零值渲染 0：整块消失的徽章把 genuinely zero 画成了 absent。 */}
      <TableCell className="text-right font-mono tabular-nums">
        {formatNumber(item.request_count)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {formatNumber(item.http_success_count)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {formatNumber(item.http_failed_count)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {formatNumber(item.final_failed_count)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {formatNumber(item.client_disconnected_count)}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        <span className="inline-flex items-center justify-end gap-1">
          {item.p95_latency_ms !== null && item.p95_latency_ms !== undefined ? (
            `${formatNumber(item.p95_latency_ms)} ms`
          ) : (
            <OperatorMissingValue
              reason={
                (item.samples?.latency_sample_count ?? 0) === 0
                  ? copy.ttNoLatencySample
                  : copy.readMissingField
              }
            />
          )}
          {(item.samples?.latency_missing_count ?? 0) > 0 ? (
            <OperatorClippedBadge
              label={copy.partialSamples}
              reason={copy.terminalTargetLatencyPartial(
                item.samples?.latency_sample_count ?? 0,
                item.samples?.latency_missing_count ?? 0,
              )}
            />
          ) : null}
        </span>
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {scope === "route_attempt" ? (
          <OperatorMissingValue reason={copy.routeAttemptCostUnavailable} />
        ) : item.known_cost_micros === null ? (
          <OperatorMissingValue reason={copy.noTrustedCostSample} />
        ) : (
          // 同屏比较用的数字必须同精度：符号已经带了币种，再传 code 会
          // 把单位写两遍（`$19.18757 USD`）。
          formatMoneyMicros(
            item.known_cost_micros,
            currencyState.currency.symbol,
            undefined,
            2,
            2,
            locale,
          )
        )}
      </TableCell>
      <TableCell>
        {scope === "final_execution" &&
        (item.samples?.cost_missing_count ?? 0) > 0 ? (
          <OperatorClippedBadge
            label={copy.partialCost}
            reason={copy.terminalTargetCostPartial(
              item.samples?.cost_sample_count ?? 0,
              item.samples?.cost_missing_count ?? 0,
            )}
          />
        ) : null}
      </TableCell>
    </TableRow>
  );
}
