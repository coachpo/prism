import { useMemo } from "react";
import { matchPath, useLocation } from "react-router-dom";
import { useLocale } from "@/i18n/useLocale";
import type { Messages } from "@/i18n/messages/en";
import {
  SHELL_ROUTE_METADATA,
  SHELL_SIDEBAR_ITEMS,
  type ShellRouteId,
  type ShellRouteMetadata,
  type ShellSidebarItemDefinition,
} from "./navigationProfileConfig";

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
