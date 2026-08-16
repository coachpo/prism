import { Fragment } from "react";
import { Link } from "@tanstack/react-router";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { ThemeToggle } from "@/components/ThemeToggle";
import { useLocale } from "@/i18n/useLocale";
import type { OperatorDensityMode } from "@/shared/design-system";
import { DensityToggle } from "./DensityToggle";
import { GlobalSearch } from "./GlobalSearch";
import { HeaderAccountMenu } from "./HeaderAccountMenu";
import type { LocalizedShellSidebarItem, ShellBreadcrumbItem } from "./useShellNavigation";

type Props = {
  authEnabled: boolean;
  breadcrumbs?: ShellBreadcrumbItem[];
  densityMode: OperatorDensityMode;
  handleLogout: () => Promise<void>;
  onToggleDensity: () => void;
  sidebarItems: LocalizedShellSidebarItem[];
  username: string | null;
};

/**
 * 48px, one outline at the bottom. Breadcrumb on the left; search, density,
 * theme, and account on the right, so the right side is no longer empty and
 * the sidebar footer no longer carries display controls.
 */
export function SiteHeader({
  authEnabled,
  breadcrumbs = [],
  densityMode,
  handleLogout,
  onToggleDensity,
  sidebarItems,
  username,
}: Props) {
  const { messages } = useLocale();

  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-panel px-[var(--density-shell-header-pad-x)]">
      <SidebarTrigger className="-ml-1 size-7" aria-label={messages.shell.toggleSidebar} />
      <Breadcrumb
        data-testid="shell-breadcrumb"
        aria-label={messages.shell.breadcrumb}
        className="min-w-0 flex-1"
      >
        <BreadcrumbList className="min-w-0 flex-nowrap gap-1 overflow-hidden text-xs sm:gap-1">
          {breadcrumbs.map((breadcrumb, index) => {
            const item = breadcrumb.current ? (
              <BreadcrumbPage
                data-testid="shell-breadcrumb-current"
                className="truncate font-medium text-foreground"
              >
                {breadcrumb.label}
              </BreadcrumbPage>
            ) : breadcrumb.href ? (
              <BreadcrumbLink asChild className="truncate">
                <Link to={breadcrumb.href}>{breadcrumb.label}</Link>
              </BreadcrumbLink>
            ) : (
              <span className="truncate text-muted-foreground">{breadcrumb.label}</span>
            );

            return (
              <Fragment key={`${breadcrumb.id}-${index}`}>
                <BreadcrumbItem className="min-w-0 max-w-full truncate">{item}</BreadcrumbItem>
                {index < breadcrumbs.length - 1 ? <BreadcrumbSeparator className="shrink-0" /> : null}
              </Fragment>
            );
          })}
        </BreadcrumbList>
      </Breadcrumb>

      <div className="flex shrink-0 items-center gap-1">
        <GlobalSearch sidebarItems={sidebarItems} />
        <DensityToggle mode={densityMode} onToggle={onToggleDensity} />
        <ThemeToggle />
        <HeaderAccountMenu
          authEnabled={authEnabled}
          handleLogout={handleLogout}
          username={username}
        />
      </div>
    </header>
  );
}
