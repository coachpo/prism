import { useCallback, useMemo } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { observeRoute } from "@/app/router/appRouter";
import { ObserveControlBar } from "@/features/observe/ObserveControlBar";
import { ObserveExportButton } from "@/features/observe/ObserveExportButton";
import { ObserveFreshnessBar } from "@/features/observe/ObserveFreshnessBar";
import { RoutingHealthEntryCard } from "@/features/observe/RoutingHealthEntryCard";
import { TerminalTargetDrillDown } from "@/features/observe/TerminalTargetDrillDown";
import { isObserveMetric, isObserveGroupBy, isObservePreset, type ObservePreset } from "@/features/observe/observeSearch";
import { ObserveActivityTable } from "@/features/observe/ObserveActivityTable";
import { ObserveErrorWorkbench } from "@/features/observe/ObserveErrorWorkbench";
import { ObserveMainChart } from "@/features/observe/ObserveMainChart";
import { useUsageSeriesFragment } from "@/features/observe/useObserveSeries";
import { NowStrip } from "@/features/observe/NowStrip";
import { useObserveFragments } from "@/features/observe/useObserveFragments";
import { WindowKpiGrid } from "@/features/observe/WindowKpiGrid";
import { SetupCard } from "@/features/observe/SetupCard";
import { useSetupCoordinator } from "@/features/observe/useSetupCoordinator";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout, OperatorPageHeader, OperatorSectionCard } from "@/shared/design-system";

/**
 * One view, not two. `overview` and `analytics` used to render the same KPI
 * row and the same main chart twice; the window KPIs and the chart now render
 * once and the tab strip became a content switcher over what sits below them.
 *
 * `events` is still accepted by the search schema so old deep links parse, but
 * it never renders here: the router sends it to /observe/routing-health.
 */
const OBSERVE_VIEWS = ["trend", "errors", "activity", "terminal_targets"] as const;

type ObserveView = (typeof OBSERVE_VIEWS)[number];

function resolveView(tab: string): ObserveView {
  // Both pre-redesign tabs rendered the main chart, so both legacy names land
  // on the trend view; the error panel and the drill-down, which used to be
  // stacked under `analytics`, now have switcher values of their own.
  if ((OBSERVE_VIEWS as readonly string[]).includes(tab)) return tab as ObserveView;
  return "trend";
}

export function ObservePage() {
  const { messages } = useLocale();
  const navigate = useNavigate();
  const search = useSearch({ from: observeRoute.id });

  const view = resolveView(search.tab);
  const preset = isObservePreset(search.preset) ? search.preset : "24h";
  const metric = isObserveMetric(search.metric) ? search.metric : "requests";
  const groupBy = isObserveGroupBy(search.group_by) ? search.group_by : "none";

  const setSearch = useCallback(
    (patch: Record<string, string | undefined>) => {
      void navigate({ to: "/observe", search: { ...search, ...patch } });
    },
    [navigate, search],
  );

  const setView = useCallback(
    (value: string) => {
      // The switcher rides on the existing `tab` key so deep links keep working.
      if ((OBSERVE_VIEWS as readonly string[]).includes(value)) setSearch({ tab: value });
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
  const setup = useSetupCoordinator();
  const chartState = useMemo(
    () => ({ metric, groupBy, interval: search.interval ?? "auto" }),
    [metric, groupBy, search.interval],
  );
  const seriesFragment = useUsageSeriesFragment(
    fragments.queryContext.data?.query_context ?? null,
    chartState,
    fragments.queryContext.phase,
  );

  const refreshing = fragments.now.phase === "loading" || fragments.summary.phase === "loading";

  return (
    <div data-testid="observe-page" className="flex flex-col gap-[var(--density-page-gap)]">
      <OperatorPageHeader
        title={messages.observe.pageTitle}
        description={messages.observe.pageDescription}
        actions={
          <ObserveExportButton
            preset={preset}
            metric={metric}
            groupBy={groupBy}
            interval={search.interval ?? "auto"}
            costSegmentKey={search.cost_segment_key}
            queryContextFragment={fragments.queryContext}
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
        onRefresh={fragments.refresh}
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

      <OperatorSectionCard title={messages.observe.nowLabel} description={messages.observe.nowBasis}>
        <NowStrip fragment={fragments.now} onRetry={fragments.refresh} />
      </OperatorSectionCard>

      <ObserveControlBar preset={preset} onPresetChange={setPreset} />

      {fragments.queryContext.data?.usage_coverage.complete === false ? (
        <OperatorCallout intent="warning" title={messages.observe.retentionCoverageTitle}>
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

      <Tabs value={view} onValueChange={setView} className="flex flex-col gap-3">
        <TabsList
          aria-label={messages.observe.viewSwitcherLabel}
          className="grid h-8 w-full max-w-md grid-cols-4 rounded-md border border-border bg-inset p-0.5"
        >
          <TabsTrigger value="trend">{messages.observe.viewTrend}</TabsTrigger>
          <TabsTrigger value="errors">{messages.observe.viewErrors}</TabsTrigger>
          <TabsTrigger value="activity">{messages.observe.viewActivity}</TabsTrigger>
          <TabsTrigger value="terminal_targets">{messages.observe.viewTerminalTargets}</TabsTrigger>
        </TabsList>
      </Tabs>

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
          />
        </OperatorSectionCard>
      ) : null}

      {view === "errors" ? (
        <OperatorSectionCard
          title={messages.observe.errorPanelTitle}
          description={messages.observe.errorPanelDescription}
        >
          <ObserveErrorWorkbench queryContext={fragments.queryContext.data?.query_context ?? null} />
        </OperatorSectionCard>
      ) : null}

      {view === "activity" ? (
        <OperatorSectionCard
          title={messages.observe.activityTitle}
          description={messages.observe.activityDescription}
        >
          <ObserveActivityTable queryContext={fragments.queryContext.data?.query_context ?? null} />
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
