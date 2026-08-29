import { useCallback, useEffect, useMemo } from "react";
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
  const groupBy = groupBelongsToScope(parsedGroupBy, scope)
    ? parsedGroupBy
    : "none";
  const needsMetricNormalize = rawMetric !== metric;
  const needsGroupNormalize = parsedGroupBy !== groupBy;
  useEffect(() => {
    if (!needsMetricNormalize && !needsGroupNormalize) return;
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
    search,
    scope,
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
    analysisContext.phase === "loading";

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
        basis={messages.observe.windowBasis(preset)}
        nowFragment={fragments.now}
        summaryFragment={fragments.summary}
        onRefresh={() => {
          fragments.refresh();
          analysisContext.refresh();
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

      <WindowKpiGrid fragment={fragments.summary} onRetry={fragments.refresh} />

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
              const nextMetric = isValidMetricForScope(metric, nextScope)
                ? metric
                : defaultMetricForScope(nextScope);
              setSearch({
                scope: nextScope === "ingress" ? undefined : nextScope,
                metric: nextMetric,
                group_by: groupBelongsToScope(groupBy, nextScope)
                  ? groupBy
                  : "none",
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
          description={messages.observe.windowBasis(preset)}
        >
          <ObserveMainChart
            fragment={seriesFragment}
            metric={metric}
            groupBy={groupBy}
            onMetricChange={(next) => setSearch({ metric: next })}
            onGroupByChange={(next) => setSearch({ group_by: next })}
            scope={scope}
          />
          <div className="mt-2 flex justify-end">
            <button
              type="button"
              className="text-xs text-primary underline-offset-4 hover:underline"
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
            queryContext={fragments.queryContext.data?.query_context ?? null}
          />
        </OperatorSectionCard>
      ) : null}

      {view === "terminal_targets" ? (
        <OperatorSectionCard
          title={messages.observe.ttDrillDownTitle}
          description={messages.observe.terminalTargetWindowNote(preset)}
        >
          <TerminalTargetDrillDown key={preset} preset={preset} />
        </OperatorSectionCard>
      ) : null}

      <RoutingHealthEntryCard />
    </div>
  );
}

export default ObservePage;
