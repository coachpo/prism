import { useLocale } from "@/i18n/useLocale";
import { MetricsScopeSwitch } from "./MetricsScopeSwitch";

const METRICS_SCOPES = ["ingress", "final_execution", "route_attempt"] as const;

export type ModelsMetricsScope = (typeof METRICS_SCOPES)[number];

/**
 * The models-list stats scope control. The selected scope is URL-backed by the
 * caller, so a switch round-trips through the address bar; re-selecting the
 * active scope keeps it rather than clearing the URL.
 */
export function ModelsMetricsScopeSwitch({
  onScopeChange,
  scope,
}: {
  onScopeChange: (scope: ModelsMetricsScope) => void;
  scope: ModelsMetricsScope;
}) {
  const { messages } = useLocale();
  const copy = messages.modelsPage;
  const scopeLabels: Record<ModelsMetricsScope, string> = {
    ingress: copy.scopeIngress,
    final_execution: copy.scopeFinalExecution,
    route_attempt: copy.scopeRouteAttempt,
  };

  return (
    <MetricsScopeSwitch
      label={copy.metricsScopeLabel}
      value={scope}
      onChange={onScopeChange}
      options={METRICS_SCOPES.map((metricsScope) => ({
        value: metricsScope,
        label: scopeLabels[metricsScope],
        basis: copy.metricsScopeBasis(metricsScope),
      }))}
    />
  );
}
