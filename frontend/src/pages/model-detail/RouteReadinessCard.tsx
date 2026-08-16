import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfig } from "@/lib/types";
import { formatApiFamily } from "@/lib/utils";
import { getLoadbalanceStrategyDetailLabel } from "@/lib/loadbalanceRoutingPolicy";
import {
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorErrorState,
  OperatorRetryButton,
} from "@/shared/design-system";
import { OperationRoutingSummary } from "@/features/models/detail/OperationRoutingSummary";
import type { DiagnosticsView } from "@/features/models/detail/ModelDetailFeaturePage";
import type { AccessTargetSummary } from "./useModelDetailDataSupport";

interface RouteReadinessCardProps {
  accessTargetSummary?: AccessTargetSummary;
  diagnosticsView: DiagnosticsView;
  onRetryDiagnostics: () => void;
  model: ModelConfig;
}

/**
 * One card for "can this model route".
 *
 * The same facts used to be spread over three surfaces: a configuration card,
 * a separate operation-routing panel, and a single-strategy warning inside the
 * targets editor — each with its own count vocabulary. Counts here read
 * `N 启用 / M 总计` everywhere, matching the models list.
 */
export function RouteReadinessCard({ accessTargetSummary, diagnosticsView, onRetryDiagnostics, model }: RouteReadinessCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelDetail;
  const modelsUiCopy = messages.modelsUi;
  const apiFamily = model.api_family ?? "openai";
  const strategyDetail = model.loadbalance_strategy
    ? getLoadbalanceStrategyDetailLabel(model.loadbalance_strategy, messages.loadbalanceStrategyCopy)
    : null;

  return (
    <OperatorSectionCard
      title={copy.routeReadinessTitle}
      description={copy.routeReadinessDescription}
      contentClassName="flex flex-col gap-3"
      data-testid="route-readiness-card"
    >
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <ReadinessFact label={messages.common.apiFamily}>
          <span className="flex items-center gap-1.5">
            <ApiFamilyIcon apiFamily={apiFamily} size={14} />
            <span className="text-sm font-medium">{formatApiFamily(apiFamily)}</span>
          </span>
        </ReadinessFact>

        <ReadinessFact label={copy.strategyLabel}>
          {model.loadbalance_strategy ? (
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-medium">{model.loadbalance_strategy.name}</span>
              {strategyDetail ? (
                <span className="truncate text-xs text-muted-foreground">{strategyDetail}</span>
              ) : null}
            </span>
          ) : (
            <OperatorMissingValue reason={copy.strategyUnassignedReason} />
          )}
        </ReadinessFact>

        <ReadinessFact label={modelsUiCopy.terminalTargets}>
          {accessTargetSummary ? (
            <span className="font-mono text-sm tabular-nums">
              {copy.targetsCount(
                formatNumber(accessTargetSummary.enabledTerminalTargetCount),
                formatNumber(accessTargetSummary.totalTerminalTargetCount),
              )}
            </span>
          ) : (
            <OperatorMissingValue />
          )}
        </ReadinessFact>

        <ReadinessFact label={modelsUiCopy.modelFallbackTargets}>
          {accessTargetSummary ? (
            <span className="font-mono text-sm tabular-nums">
              {copy.targetsCount(
                formatNumber(accessTargetSummary.enabledModelFallbackTargetCount),
                formatNumber(accessTargetSummary.totalModelTargetCount),
              )}
            </span>
          ) : (
            <OperatorMissingValue />
          )}
        </ReadinessFact>
      </div>

      {/*
        All four states render something. Returning null for a read failure
        made a broken fetch look identical to a model with nothing to report.
      */}
      {diagnosticsView.kind === "loaded" ? <OperationRoutingSummary diagnostics={diagnosticsView.value} /> : null}
      {diagnosticsView.kind === "loading" || diagnosticsView.kind === "idle" ? (
        <OperatorInsetPanel className="gap-1 p-2.5" data-testid="route-readiness-diagnostics-loading">
          <Skeleton className="h-4 w-40" />
        </OperatorInsetPanel>
      ) : null}
      {diagnosticsView.kind === "error" ? (
        <OperatorErrorState
          testId="route-readiness-diagnostics-error"
          title={copy.diagnosticsErrorTitle}
          description={copy.diagnosticsErrorDescription}
          details={diagnosticsView.message}
          detailsLabel={copy.diagnosticsErrorDetailsLabel}
          action={<OperatorRetryButton onClick={onRetryDiagnostics}>{copy.diagnosticsRetry}</OperatorRetryButton>}
        />
      ) : null}
    </OperatorSectionCard>
  );
}

function ReadinessFact({ children, label }: { children: React.ReactNode; label: string }) {
  return (
    <OperatorInsetPanel className="gap-1 p-2.5">
      <p className="text-[11px] font-medium tracking-[0.04em] text-muted-foreground">{label}</p>
      {children}
    </OperatorInsetPanel>
  );
}
