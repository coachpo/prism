import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { observeRoute } from "@/app/router/appRouter";
import { ObserveControlBar } from "@/features/observe/ObserveControlBar";
import { ObserveExportButton } from "@/features/observe/ObserveExportButton";
import { ObserveFreshnessBar } from "@/features/observe/ObserveFreshnessBar";
import { RoutingHealthEntryCard } from "@/features/observe/RoutingHealthEntryCard";
import { TerminalTargetDrillDown } from "@/features/observe/TerminalTargetDrillDown";
import {
  defaultMetricForScope,
  groupBelongsToScope,
  isObserveGroupBy,
  isObserveMetric,
  isObservePreset,
  isObserveScope,
  isValidMetricForScope,
  OBSERVE_SCOPES,
  type ObserveGroupBy,
  type ObserveMetric,
  type ObservePreset,
  type ObserveScope,
} from "@/features/observe/observeSearch";
import { ObserveActivityTable } from "@/features/observe/ObserveActivityTable";
import { ObserveErrorWorkbench } from "@/features/observe/ObserveErrorWorkbench";
import { ObserveMainChart } from "@/features/observe/ObserveMainChart";
import { ObserveScopedCoverageWarnings } from "@/features/observe/ObserveScopedCoverageWarnings";
import { useUsageSeriesFragment } from "@/features/observe/useObserveSeries";
import { NowStrip } from "@/features/observe/NowStrip";
import {
  useObserveAnalysisContext,
  useObserveFragments,
} from "@/features/observe/useObserveFragments";
import { WindowKpiGrid } from "@/features/observe/WindowKpiGrid";
import { SetupCard } from "@/features/observe/SetupCard";
import { useSetupCoordinator } from "@/features/observe/useSetupCoordinator";
import { useLocale } from "@/i18n/useLocale";
import {
  OperatorCallout,
  OperatorPageHeader,
  OperatorSectionCard,
} from "@/shared/design-system";

/**
 * One view, not two. `overview` and `analytics` used to render the same KPI
 * row and the same main chart twice; the window KPIs and the chart now render
 * once and the tab strip became a content switcher over what sits below them.
 *
 * `events` is still accepted by the search schema so old deep links parse, but
 * it never renders here: the router sends it to /observe/routing-health.
 */
const OBSERVE_VIEWS = [
  "trend",
  "errors",
  "activity",
  "terminal_targets",
] as const;

type ObserveView = (typeof OBSERVE_VIEWS)[number];

function resolveView(tab: string): ObserveView {
  // Both pre-redesign tabs rendered the main chart, so both legacy names land
  // on the trend view; the error panel and the drill-down, which used to be
  // stacked under `analytics`, now have switcher values of their own.
  if ((OBSERVE_VIEWS as readonly string[]).includes(tab))
    return tab as ObserveView;
  return "trend";
}

export function ObservePage() {
  const { messages } = useLocale();
  const navigate = useNavigate();
  const search = useSearch({ from: observeRoute.id });

  const view = resolveView(search.tab);
  const preset = isObservePreset(search.preset) ? search.preset : "24h";
  const rawScope = isObserveScope(search.scope) ? search.scope : "ingress";
  const scope: ObserveScope = rawScope;
  const rawMetric = search.metric;
  const metric =
    isObserveMetric(rawMetric) && isValidMetricForScope(rawMetric, scope)
      ? rawMetric
      : defaultMetricForScope(scope);
  const parsedGroupBy = isObserveGroupBy(search.group_by)
    ? search.group_by
    : "none";
  // The error view renders no grouped ranking and carries no grouping control,
  // so a `group_by` in its URL is read-only-not-writable: it re-bases a read
  // nobody can see or undo. It is dropped instead of left dangling.
  const groupBy =
    view === "errors" || !groupBelongsToScope(parsedGroupBy, scope)
      ? "none"
      : parsedGroupBy;
  const needsMetricNormalize = rawMetric !== metric;
  const needsGroupNormalize = parsedGroupBy !== groupBy;
  /**
   * A scope switch that rewrites the metric or the grouping must say so: the
   * same URL, shared or reloaded, otherwise renders a different chart under an
   * unchanged-looking control strip.
   */
  const [scopeRewrite, setScopeRewrite] = useState<{
    fromMetric: ObserveMetric | null;
    toMetric: ObserveMetric;
    fromGroup: ObserveGroupBy | null;
  } | null>(null);
  useEffect(() => {
    if (!needsMetricNormalize && !needsGroupNormalize) return;
    const fromMetric =
      isObserveMetric(rawMetric) && !isValidMetricForScope(rawMetric, scope)
        ? rawMetric
        : null;
    const fromGroup =
      view !== "errors" &&
      parsedGroupBy !== "none" &&
      !groupBelongsToScope(parsedGroupBy, scope)
        ? parsedGroupBy
        : null;
    if (fromMetric || fromGroup) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setScopeRewrite({ fromMetric, toMetric: metric, fromGroup });
    }
    void navigate({
      to: "/observe",
      search: {
        ...search,
        metric,
        group_by: groupBy === "none" ? undefined : groupBy,
      },
      replace: true,
      resetScroll: false,
    });
  }, [
    needsMetricNormalize,
    needsGroupNormalize,
    metric,
    groupBy,
    navigate,
    parsedGroupBy,
    rawMetric,
    search,
    scope,
    view,
  ]);

  const setSearch = useCallback(
    (patch: Record<string, string | undefined>) => {
      // Every control on this page — the view switcher, the metric and grouping
      // toggles, the window preset — writes to the URL. They are in-page state
      // changes, not navigations, so the router's default scroll reset would
      // yank the operator back to the top of a long page mid-inspection.
      void navigate({
        to: "/observe",
        search: { ...search, ...patch },
        resetScroll: false,
      });
    },
    [navigate, search],
  );

  const setView = useCallback(
    (value: string) => {
      // The switcher rides on the existing `tab` key so deep links keep working.
      if ((OBSERVE_VIEWS as readonly string[]).includes(value))
        setSearch({ tab: value });
    },
    [setSearch],
  );

  const setPreset = useCallback(
    (value: ObservePreset) => {
      setSearch({ preset: value });
    },
    [setSearch],
  );

  const fragments = useObserveFragments(preset);
  const analysisContext = useObserveAnalysisContext(preset, scope);
  const setup = useSetupCoordinator();
  const chartState = useMemo(
    () => ({ metric, groupBy, interval: search.interval ?? "auto", scope }),
    [metric, groupBy, scope, search.interval],
  );
  const seriesFragment = useUsageSeriesFragment(
    analysisContext.phase === "ready"
      ? (analysisContext.data?.query_context ?? null)
      : null,
    chartState,
    analysisContext.phase,
  );

  const refreshing =
    fragments.now.phase === "loading" ||
    fragments.summary.phase === "loading" ||
    analysisContext.phase === "loading" ||
    setup.loading;

  /** The bucket width the server actually applied, not the requested `auto`. */
  const effectiveInterval = seriesFragment.data?.interval ?? null;

  return (
    <div
      data-testid="observe-page"
      className="flex flex-col gap-[var(--density-page-gap)]"
    >
      <OperatorPageHeader
        title={messages.observe.pageTitle}
        description={messages.observe.pageDescription}
        actions={
          <ObserveExportButton
            preset={preset}
            metric={metric}
            scope={scope}
            groupBy={groupBy}
            interval={search.interval ?? "auto"}
            costSegmentKey={search.cost_segment_key}
            queryContextFragment={analysisContext}
            summaryFragment={fragments.summary}
            nowFragment={fragments.now}
            seriesFragment={seriesFragment}
          />
        }
      />

      <ObserveFreshnessBar
        basis={messages.observe.windowBasis(
          messages.observe.presetName(preset),
        )}
        nowFragment={fragments.now}
        summaryFragment={fragments.summary}
        onRefresh={() => {
          // Every read that feeds this page, not a third of them: the setup
          // block shares the page's observation timestamp, so leaving it on the
          // previous read would repaint stale readiness facts as fresh.
          fragments.refresh();
          analysisContext.refresh();
          setup.refresh();
        }}
        refreshing={refreshing}
      />

      <SetupCard
        state={setup.state}
        collapsed={setup.collapsed}
        cardRef={setup.cardRef}
        onBlurCapture={setup.handleBlurCapture}
        onRetry={setup.refresh}
        onToggle={setup.toggleDisclosure}
      />

      <OperatorSectionCard
        title={messages.observe.nowLabel}
        description={messages.observe.nowBasis}
      >
        <NowStrip fragment={fragments.now} onRetry={fragments.refresh} />
      </OperatorSectionCard>

      <ObserveControlBar preset={preset} onPresetChange={setPreset} />

      {fragments.queryContext.data?.usage_coverage.complete === false ? (
        <OperatorCallout
          intent="warning"
          title={messages.observe.retentionCoverageTitle}
        >
          <p>{messages.observe.retentionCoverageDescription}</p>
          <Link
            className="mt-2 inline-flex text-sm font-medium text-primary underline-offset-4 hover:underline"
            to="/system/settings?scope=instance&section=retention"
          >
            {messages.observe.retentionCoverageLink}
          </Link>
        </OperatorCallout>
      ) : null}

      {/* 视图切换与视图正文排在窗口 KPI 之前：1440×900 上，KPI 网格把
          tab 条压到 829px、图表卡头压到 937px，「这次请求为何失败」这类
          最常见的任务永远要先滚一屏，1280×800 连自己在哪个视图都看不见。
          窗口 KPI 是这一屏的小结，留在视图正文之后。 */}
      <Tabs
        value={view}
        onValueChange={setView}
        className="flex flex-col gap-3"
      >
        <TabsList
          aria-label={messages.observe.viewSwitcherLabel}
          className="grid h-8 w-full max-w-md grid-cols-4 rounded-md border border-border bg-inset p-0.5"
        >
          <TabsTrigger value="trend">{messages.observe.viewTrend}</TabsTrigger>
          <TabsTrigger value="errors">
            {messages.observe.viewErrors}
          </TabsTrigger>
          <TabsTrigger value="activity">
            {messages.observe.viewActivity}
          </TabsTrigger>
          <TabsTrigger value="terminal_targets">
            {messages.observe.viewTerminalTargets}
          </TabsTrigger>
        </TabsList>
      </Tabs>

      {view === "trend" || view === "errors" ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">
            {messages.observe.analysisScopeLabel}
          </span>
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={scope}
            aria-label={messages.observe.analysisScopeLabel}
            onValueChange={(value) => {
              if (!value || !isObserveScope(value)) return;
              const nextScope = value as ObserveScope;
              const metricKept = isValidMetricForScope(metric, nextScope);
              const nextMetric = metricKept
                ? metric
                : defaultMetricForScope(nextScope);
              const groupKept = groupBelongsToScope(groupBy, nextScope);
              setScopeRewrite(
                metricKept && groupKept
                  ? null
                  : {
                      fromMetric: metricKept ? null : metric,
                      toMetric: nextMetric,
                      fromGroup: groupKept || groupBy === "none" ? null : groupBy,
                    },
              );
              setSearch({
                scope: nextScope === "ingress" ? undefined : nextScope,
                metric: nextMetric,
                group_by: groupKept ? groupBy : "none",
              });
            }}
          >
            {OBSERVE_SCOPES.map((value) => (
              <ToggleGroupItem key={value} value={value}>
                {messages.observe.analysisScopeName(value)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          <span className="text-xs text-muted-foreground">
            {messages.observe.analysisScopeBasis(scope)}
          </span>
        </div>
      ) : null}

      {(view === "trend" || view === "errors") &&
      analysisContext.phase === "ready" &&
      analysisContext.data ? (
        <ObserveScopedCoverageWarnings
          scope={scope}
          usageCoverage={analysisContext.data.usage_coverage}
          requestCoverage={analysisContext.data.request_coverage}
        />
      ) : null}

      {view === "trend" ? (
        <OperatorSectionCard
          title={messages.observe.mainChartTitle}
          // 这张图用的就是页级时间窗，新鲜度栏已经写过一次口径：只有块级
          // 口径与页级不同才需要在块上重述。桶宽是这张图独有的基准，留着。
          description={
            effectiveInterval
              ? messages.observe.bucketBasis(
                  messages.observe.intervalName(effectiveInterval),
                )
              : undefined
          }
        >
          {scopeRewrite ? (
            <OperatorCallout
              intent="muted"
              className="mb-3"
              data-testid="observe-scope-rewrite-notice"
              title={messages.observe.scopeRewriteTitle}
            >
              {scopeRewrite.fromMetric ? (
                <p>
                  {messages.observe.scopeRewriteMetric(
                    messages.observe.metricName(scopeRewrite.fromMetric),
                    messages.observe.metricName(scopeRewrite.toMetric),
                  )}
                </p>
              ) : null}
              {scopeRewrite.fromGroup ? (
                <p>
                  {messages.observe.scopeRewriteGroup(
                    messages.observe.groupName(scopeRewrite.fromGroup),
                  )}
                </p>
              ) : null}
            </OperatorCallout>
          ) : null}
          <ObserveMainChart
            fragment={seriesFragment}
            metric={metric}
            groupBy={groupBy}
            interval={search.interval ?? "auto"}
            view={search.view === "table" ? "table" : "chart"}
            onViewChange={(next) =>
              setSearch({ view: next === "chart" ? undefined : next })
            }
            onMetricChange={(next) => {
              setScopeRewrite(null);
              setSearch({ metric: next });
            }}
            onGroupByChange={(next) => {
              setScopeRewrite(null);
              setSearch({ group_by: next });
            }}
            onIntervalChange={(next) => setSearch({ interval: next })}
            onRetry={seriesFragment.refresh}
            scope={scope}
          />
          <div className="mt-2 flex justify-end">
            {/* 趋势 → 错误是保持同口径的唯一入口：文字高 16px，命中区靠
                上下各 6px 的内边距补到 28px，视觉行高不变。 */}
            <button
              type="button"
              className="-my-1.5 inline-flex min-h-7 items-center py-1.5 text-xs text-primary underline-offset-4 hover:underline"
              onClick={() => setView("errors")}
            >
              {messages.observe.viewLinkedErrors}
            </button>
          </div>
        </OperatorSectionCard>
      ) : null}

      {view === "errors" ? (
        <OperatorSectionCard
          title={messages.observe.errorPanelTitle}
          description={messages.observe.errorPanelDescription}
        >
          <ObserveErrorWorkbench
            groupBy={groupBy}
            queryContext={
              analysisContext.phase === "ready"
                ? (analysisContext.data?.query_context ?? null)
                : null
            }
            scope={scope}
          />
        </OperatorSectionCard>
      ) : null}

      {view === "activity" ? (
        <OperatorSectionCard
          className="gap-0"
          contentClassName="px-0"
          title={messages.observe.activityTitle}
          description={messages.observe.activityIngressDescription}
        >
          <ObserveActivityTable
            preset={preset}
            onPresetChange={setPreset}
            queryContext={fragments.queryContext.data?.query_context ?? null}
          />
        </OperatorSectionCard>
      ) : null}

      {view === "terminal_targets" ? (
        <OperatorSectionCard
          title={messages.observe.ttDrillDownTitle}
          description={messages.observe.terminalTargetWindowNote(
            messages.observe.presetName(preset),
          )}
        >
          <TerminalTargetDrillDown
            key={preset}
            preset={preset}
            scope={search.tt_scope === "route_attempt" ? "route_attempt" : "final_execution"}
            onScopeChange={(next) =>
              setSearch({
                tt_scope: next === "final_execution" ? undefined : next,
              })
            }
          />
        </OperatorSectionCard>
      ) : null}

      <WindowKpiGrid fragment={fragments.summary} onRetry={fragments.refresh} />

      <RoutingHealthEntryCard />
    </div>
  );
}

export default ObservePage;
