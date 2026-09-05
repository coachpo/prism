import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import { useDensityMode } from "./densityMode";
import { readSidebarCollapsed, writeSidebarCollapsed } from "./sidebarPersistence";
import { useShellNavigation } from "./useShellNavigation";

export function useAppLayoutState() {
  const navigate = useNavigate();
  const { authEnabled, username, logout } = useAuth();
  const { messages } = useLocale();
  const shellNavigation = useShellNavigation();
  const density = useDensityMode();

  const [desktopSidebarCollapsed, setDesktopSidebarCollapsed] = useState(() =>
    readSidebarCollapsed()
  );

  // 只有操作者亲手改过才写入：否则首次渲染就会把「按视口推断的默认值」
  // 固化下来，换个屏幕再也推断不出。
  const commitSidebarCollapsed = (collapsed: boolean) => {
    writeSidebarCollapsed(collapsed);
    setDesktopSidebarCollapsed(collapsed);
  };

  const setDesktopSidebarOpen = (open: boolean) => {
    commitSidebarCollapsed(!open);
  };

  const toggleDesktopSidebar = () => {
    commitSidebarCollapsed(!desktopSidebarCollapsed);
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
    densityMode: density.mode,
    desktopSidebarCollapsed,
    handleLogout,
    setDesktopSidebarOpen,
    sidebarGroups: shellNavigation.sidebarGroups,
    sidebarItems: shellNavigation.sidebarItems,
    toggleDensity: density.toggle,
    toggleDesktopSidebar,
    username,
  };
}
