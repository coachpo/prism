import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import { readSidebarCollapsed, writeSidebarCollapsed } from "./sidebarPersistence";
import type { ShellScopeBadgeKind } from "@/shell";
import { useShellNavigation } from "./useShellNavigation";

export function useAppLayoutState() {
  const navigate = useNavigate();
  const { authEnabled, username, logout } = useAuth();
  const { messages } = useLocale();
  const shellNavigation = useShellNavigation();

  const [desktopSidebarCollapsed, setDesktopSidebarCollapsed] = useState(() =>
    readSidebarCollapsed()
  );

  useEffect(() => {
    writeSidebarCollapsed(desktopSidebarCollapsed);
  }, [desktopSidebarCollapsed]);

  const setDesktopSidebarOpen = (open: boolean) => {
    setDesktopSidebarCollapsed(!open);
  };

  const toggleDesktopSidebar = () => {
    setDesktopSidebarCollapsed((current) => !current);
  };

  const handleLogout = async () => {
    try {
      await logout();
      navigate("/auth/login", { replace: true });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : messages.shell.logoutFailed
      );
    }
  };

  const shellScopeBadge: ShellScopeBadgeKind | null = shellNavigation.isProfileScopedPage
    ? "selected-profile"
    : shellNavigation.matchedRoute.profileScoped === false
      ? "global"
      : null;

  return {
    authEnabled,
    breadcrumbs: shellNavigation.breadcrumbs,
    desktopSidebarCollapsed,
    handleLogout,
    isProfileScopedPage: shellNavigation.isProfileScopedPage,
    setDesktopSidebarOpen,
    shellScopeBadge,
    sidebarItems: shellNavigation.sidebarItems,
    toggleDesktopSidebar,
    username,
  };
}
