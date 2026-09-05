import type { useLocale } from "@/i18n/useLocale";
import type {
  RoutingDiagnosticsResponse,
  RoutingDiagnosticRoute,
} from "@/lib/api/observability";
import type { OperatorBadgeIntent } from "@/shared/design-system";

export type ObserveCopy = ReturnType<typeof useLocale>["messages"]["observe"];

export interface RouteDisposition {
  key: string;
  intent: OperatorBadgeIntent;
  label: string;
}

export function routeDisposition(
  route: RoutingDiagnosticRoute,
  copy: ObserveCopy,
): RouteDisposition {
  if (!route.accepted) {
    return { key: "not_accepted", intent: "idle", label: copy.routingNotAccepted };
  }
  if (route.statically_routable) {
    return { key: "routable", intent: "healthy", label: copy.routingRoutable };
  }
  if (route.configured_leaf_exists) {
    return {
      key: "configured_but_ineligible",
      intent: "degraded",
      label: copy.routingConfiguredButIneligible,
    };
  }
  return { key: "uncovered", intent: "failing", label: copy.routingUncovered };
}

/**
 * 整个模型配置的路由结论：取所有已接受路由里最坏的一个。页头徽章用它，
 * 避免「已启用」这一个配置布尔独自代表健康。
 */
export function summarizeRoutingDisposition(
  diagnostics: RoutingDiagnosticsResponse,
  copy: ObserveCopy,
): RouteDisposition | null {
  const accepted = (diagnostics.operation_routes ?? []).filter(
    (route) => route.accepted,
  );
  if (accepted.length === 0) return null;
  const dispositions = accepted.map((route) => routeDisposition(route, copy));
  return (
    dispositions.find((item) => item.key === "uncovered") ??
    dispositions.find((item) => item.key === "configured_but_ineligible") ??
    dispositions[0]
  );
}
