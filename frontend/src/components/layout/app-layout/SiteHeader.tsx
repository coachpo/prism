import { Fragment } from "react";
import { Link } from "react-router-dom";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { ShellScopeBadge, type ShellScopeBadgeKind } from "@/shell";
import type { ShellBreadcrumbItem } from "./useShellNavigation";

type Props = {
  breadcrumbs?: ShellBreadcrumbItem[];
  scopeBadge?: ShellScopeBadgeKind | null;
};

export function SiteHeader({ breadcrumbs = [], scopeBadge = null }: Props) {
  return (
    <header className="flex h-16 shrink-0 items-center gap-2 border-b border-outline-variant bg-surface px-[var(--density-shell-header-pad-x)]">
      <SidebarTrigger className="-ml-1" />
      <Separator orientation="vertical" className="h-4 shrink-0" />
      <Breadcrumb data-testid="shell-breadcrumb" className="min-w-0 flex-1">
        <BreadcrumbList className="min-w-0 flex-nowrap overflow-hidden">
          {breadcrumbs.map((breadcrumb, index) => {
            const item = breadcrumb.current ? (
              <BreadcrumbPage data-testid="shell-breadcrumb-current" className="truncate">
                {breadcrumb.label}
              </BreadcrumbPage>
            ) : breadcrumb.href ? (
              <BreadcrumbLink asChild className="truncate">
                <Link to={breadcrumb.href}>{breadcrumb.label}</Link>
              </BreadcrumbLink>
            ) : (
              <span className="truncate">{breadcrumb.label}</span>
            );

            return (
              <Fragment key={breadcrumb.id}>
                <BreadcrumbItem className="min-w-0 max-w-full truncate">{item}</BreadcrumbItem>
                {index < breadcrumbs.length - 1 ? <BreadcrumbSeparator className="shrink-0" /> : null}
              </Fragment>
            );
          })}
        </BreadcrumbList>
      </Breadcrumb>
      {scopeBadge ? <ShellScopeBadge kind={scopeBadge} /> : null}
    </header>
  );
}
