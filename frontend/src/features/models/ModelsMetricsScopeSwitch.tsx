import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useLocale } from "@/i18n/useLocale";

const METRICS_SCOPES = ["ingress", "final_execution", "route_attempt"] as const;

export type ModelsMetricsScope = (typeof METRICS_SCOPES)[number];

/**
 * The models-list stats scope control: a controlled single-select segmented
 * control over the three attribution scopes. The selected scope is URL-backed
 * by the caller, so a switch round-trips through the address bar.
 *
 * Radix single toggle groups emit an empty value when the active item is
 * pressed again; a controlled single-select must keep the current scope
 * instead of clearing it, so empty and unknown values are dropped here.
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

    return (
        <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            spacing={0}
            value={scope}
            aria-label={copy.metricsScopeLabel}
            onValueChange={(value) => {
                if (
                    value !== "ingress" &&
                    value !== "final_execution" &&
                    value !== "route_attempt"
                )
                    return;
                onScopeChange(value);
            }}
        >
            {METRICS_SCOPES.map((metricsScope) => (
                <ToggleGroupItem
                    key={metricsScope}
                    value={metricsScope}
                    title={copy.metricsScopeBasis(metricsScope)}
                >
                    {metricsScope === "ingress"
                        ? copy.scopeIngress
                        : metricsScope === "final_execution"
                          ? copy.scopeFinalExecution
                          : copy.scopeRouteAttempt}
                </ToggleGroupItem>
            ))}
        </ToggleGroup>
    );
}
