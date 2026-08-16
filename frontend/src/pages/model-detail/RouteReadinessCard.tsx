import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { useLocale } from "@/i18n/useLocale";
import type { RoutingDiagnosticsResponse } from "@/lib/api/observability";
import type { ModelConfig } from "@/lib/types";
import { formatApiFamily } from "@/lib/utils";
import { getLoadbalanceStrategyDetailLabel } from "@/lib/loadbalanceRoutingPolicy";
import {
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorSectionCard,
} from "@/shared/design-system";
import { OperationRoutingSummary } from "@/features/models/detail/OperationRoutingSummary";
import type { AccessTargetSummary } from "./useModelDetailDataSupport";

interface RouteReadinessCardProps {
  accessTargetSummary?: AccessTargetSummary;
  diagnostics: RoutingDiagnosticsResponse | null;
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
export function RouteReadinessCard({ accessTargetSummary, diagnostics, model }: RouteReadinessCardProps) {
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

      {diagnostics ? <OperationRoutingSummary diagnostics={diagnostics} /> : null}
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
