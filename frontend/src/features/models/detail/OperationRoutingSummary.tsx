import { useLocale } from "@/i18n/useLocale";
import type { RoutingDiagnosticsResponse, RoutingDiagnosticRoute } from "@/lib/api/observability";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorStatusBadge,
  OperatorTypeBadge,
  type OperatorBadgeIntent,
} from "@/shared/design-system";

/**
 * Static routing summary (MC-A2/A3/A4): authoritative backend analyzer output
 * for the accepted OpenAI operations plus configuration warnings. The backend
 * is the only source of truth; the frontend never re-derives dispositions.
 */
export function OperationRoutingSummary({ diagnostics }: { diagnostics: RoutingDiagnosticsResponse | null }) {
  const { messages } = useLocale();
  const copy = messages.observe;
  if (!diagnostics) {
    return null;
  }
  const warnings = diagnostics.configuration_warnings ?? [];
  const routes = diagnostics.operation_routes ?? [];
  const singleTruncated = warnings.find((warning) => warning.code === "single_strategy_truncates_targets");

  return (
    <OperatorInsetPanel data-testid="operation-routing-summary">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="font-medium">{copy.routingSummaryTitle}</span>
          {diagnostics.openai_accepted_format ? (
            <OperatorTypeBadge intent="accent" label={modeLabel(diagnostics.openai_accepted_format, copy)} preserveLabel />
          ) : null}
          {diagnostics.strategy ? (
            <span className="text-xs text-muted-foreground">
              {copy.routingStrategyLabel}: {strategyLabel(diagnostics.strategy.type, copy)}
            </span>
          ) : null}
        </div>
        <ul className="flex flex-col gap-1" data-testid="routing-operation-list">
          {routes.map((route) => (
            <li key={route.operation_name} data-testid={`routing-operation-${route.operation_name}`}>
              <OperationRouteRow route={route} copy={copy} />
            </li>
          ))}
        </ul>
        {singleTruncated ? (
          <OperatorCallout intent="warning" data-testid="single-truncation-callout">
            {singleTruncated.message}
          </OperatorCallout>
        ) : null}
        {warnings
          .filter((warning) => warning && warning.code !== "single_strategy_truncates_targets")
          .map((warning) => (
            <OperatorCallout key={warning.code} intent={warning.severity === "danger" ? "danger" : "warning"}>
              {warning.message}
            </OperatorCallout>
          ))}
      </div>
    </OperatorInsetPanel>
  );
}

function OperationRouteRow({
  route,
  copy,
}: {
  route: RoutingDiagnosticRoute;
  copy: ReturnType<typeof useLocale>["messages"]["observe"];
}) {
  let intent: OperatorBadgeIntent = "idle";
  let label: string;
  if (!route.accepted) {
    intent = "idle";
    label = copy.routingNotAccepted;
  } else if (route.statically_routable) {
    intent = "healthy";
    label = copy.routingRoutable;
  } else if (route.configured_leaf_exists) {
    intent = "degraded";
    label = copy.routingConfiguredButIneligible;
  } else {
    intent = "failing";
    label = copy.routingUncovered;
  }
  const displayName = groupName(route.operation_name, copy);
  return (
    <div className="flex items-center justify-between gap-2 rounded-md px-1 py-0.5 text-sm">
      <span className="font-mono text-xs">{displayName}</span>
      <OperatorStatusBadge intent={intent} label={label} />
    </div>
  );
}

function groupName(operationName: string, copy: ReturnType<typeof useLocale>["messages"]["observe"]): string {
  if (operationName.startsWith("openai.responses")) return copy.routingResponsesLabel;
  if (operationName === "openai.chat_completions") return copy.routingChatLabel;
  // Image operations are their own groups, one per operation, because
  // generations and edits are authorized independently.
  if (operationName === "openai.images.generations") return copy.imagesGenerations ?? operationName;
  if (operationName === "openai.images.edits") return copy.imagesEdits ?? operationName;
  return operationName;
}

function modeLabel(mode: string, copy: ReturnType<typeof useLocale>["messages"]["observe"]): string {
  if (mode === "dual_native") return copy.modeDual ?? "双模式";
  if (mode === "chat_completions_only") return copy.modeChat ?? "仅 Chat Completions";
  if (mode === "responses_only") return copy.modeResponses ?? "仅 Responses";
  return mode;
}

function strategyLabel(type: string, copy: ReturnType<typeof useLocale>["messages"]["observe"]): string {
  if (type === "single") return copy.strategySingle ?? "单一";
  if (type === "fill-first") return copy.strategyFillFirst ?? "优先填满";
  if (type === "round-robin") return copy.strategyRoundRobin ?? "轮询";
  return type;
}
