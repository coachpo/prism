import type { CSSProperties, ReactNode } from "react";
import { useLocale } from "@/i18n/useLocale";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { AppSidebar } from "./app-layout/AppSidebar";
import { BreadcrumbEntityProvider } from "./app-layout/BreadcrumbEntityProvider";
import { SiteHeader } from "./app-layout/SiteHeader";
import { useAppLayoutState } from "./app-layout/useAppLayoutState";

/** 240px expanded, collapsing to a 56px icon rail rather than off-canvas. */
const SHELL_SIDEBAR_STYLE = {
  "--sidebar-width": "15rem",
  "--sidebar-width-icon": "3.5rem",
} as CSSProperties;

function Shell({ children }: { children?: ReactNode }) {
  const state = useAppLayoutState();
  const { messages } = useLocale();

  return (
    <SidebarProvider
      open={!state.desktopSidebarCollapsed}
      onOpenChange={state.setDesktopSidebarOpen}
      style={SHELL_SIDEBAR_STYLE}
    >
      <a
        href="#prism-main-content"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-sm font-medium text-on-primary focus:not-sr-only focus:absolute focus:left-4 focus:top-4"
      >
        {messages.shell.skipToMainContent}
      </a>
      <AppSidebar authEnabled={state.authEnabled} sidebarGroups={state.sidebarGroups} />

      <SidebarInset className="min-h-svh overflow-hidden bg-canvas">
        <SiteHeader
          authEnabled={state.authEnabled}
          breadcrumbs={state.breadcrumbs}
          densityMode={state.densityMode}
          handleLogout={state.handleLogout}
          onToggleDensity={state.toggleDensity}
          sidebarItems={state.sidebarItems}
          username={state.username}
        />
        <div
          id="prism-main-content"
          tabIndex={-1}
          className="flex flex-1 flex-col gap-[var(--density-page-gap)] p-[var(--density-page-pad-y)] px-[var(--density-page-pad-x)]"
        >
          {children}
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

export function Page({ children }: { children?: ReactNode }) {
  // The provider wraps the shell so a detail page can publish its entity name
  // for the breadcrumb leaf while breadcrumb assembly stays in the shell.
  return (
    <BreadcrumbEntityProvider>
      <Shell>{children}</Shell>
    </BreadcrumbEntityProvider>
  );
}
