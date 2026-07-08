import { useMemo } from "react";
import { matchPath, useLocation } from "react-router-dom";
import type { LucideIcon } from "lucide-react";
import {
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
import type { Messages } from "@/i18n/messages/en";
import { APP_VERSION, formatVersionLabel } from "@/lib/appVersion";

export type NavLabelKey = keyof Messages["nav"];

export type ShellSidebarGroupId = "overview" | "configuration" | "observability" | "access";

export type ShellSidebarItemId =
  | "dashboard"
  | "models"
  | "endpoints"
  | "loadbalance-strategies"
  | "settings"
  | "proxy-api-keys"
  | "pricing-templates"
  | "request-logs";

export type ShellRouteId = ShellSidebarItemId | "model-detail" | "request-log-audit";

export interface ShellSidebarItemDefinition {
  groupId: ShellSidebarGroupId;
  icon: LucideIcon;
  id: ShellSidebarItemId;
  labelKey: NavLabelKey;
  to: string;
}

export interface ShellRouteMetadata {
  canonicalPath: string;
  id: ShellRouteId;
  pathPattern: string;
  profileScoped: boolean;
  sidebarItemId: ShellSidebarItemId;
  sidebarItem?: ShellSidebarItemDefinition;
}

export const SHELL_ROUTE_METADATA: readonly ShellRouteMetadata[] = [
  { canonicalPath: "/observe", id: "dashboard", pathPattern: "/observe", profileScoped: false, sidebarItem: { groupId: "overview", icon: LayoutDashboard, id: "dashboard", labelKey: "dashboard", to: "/observe" }, sidebarItemId: "dashboard" },
  { canonicalPath: "/models", id: "models", pathPattern: "/models", profileScoped: true, sidebarItem: { groupId: "configuration", icon: Server, id: "models", labelKey: "models", to: "/models" }, sidebarItemId: "models" },
  { canonicalPath: "/models/:id", id: "model-detail", pathPattern: "/models/:id", profileScoped: true, sidebarItemId: "models" },
  { canonicalPath: "/route/endpoints", id: "endpoints", pathPattern: "/route/endpoints", profileScoped: true, sidebarItem: { groupId: "configuration", icon: Plug, id: "endpoints", labelKey: "endpoints", to: "/route/endpoints" }, sidebarItemId: "endpoints" },
  { canonicalPath: "/route/ban-policies", id: "loadbalance-strategies", pathPattern: "/route/ban-policies", profileScoped: true, sidebarItem: { groupId: "configuration", icon: Scale, id: "loadbalance-strategies", labelKey: "loadbalanceStrategies", to: "/route/ban-policies" }, sidebarItemId: "loadbalance-strategies" },
  { canonicalPath: "/system/settings", id: "settings", pathPattern: "/system/settings", profileScoped: false, sidebarItem: { groupId: "access", icon: Settings, id: "settings", labelKey: "settings", to: "/system/settings" }, sidebarItemId: "settings" },
  { canonicalPath: "/control/proxy-keys", id: "proxy-api-keys", pathPattern: "/control/proxy-keys", profileScoped: false, sidebarItem: { groupId: "access", icon: KeyRound, id: "proxy-api-keys", labelKey: "apiKeys", to: "/control/proxy-keys" }, sidebarItemId: "proxy-api-keys" },
  { canonicalPath: "/route/pricing", id: "pricing-templates", pathPattern: "/route/pricing", profileScoped: true, sidebarItem: { groupId: "configuration", icon: Coins, id: "pricing-templates", labelKey: "pricingTemplates", to: "/route/pricing" }, sidebarItemId: "pricing-templates" },
  { canonicalPath: "/observe/requests", id: "request-logs", pathPattern: "/observe/requests", profileScoped: true, sidebarItem: { groupId: "observability", icon: FileText, id: "request-logs", labelKey: "requestLogs", to: "/observe/requests" }, sidebarItemId: "request-logs" },
  { canonicalPath: "/observe/requests/:requestId/audit", id: "request-log-audit", pathPattern: "/observe/requests/:requestId/audit", profileScoped: true, sidebarItemId: "request-logs" },
];

export const SHELL_SIDEBAR_ITEMS: readonly ShellSidebarItemDefinition[] = SHELL_ROUTE_METADATA.flatMap(
  (route) => (route.sidebarItem ? [route.sidebarItem] : [])
);

const GIT_RUN_NUMBER = String(import.meta.env.VITE_GIT_RUN_NUMBER ?? "local").trim() || "local";
const GIT_REVISION = String(import.meta.env.VITE_GIT_REVISION ?? "unknown").trim() || "unknown";

export const VERSION_LABEL = formatVersionLabel(APP_VERSION, GIT_RUN_NUMBER, GIT_REVISION);

type ShellBreadcrumbLeafId =
  | "request-logs-request"
  | "settings-audit-configuration"
  | "settings-authentication"
  | "settings-billing-currency"
  | "settings-retention-deletion"
  | "settings-timezone";

export interface ShellBreadcrumbItem {
  current: boolean;
  href: string | null;
  id: ShellRouteId | ShellBreadcrumbLeafId;
  label: string;
}

export interface LocalizedShellSidebarItem extends ShellSidebarItemDefinition {
  current: boolean;
  label: string;
}

export interface ShellNavigationState {
  activeSidebarItem: LocalizedShellSidebarItem | null;
  breadcrumbs: ShellBreadcrumbItem[];
  isProfileScopedPage: boolean;
  matchedRoute: ShellRouteMetadata;
  sidebarItems: LocalizedShellSidebarItem[];
}

interface MatchedShellRoute {
  params: Record<string, string>;
  route: ShellRouteMetadata;
}

const SETTINGS_HASH_BREADCRUMBS: Record<
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
  "audit-configuration": {
    id: "settings-audit-configuration",
    label: (messages) => messages.settingsPage.auditPrivacy,
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

function matchShellRoute(pathname: string): MatchedShellRoute {
  for (const route of SHELL_ROUTE_METADATA) {
    const match = matchPath({ end: true, path: route.pathPattern }, pathname);
    if (match) {
      return {
        params: normalizeParams(match.params),
        route,
      };
    }
  }

  return {
    params: {},
    route: SHELL_ROUTE_METADATA[0],
  };
}

function getTopLevelLabel(
  messages: Messages,
  route: ShellRouteMetadata,
): string {
  const sidebarItem = route.sidebarItem ?? SHELL_SIDEBAR_ITEMS.find((item) => item.id === route.sidebarItemId);
  return sidebarItem ? messages.nav[sidebarItem.labelKey] : messages.nav.dashboard;
}

function buildBreadcrumbs(
  matchedRoute: MatchedShellRoute,
  messages: Messages,
  hash: string,
  search: string
): ShellBreadcrumbItem[] {
  const routeLabel = getTopLevelLabel(messages, matchedRoute.route);

  switch (matchedRoute.route.id) {
    case "model-detail":
      return [
        { current: false, href: "/models", id: "models", label: messages.nav.models },
        { current: true, href: null, id: "model-detail", label: messages.modelDetail.configuration },
      ];

    case "settings": {
      const sectionHash = hash.replace(/^#/, "");
      const settingsLeaf = SETTINGS_HASH_BREADCRUMBS[sectionHash];
      if (settingsLeaf) {
        return [
          { current: false, href: "/settings", id: "settings", label: messages.nav.settings },
          {
            current: true,
            href: null,
            id: settingsLeaf.id,
            label: settingsLeaf.label(messages),
          },
        ];
      }

      return [{ current: true, href: null, id: "settings", label: messages.nav.settings }];
    }

    case "request-logs": {
      const requestId = new URLSearchParams(search).get("request_id")?.trim() ?? "";
      if (requestId) {
        return [
          {
            current: false,
            href: "/observe/requests",
            id: "request-logs",
            label: messages.nav.requestLogs,
          },
          {
            current: true,
            href: null,
            id: "request-logs-request",
            label: `#${requestId}`,
          },
        ];
      }

      return [{ current: true, href: null, id: "request-logs", label: messages.nav.requestLogs }];
    }

    case "request-log-audit": {
      const requestId = matchedRoute.params.requestId;
      return [
        {
          current: false,
          href: "/observe/requests",
          id: "request-logs",
          label: messages.nav.requestLogs,
        },
        {
          current: false,
          href: `/observe/requests?request_id=${encodeURIComponent(requestId)}`,
          id: "request-logs-request",
          label: `#${requestId}`,
        },
        {
          current: true,
          href: null,
          id: "request-log-audit",
          label: messages.requestLogs.audit,
        },
      ];
    }

    default:
      return [{ current: true, href: null, id: matchedRoute.route.id, label: routeLabel }];
  }
}

export function useShellNavigation(): ShellNavigationState {
  const location = useLocation();
  const { messages } = useLocale();

  return useMemo(() => {
    const matchedRoute = matchShellRoute(location.pathname);
    const sidebarItems = SHELL_SIDEBAR_ITEMS.map((item) => ({
      ...item,
      current: item.id === matchedRoute.route.sidebarItemId,
      label: messages.nav[item.labelKey],
    }));
    const activeSidebarItem = sidebarItems.find((item) => item.id === matchedRoute.route.sidebarItemId) ?? null;

    return {
      activeSidebarItem,
      breadcrumbs: buildBreadcrumbs(matchedRoute, messages, location.hash, location.search),
      isProfileScopedPage: matchedRoute.route.profileScoped,
      matchedRoute: matchedRoute.route,
      sidebarItems,
    };
  }, [location.hash, location.pathname, location.search, messages]);
}
