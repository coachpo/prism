import type { CSSProperties, ReactNode } from "react";
import { useLocale } from "@/i18n/useLocale";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { AppSidebar } from "./app-layout/AppSidebar";
import { SiteHeader } from "./app-layout/SiteHeader";
import { useAppLayoutState } from "./app-layout/useAppLayoutState";

export function Page({ children }: { children?: ReactNode }) {
  const state = useAppLayoutState();
  const { messages } = useLocale();

  return (
    <>
      <SidebarProvider
        open={!state.desktopSidebarCollapsed}
        onOpenChange={state.setDesktopSidebarOpen}
        style={{ "--sidebar-width": "20rem" } as CSSProperties}
      >
        <a
          href="#prism-main-content"
          className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground shadow-operator-panel focus:not-sr-only focus:absolute focus:left-4 focus:top-4"
        >
          {messages.shell.skipToMainContent}
        </a>
        <AppSidebar
          authEnabled={state.authEnabled}
          handleLogout={state.handleLogout}
          sidebarItems={state.sidebarItems}
          username={state.username}
        />

        <SidebarInset className="min-h-svh overflow-hidden">
          <SiteHeader breadcrumbs={state.breadcrumbs} />
          <div
            id="prism-main-content"
            tabIndex={-1}
            className="flex flex-1 flex-col gap-[var(--density-page-gap)] p-[var(--density-page-pad-y)] px-[var(--density-page-pad-x)] outline-none"
          >
            {children}
          </div>
        </SidebarInset>
      </SidebarProvider>
    </>
  );
}
