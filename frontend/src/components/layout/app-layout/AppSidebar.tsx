import { Link, useNavigate } from "@tanstack/react-router";
import type { MouseEvent } from "react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useSidebar } from "@/components/ui/sidebar-context";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import { SidebarFooterStatus } from "./SidebarFooterStatus";
import type { LocalizedShellSidebarGroup } from "./useShellNavigation";

type Props = {
  authEnabled: boolean;
  sidebarGroups: LocalizedShellSidebarGroup[];
};

/**
 * The gateway an operator is looking at. Self-hosted installs run several of
 * these, and the origin is the only instance identity the backend actually
 * exposes — nothing here is invented.
 */
function gatewayBase(): string {
  if (typeof window === "undefined") return "";
  return window.location.origin.replace(/^https?:\/\//, "");
}

export function AppSidebar({ authEnabled, sidebarGroups }: Props) {
  const { messages } = useLocale();
  const { isMobile, setOpenMobile } = useSidebar();
  const navigate = useNavigate();
  const base = gatewayBase();

  const handleNavigate = (event: MouseEvent<HTMLAnchorElement>, to: string) => {
    event.preventDefault();
    void navigate({ to });
    if (isMobile) setOpenMobile(false);
  };

  return (
    <Sidebar
      data-testid="shell-sidebar"
      aria-label={messages.shell.primaryNavigation}
      collapsible="icon"
      className="border-r border-sidebar-border"
    >
      {/* Same 48px band as SiteHeader with the rule inside it, so the two
          bottom outlines read as one continuous line across the viewport. */}
      <SidebarHeader className="h-12 shrink-0 border-b border-sidebar-border p-0">
        <Link
          to="/observe"
          aria-label={messages.shell.goToDashboard}
          onClick={(event) => handleNavigate(event, "/observe")}
          className="flex h-full items-center gap-2 px-3 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
        >
          <span
            aria-hidden="true"
            className="flex size-6 shrink-0 items-center justify-center rounded-[4px] bg-primary text-[0.6875rem] font-semibold text-on-primary"
          >
            P
          </span>
          <span className="flex min-w-0 flex-col group-data-[collapsible=icon]:hidden">
            <span className="text-[0.8125rem] font-semibold leading-4 text-sidebar-foreground">
              Prism
            </span>
            {base ? (
              <span
                title={`${messages.shell.gatewayBaseLabel}: ${base}`}
                className="truncate font-mono text-[0.6875rem] leading-4 text-muted-foreground"
              >
                {base}
              </span>
            ) : null}
          </span>
        </Link>
      </SidebarHeader>

      <SidebarContent className="gap-0 px-2 py-2">
        {sidebarGroups.map((group) => (
          <SidebarGroup key={group.id} className="gap-0.5 px-0 py-1">
            <SidebarGroupLabel className="px-2 text-[11px] font-medium normal-case tracking-normal text-muted-foreground">
              {group.label}
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {group.items.map((item) => {
                  const ItemIcon = item.icon;
                  return (
                    <SidebarMenuItem key={item.id}>
                      <SidebarMenuButton
                        asChild
                        isActive={item.current}
                        tooltip={item.label}
                        className={cn(
                          "relative h-8 rounded-md px-2 text-[0.8125rem]",
                          // Active reads as a tinted row with a 2px edge bar,
                          // not a solid blue reverse fill.
                          "data-[active=true]:bg-primary-soft data-[active=true]:text-on-primary-soft",
                          "data-[active=true]:before:absolute data-[active=true]:before:left-0 data-[active=true]:before:top-1 data-[active=true]:before:bottom-1 data-[active=true]:before:w-0.5 data-[active=true]:before:rounded-full data-[active=true]:before:bg-primary",
                        )}
                      >
                        <Link
                          to={item.to}
                          activeOptions={{ exact: true, includeSearch: false }}
                          activeProps={{ "aria-current": "page" }}
                          onClick={(event) => handleNavigate(event, item.to)}
                        >
                          <ItemIcon aria-hidden="true" />
                          <span>{item.label}</span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter className="gap-0 border-t border-sidebar-border p-2">
        <SidebarFooterStatus authEnabled={authEnabled} />
      </SidebarFooter>
    </Sidebar>
  );
}
