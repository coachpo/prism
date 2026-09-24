import { modelStrategyLabel } from "../models/modelStrategyLabel";
import { Link } from "@tanstack/react-router";

import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfig } from "@/lib/types";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorErrorState,
  OperatorRetryButton,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { useTimezone } from "@/hooks/useTimezone";
import { OperationRoutingSummary } from "@/features/models/detail/OperationRoutingSummary";
import type { DiagnosticsView } from "@/features/models/detail/ModelDetailFeaturePage";
import { buildUpstreamIdentitySummary } from "./modelAccessTargetProjection";

interface RouteReadinessCardProps {
  diagnosticsView: DiagnosticsView;
  onRetryDiagnostics: () => void;
  model: ModelConfig;
}

/**
 * One card for "can this model route, and where does it exit".
 *
 * The summary aggregates only DIRECT facts from this model config's mixed
 * access-target list: Terminal Target rows carry the actual upstream identity,
 * Model Target rows are logical edges that never contribute identities, and
 * nothing here follows them recursively or repeats the full mapping — the
 * ordered target list below owns that, including the enabled/total counts.
 * Missing identities are unknown evidence, never backfilled from the entry id.
 */
export function RouteReadinessCard({
  diagnosticsView,
  onRetryDiagnostics,
  model,
}: RouteReadinessCardProps) {
  const { formatNumber, messages } = useLocale();
  const { format: formatDateTime } = useTimezone();
  const copy = messages.modelDetail;
  const apiFamily = model.api_family ?? "openai";
  const upstreamIdentity = buildUpstreamIdentitySummary(model);

  return (
    <OperatorSectionCard
      title={copy.routeReadinessTitle}
      description={copy.routeReadinessDescription}
      contentClassName="flex flex-col gap-3"
      data-testid="route-readiness-card"
    >
      {/*
        结论先于依据：能不能路由是这张卡要回答的问题，八个瓦片是它的依据。
        四种诊断状态都渲染点什么 —— 读取失败与「没什么可报」必须能区分。
      */}
      {diagnosticsView.kind === "loaded" ? (
        <OperationRoutingSummary diagnostics={diagnosticsView.value} />
      ) : null}
      {diagnosticsView.kind === "loading" || diagnosticsView.kind === "idle" ? (
        <OperatorInsetPanel
          className="gap-1 p-2.5"
          data-testid="route-readiness-diagnostics-loading"
        >
          <Skeleton className="h-4 w-40" />
        </OperatorInsetPanel>
      ) : null}
      {diagnosticsView.kind === "error" ? (
        <OperatorErrorState
          testId="route-readiness-diagnostics-error"
          title={copy.diagnosticsErrorTitle}
          description={copy.diagnosticsErrorDescription}
          action={
            <OperatorRetryButton onClick={onRetryDiagnostics}>
              {copy.diagnosticsRetry}
            </OperatorRetryButton>
          }
        />
      ) : null}

      {/* 服务选择方式只在这里写一次；各类目标的启用数由下方目标列表给出。 */}
      <div className="grid gap-3 sm:grid-cols-2">
        <ReadinessFact label={messages.modelsUi.apiFamilyLabel}>
          <span className="flex items-center gap-1.5">
            <ApiFamilyIcon apiFamily={apiFamily} size={14} />
            <span className="text-sm font-medium">
              {formatApiFamily(apiFamily)}
            </span>
          </span>
        </ReadinessFact>

        <ReadinessFact label={copy.strategyLabel}>
          {model.loadbalance_strategy ? (
            // 配置链上下相邻的两页之间要能一步走到：这里原来只是一段死文本，
            // 要改这条策略得自己回到侧栏再找一次。
            <Link
              to="/route/ban-policies"
              className="truncate text-sm font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            >
              {modelStrategyLabel(model.loadbalance_strategy)}
            </Link>
          ) : (
            <OperatorMissingValue reason={copy.strategyUnassignedReason} />
          )}
        </ReadinessFact>
      </div>

      {upstreamIdentity.unknownUpstreamModelIdCount > 0 ? (
        <OperatorCallout intent="warning">
          {copy.upstreamUnknownCount(
            formatNumber(upstreamIdentity.unknownUpstreamModelIdCount),
          )}
        </OperatorCallout>
      ) : null}

      {!upstreamIdentity.hasDirectTerminalTargets ? (
        <OperatorStatusBadge
          intent="idle"
          preserveLabel
          label={copy.noDirectTerminalTargets}
          title={copy.noDirectTerminalTargetsReason}
        />
      ) : null}

      <p className="text-xs text-muted-foreground">
        {copy.configurationUpdatedAt(formatDateTime(model.updated_at))}
      </p>
    </OperatorSectionCard>
  );
}

function ReadinessFact({
  children,
  label,
}: {
  children: React.ReactNode;
  label: string;
}) {
  return (
    <OperatorInsetPanel className="gap-1 p-2.5">
      <p className="text-[11px] font-medium tracking-[0.04em] text-muted-foreground">
        {label}
      </p>
      {children}
    </OperatorInsetPanel>
  );
}
