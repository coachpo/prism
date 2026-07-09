import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import { readSidebarCollapsed, writeSidebarCollapsed } from "./sidebarPersistence";
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
      void navigate({ to: "/auth/login", replace: true });
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : messages.shell.logoutFailed
      );
    }
  };

  return {
    authEnabled,
    breadcrumbs: shellNavigation.breadcrumbs,
    desktopSidebarCollapsed,
    handleLogout,
    setDesktopSidebarOpen,
    sidebarItems: shellNavigation.sidebarItems,
    toggleDesktopSidebar,
    username,
  };
}
