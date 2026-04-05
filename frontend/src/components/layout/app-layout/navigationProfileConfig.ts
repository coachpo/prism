import type { LucideIcon } from "lucide-react";
import {
  BarChart3,
  Coins,
  FileText,
  KeyRound,
  LayoutDashboard,
  Plug,
  Scale,
  Server,
  Settings,
} from "lucide-react";
import type { Messages } from "@/i18n/messages/en";
import { APP_VERSION, formatVersionLabel } from "@/lib/appVersion";

export type NavLabelKey = keyof Messages["nav"];

export type ShellSidebarGroupId = "overview" | "configuration" | "observability" | "access";

export type ShellSidebarItemId =
  | "dashboard"
  | "models"
  | "endpoints"
  | "loadbalance-strategies"
  | "statistics"
  | "settings"
  | "proxy-api-keys"
  | "pricing-templates"
  | "request-logs";

export type ShellRouteId =
  | ShellSidebarItemId
  | "model-detail"
  | "proxy-model-detail";

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
  {
    canonicalPath: "/dashboard",
    id: "dashboard",
    pathPattern: "/dashboard",
    profileScoped: false,
    sidebarItem: {
      groupId: "overview",
      icon: LayoutDashboard,
      id: "dashboard",
      labelKey: "dashboard",
      to: "/dashboard",
    },
    sidebarItemId: "dashboard",
  },
  {
    canonicalPath: "/models",
    id: "models",
    pathPattern: "/models",
    profileScoped: true,
    sidebarItem: {
      groupId: "configuration",
      icon: Server,
      id: "models",
      labelKey: "models",
      to: "/models",
    },
    sidebarItemId: "models",
  },
  {
    canonicalPath: "/models/:id/proxy",
    id: "proxy-model-detail",
    pathPattern: "/models/:id/proxy",
    profileScoped: true,
    sidebarItemId: "models",
  },
  {
    canonicalPath: "/models/:id",
    id: "model-detail",
    pathPattern: "/models/:id",
    profileScoped: true,
    sidebarItemId: "models",
  },
  {
    canonicalPath: "/endpoints",
    id: "endpoints",
    pathPattern: "/endpoints",
    profileScoped: true,
    sidebarItem: {
      groupId: "configuration",
      icon: Plug,
      id: "endpoints",
      labelKey: "endpoints",
      to: "/endpoints",
    },
    sidebarItemId: "endpoints",
  },
  {
    canonicalPath: "/loadbalance-strategies",
    id: "loadbalance-strategies",
    pathPattern: "/loadbalance-strategies",
    profileScoped: true,
    sidebarItem: {
      groupId: "configuration",
      icon: Scale,
      id: "loadbalance-strategies",
      labelKey: "loadbalanceStrategies",
      to: "/loadbalance-strategies",
    },
    sidebarItemId: "loadbalance-strategies",
  },
  {
    canonicalPath: "/statistics",
    id: "statistics",
    pathPattern: "/statistics",
    profileScoped: true,
    sidebarItem: {
      groupId: "observability",
      icon: BarChart3,
      id: "statistics",
      labelKey: "statistics",
      to: "/statistics",
    },
    sidebarItemId: "statistics",
  },
  {
    canonicalPath: "/settings",
    id: "settings",
    pathPattern: "/settings",
    profileScoped: false,
    sidebarItem: {
      groupId: "access",
      icon: Settings,
      id: "settings",
      labelKey: "settings",
      to: "/settings",
    },
    sidebarItemId: "settings",
  },
  {
    canonicalPath: "/proxy-api-keys",
    id: "proxy-api-keys",
    pathPattern: "/proxy-api-keys",
    profileScoped: false,
    sidebarItem: {
      groupId: "access",
      icon: KeyRound,
      id: "proxy-api-keys",
      labelKey: "apiKeys",
      to: "/proxy-api-keys",
    },
    sidebarItemId: "proxy-api-keys",
  },
  {
    canonicalPath: "/pricing-templates",
    id: "pricing-templates",
    pathPattern: "/pricing-templates",
    profileScoped: true,
    sidebarItem: {
      groupId: "configuration",
      icon: Coins,
      id: "pricing-templates",
      labelKey: "pricingTemplates",
      to: "/pricing-templates",
    },
    sidebarItemId: "pricing-templates",
  },
  {
    canonicalPath: "/request-logs",
    id: "request-logs",
    pathPattern: "/request-logs",
    profileScoped: true,
    sidebarItem: {
      groupId: "observability",
      icon: FileText,
      id: "request-logs",
      labelKey: "requestLogs",
      to: "/request-logs",
    },
    sidebarItemId: "request-logs",
  },
];

export const SHELL_SIDEBAR_ITEMS: readonly ShellSidebarItemDefinition[] = SHELL_ROUTE_METADATA.flatMap(
  (route) => (route.sidebarItem ? [route.sidebarItem] : [])
);

const GIT_RUN_NUMBER = String(import.meta.env.VITE_GIT_RUN_NUMBER ?? "local").trim() || "local";
const GIT_REVISION = String(import.meta.env.VITE_GIT_REVISION ?? "unknown").trim() || "unknown";

export const VERSION_LABEL = formatVersionLabel(APP_VERSION, GIT_RUN_NUMBER, GIT_REVISION);
