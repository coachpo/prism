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

      {/* 不要在这里加 overflow-hidden：它会让 <main> 成为 scrollport，
          而它随内容增高、从不滚动，于是所有以页面为参照的 sticky（审计页上下文条、
          设置页三处侧栏、每张表的表头）全部静默失效。横向溢出用子节点的
          min-w-0 压住，不要用 overflow 压。 */}
      <SidebarInset className="min-h-svh bg-canvas">
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
          // 页面布局的断点要按「内容区实际有多宽」算，不是按视口。侧栏展开会吃掉
          // 240px：同一个视口下把侧栏展开，按视口写的 lg: 仍然成立，表格却已经开始溢出。
          className="@container/main flex min-w-0 flex-1 flex-col gap-[var(--density-page-gap)] p-[var(--density-page-pad-y)] px-[var(--density-page-pad-x)]"
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
