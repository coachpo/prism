import { useMemo } from "react";
import { useRouterState } from "@tanstack/react-router";
import type { LucideIcon } from "lucide-react";
import {
  Activity,
  Coins,
  FileText,
  KeyRound,
  LayoutDashboard,
  Plug,
  Scale,
  Server,
  Settings,
} from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { Messages } from "@/i18n/messages";
import { useBreadcrumbEntity } from "./breadcrumbEntity";

export type NavLabelKey = keyof Messages["nav"];

/**
 * Three groups, chosen so that path prefix, sidebar group, and the first
 * breadcrumb segment are the same thing:
 *   observability -> /observe/*, routing -> /route/*, system -> /system/*
 */
export type ShellSidebarGroupId = "observability" | "routing" | "system";

export type ShellSidebarItemId =
  | "dashboard"
  | "request-logs"
  | "routing-health"
  | "endpoints"
  | "pricing-templates"
  | "loadbalance-strategies"
  | "models"
  | "settings"
  | "proxy-api-keys";

export type ShellRouteId =
  | ShellSidebarItemId
  | "model-export"
  | "model-detail"
  | "request-log-audit";

export interface ShellSidebarItemDefinition {
  groupId: ShellSidebarGroupId;
  icon: LucideIcon;
  id: ShellSidebarItemId;
  labelKey: NavLabelKey;
  to: string;
}

export interface ShellRouteMetadata {
  canonicalPath: string;
  /** Label shown as the breadcrumb page segment when the route is not a sidebar item. */
  breadcrumbLabelKey?: NavLabelKey;
  groupId: ShellSidebarGroupId;
  id: ShellRouteId;
  pathPattern: string;
  sidebarItemId: ShellSidebarItemId;
  sidebarItem?: ShellSidebarItemDefinition;
}

export const SHELL_ROUTE_METADATA: readonly ShellRouteMetadata[] = [
  {
    canonicalPath: "/observe",
    groupId: "observability",
    id: "dashboard",
    pathPattern: "/observe",
    sidebarItem: { groupId: "observability", icon: LayoutDashboard, id: "dashboard", labelKey: "dashboard", to: "/observe" },
    sidebarItemId: "dashboard",
  },
  {
    canonicalPath: "/observe/requests",
    groupId: "observability",
    id: "request-logs",
    pathPattern: "/observe/requests",
    sidebarItem: { groupId: "observability", icon: FileText, id: "request-logs", labelKey: "requestLogs", to: "/observe/requests" },
    sidebarItemId: "request-logs",
  },
  // Audit is its own page, but triage always arrives from the request list, so
  // the list stays highlighted in the sidebar.
  {
    breadcrumbLabelKey: "requestAudit",
    canonicalPath: "/observe/requests/:requestId/audit",
    groupId: "observability",
    id: "request-log-audit",
    pathPattern: "/observe/requests/:requestId/audit",
    sidebarItemId: "request-logs",
  },
  {
    canonicalPath: "/observe/routing-health",
    groupId: "observability",
    id: "routing-health",
    pathPattern: "/observe/routing-health",
    sidebarItem: { groupId: "observability", icon: Activity, id: "routing-health", labelKey: "routingHealth", to: "/observe/routing-health" },
    sidebarItemId: "routing-health",
  },
  // Routing follows the real setup dependency order: endpoint -> pricing ->
  // strategy -> model. Configuring top to bottom is itself the guided path.
  {
    canonicalPath: "/route/endpoints",
    groupId: "routing",
    id: "endpoints",
    pathPattern: "/route/endpoints",
    sidebarItem: { groupId: "routing", icon: Plug, id: "endpoints", labelKey: "endpoints", to: "/route/endpoints" },
    sidebarItemId: "endpoints",
  },
  {
    canonicalPath: "/route/pricing",
    groupId: "routing",
    id: "pricing-templates",
    pathPattern: "/route/pricing",
    sidebarItem: { groupId: "routing", icon: Coins, id: "pricing-templates", labelKey: "pricingTemplates", to: "/route/pricing" },
    sidebarItemId: "pricing-templates",
  },
  {
    canonicalPath: "/route/ban-policies",
    groupId: "routing",
    id: "loadbalance-strategies",
    pathPattern: "/route/ban-policies",
    sidebarItem: { groupId: "routing", icon: Scale, id: "loadbalance-strategies", labelKey: "loadbalanceStrategies", to: "/route/ban-policies" },
    sidebarItemId: "loadbalance-strategies",
  },
  {
    canonicalPath: "/route/models",
    groupId: "routing",
    id: "models",
    pathPattern: "/route/models",
    sidebarItem: { groupId: "routing", icon: Server, id: "models", labelKey: "models", to: "/route/models" },
    sidebarItemId: "models",
  },
  {
    breadcrumbLabelKey: "modelExport",
    canonicalPath: "/route/models/export",
    groupId: "routing",
    id: "model-export",
    pathPattern: "/route/models/export",
    sidebarItemId: "models",
  },
  {
    canonicalPath: "/route/models/:modelId",
    groupId: "routing",
    id: "model-detail",
    pathPattern: "/route/models/:modelId",
    sidebarItemId: "models",
  },
  {
    canonicalPath: "/system/settings",
    groupId: "system",
    id: "settings",
    pathPattern: "/system/settings",
    // 一个视图一个规范地址：侧栏原来落到带 section 的那一个、面包屑回链落到裸路径，
    // 同一份内容有了两个合法 URL、两种面包屑形态。分区选择交给页内的分区导航。
    sidebarItem: { groupId: "system", icon: Settings, id: "settings", labelKey: "settings", to: "/system/settings" },
    sidebarItemId: "settings",
  },
  {
    canonicalPath: "/system/proxy-keys",
    groupId: "system",
    id: "proxy-api-keys",
    pathPattern: "/system/proxy-keys",
    sidebarItem: { groupId: "system", icon: KeyRound, id: "proxy-api-keys", labelKey: "apiKeys", to: "/system/proxy-keys" },
    sidebarItemId: "proxy-api-keys",
  },
];

export const SHELL_SIDEBAR_GROUP_ORDER: readonly ShellSidebarGroupId[] = [
  "observability",
  "routing",
  "system",
];

export const SHELL_SIDEBAR_ITEMS: readonly ShellSidebarItemDefinition[] = SHELL_ROUTE_METADATA.flatMap(
  (route) => (route.sidebarItem ? [route.sidebarItem] : [])
);

type ShellBreadcrumbLeafId =
  | "entity"
  | "settings-audit-configuration"
  | "settings-authentication"
  | "settings-billing-currency"
  | "settings-retention-deletion"
  | "settings-timezone";

export interface ShellBreadcrumbItem {
  current: boolean;
  href: string | null;
  /** 回程的查询参数。路由不解析写进 href 的 query 串，必须单独带上。 */
  search?: Record<string, string>;
  id: ShellRouteId | ShellSidebarGroupId | ShellBreadcrumbLeafId;
  label: string;
}

export interface LocalizedShellSidebarItem extends ShellSidebarItemDefinition {
  current: boolean;
  label: string;
}

export interface LocalizedShellSidebarGroup {
  id: ShellSidebarGroupId;
  items: LocalizedShellSidebarItem[];
  label: string;
}

export interface ShellNavigationState {
  activeSidebarItem: LocalizedShellSidebarItem | null;
  breadcrumbs: ShellBreadcrumbItem[];
  sidebarGroups: LocalizedShellSidebarGroup[];
  sidebarItems: LocalizedShellSidebarItem[];
}

interface MatchedShellRoute {
  params: Record<string, string>;
  route: ShellRouteMetadata;
}

const SETTINGS_SECTION_BREADCRUMBS: Record<
  string,
  { id: ShellBreadcrumbLeafId; label: (messages: Messages) => string }
> = {
  authentication: {
    id: "settings-authentication",
    label: (messages) => messages.settingsAuthentication.authentication,
  },
  "billing-currency": {
    id: "settings-billing-currency",
    label: (messages) => messages.settingsPage.billingCurrency,
  },
  "audit-privacy": {
    id: "settings-audit-configuration",
    label: (messages) => messages.settingsPage.auditPrivacy,
  },
  "audit-configuration": {
    id: "settings-audit-configuration",
    label: (messages) => messages.settingsPage.auditPrivacy,
  },
  retention: {
    id: "settings-retention-deletion",
    label: (messages) => messages.settingsPage.retentionDeletion,
  },
  "retention-deletion": {
    id: "settings-retention-deletion",
    label: (messages) => messages.settingsPage.retentionDeletion,
  },
  timezone: {
    id: "settings-timezone",
    label: (messages) => messages.settingsPage.timezone,
  },
};

function normalizeParams(
  params: Record<string, string | undefined>
): Record<string, string> {
  return Object.fromEntries(
    Object.entries(params).map(([key, value]) => [key, value ?? ""])
  );
}

function matchShellPath(pathPattern: string, pathname: string): Record<string, string> | null {
  const patternParts = pathPattern.split("/").filter(Boolean);
  const pathParts = pathname.split("/").filter(Boolean);
  if (patternParts.length !== pathParts.length) return null;

  const params: Record<string, string> = {};
  for (let index = 0; index < patternParts.length; index += 1) {
    const patternPart = patternParts[index];
    const pathPart = pathParts[index] ?? "";
    if (patternPart.startsWith(":")) {
      params[patternPart.slice(1)] = decodeURIComponent(pathPart);
      continue;
    }
    if (patternPart !== pathPart) return null;
  }
  return params;
}

function matchShellRoute(pathname: string): MatchedShellRoute {
  for (const route of SHELL_ROUTE_METADATA) {
    const params = matchShellPath(route.pathPattern, pathname);
    if (params) {
      return { params: normalizeParams(params), route };
    }
  }

  return { params: {}, route: SHELL_ROUTE_METADATA[0] };
}

function getPageLabel(messages: Messages, route: ShellRouteMetadata): string {
  if (route.breadcrumbLabelKey) return messages.nav[route.breadcrumbLabelKey];
  const sidebarItem =
    route.sidebarItem ?? SHELL_SIDEBAR_ITEMS.find((item) => item.id === route.sidebarItemId);
  return sidebarItem ? messages.nav[sidebarItem.labelKey] : messages.nav.dashboard;
}

function getPageHref(route: ShellRouteMetadata): string | null {
  if (route.sidebarItem) return null;
  const sidebarItem = SHELL_SIDEBAR_ITEMS.find((item) => item.id === route.sidebarItemId);
  return sidebarItem?.to ?? null;
}

/**
 * Breadcrumbs are fixed at group -> page -> entity. The group segment is a
 * label, not a link: a group is a section of the sidebar, not a page. The
 * entity segment appears only when the page is scoped to one, and detail pages
 * publish their real entity name through `useBreadcrumbEntity`.
 */
function buildBreadcrumbs(
  matchedRoute: MatchedShellRoute,
  messages: Messages,
  entity: string | null,
  search: string
): ShellBreadcrumbItem[] {
  const { route } = matchedRoute;
  const crumbs: ShellBreadcrumbItem[] = [
    {
      current: false,
      href: null,
      id: route.groupId,
      label: messages.shell.groupLabels[route.groupId],
    },
  ];

  const entityLabel = resolveEntityLabel(matchedRoute, messages, entity, search);
  const pageHref = entityLabel ? (getPageHref(route) ?? route.canonicalPath) : null;

  crumbs.push({
    current: !entityLabel,
    href: entityLabel ? pageHref : null,
    // 审计页不知道操作者原本的时间窗，但默认的 24 小时几乎肯定不含这条请求。
    // 回到保留期内的全部时间，至少保证落点不是一个空列表。
    search:
      entityLabel && route.id === "request-log-audit"
        ? { time_range: "all" }
        : undefined,
    id: route.id,
    label: getPageLabel(messages, route),
  });

  if (entityLabel) {
    crumbs.push({ current: true, href: null, id: entityLabel.id, label: entityLabel.label });
  }

  return crumbs;
}

function resolveEntityLabel(
  matchedRoute: MatchedShellRoute,
  messages: Messages,
  entity: string | null,
  search: string
): { id: ShellBreadcrumbLeafId; label: string } | null {
  switch (matchedRoute.route.id) {
    case "model-detail":
      // Falls back to the id until the page publishes the display name, so the
      // leaf is never a generic word like "配置".
      return { id: "entity", label: entity ?? `#${matchedRoute.params.modelId}` };

    case "request-log-audit":
      return { id: "entity", label: `#${matchedRoute.params.requestId}` };

    case "request-logs": {
      const requestId = new URLSearchParams(search).get("request_id")?.trim() ?? "";
      return requestId ? { id: "entity", label: `#${requestId}` } : null;
    }

    case "settings": {
      const section = new URLSearchParams(search).get("section")?.trim() ?? "";
      const settingsLeaf = SETTINGS_SECTION_BREADCRUMBS[section];
      return settingsLeaf ? { id: settingsLeaf.id, label: settingsLeaf.label(messages) } : null;
    }

    default:
      return entity ? { id: "entity", label: entity } : null;
  }
}

export function useShellNavigation(): ShellNavigationState {
  const location = useRouterState({ select: (state) => state.location });
  const { messages } = useLocale();
  const entity = useBreadcrumbEntity();

  return useMemo(() => {
    const matchedRoute = matchShellRoute(location.pathname);
    const sidebarItems = SHELL_SIDEBAR_ITEMS.map((item) => ({
      ...item,
      current: item.id === matchedRoute.route.sidebarItemId,
      label: messages.nav[item.labelKey],
    }));
    const activeSidebarItem =
      sidebarItems.find((item) => item.id === matchedRoute.route.sidebarItemId) ?? null;
    const sidebarGroups = SHELL_SIDEBAR_GROUP_ORDER.map((groupId) => ({
      id: groupId,
      items: sidebarItems.filter((item) => item.groupId === groupId),
      label: messages.shell.groupLabels[groupId],
    })).filter((group) => group.items.length > 0);

    return {
      activeSidebarItem,
      breadcrumbs: buildBreadcrumbs(matchedRoute, messages, entity, location.searchStr),
      sidebarGroups,
      sidebarItems,
    };
  }, [entity, location.pathname, location.searchStr, messages]);
}
