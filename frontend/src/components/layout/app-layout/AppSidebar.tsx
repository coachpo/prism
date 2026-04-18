import { NavLink } from "react-router-dom";
import { X, Zap } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useSidebar } from "@/components/ui/sidebar-context";
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
  SidebarSeparator,
} from "@/components/ui/sidebar";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  SHELL_SIDEBAR_ITEMS,
  VERSION_LABEL,
  type ShellSidebarGroupId,
} from "./navigationProfileConfig";
import type { LocalizedShellSidebarItem } from "./useShellNavigation";

type Props = {
  activeProfileName: string;
  closeProfileSwitcher: () => void;
  hasMismatch: boolean;
  selectedProfileName: string;
  sidebarItems?: LocalizedShellSidebarItem[];
};

const SIDEBAR_GROUP_ORDER: ShellSidebarGroupId[] = [
  "overview",
  "configuration",
  "observability",
  "access",
];

function formatGroupLabel(groupId: ShellSidebarGroupId) {
  return groupId
    .split("-")
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function AppSidebar({
  activeProfileName,
  closeProfileSwitcher,
  hasMismatch,
  selectedProfileName,
  sidebarItems,
}: Props) {
  const { messages } = useLocale();
  const { isMobile, setOpenMobile, state, toggleSidebar } = useSidebar();
  const desktopToggleLabel =
    state === "collapsed" ? messages.shell.expandSidebar : messages.shell.collapseSidebar;
  const resolvedSidebarItems =
    sidebarItems ??
    SHELL_SIDEBAR_ITEMS.map((item) => ({
      ...item,
      current: false,
      label: messages.nav[item.labelKey],
    }));

  const groupedItems = SIDEBAR_GROUP_ORDER.map((groupId) => ({
    groupId,
    items: resolvedSidebarItems.filter((item) => item.groupId === groupId),
  })).filter((group) => group.items.length > 0);

  const handleNavigate = () => {
    closeProfileSwitcher();
    if (isMobile) {
      setOpenMobile(false);
    }
  };

  return (
    <Sidebar
      data-testid="shell-sidebar"
      aria-label={messages.shell.primaryNavigation}
      variant="inset"
      collapsible="icon"
      className="border-r-0"
    >
      <SidebarHeader className="gap-3 border-b border-sidebar-border/70 p-3">
        <div className="flex items-center gap-3 rounded-xl border border-sidebar-border/70 bg-sidebar-accent/35 px-2.5 py-2 shadow-sm">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={toggleSidebar}
            aria-label={desktopToggleLabel}
            title={desktopToggleLabel}
            className="hidden rounded-xl bg-sidebar-primary text-sidebar-primary-foreground shadow-sm hover:bg-sidebar-primary/90 hover:text-sidebar-primary-foreground lg:inline-flex"
          >
            <Zap />
          </Button>
          <div className="flex size-9 items-center justify-center rounded-xl bg-sidebar-primary text-sidebar-primary-foreground shadow-sm lg:hidden">
            <Zap className="size-4" />
          </div>
          <div className="grid min-w-0 flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
            <span className="truncate text-sm font-semibold">Prism</span>
            <span className="truncate text-[11px] text-sidebar-foreground/60">{VERSION_LABEL}</span>
          </div>
          <div className="ml-auto flex items-center gap-1.5 lg:hidden">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => setOpenMobile(false)}
              aria-label={messages.shell.closeSidebar}
              title={messages.shell.closeSidebar}
              className="text-sidebar-foreground/55 hover:bg-sidebar-accent hover:text-sidebar-foreground lg:hidden"
            >
              <X />
            </Button>
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent className="gap-1 px-0 py-4">
        {groupedItems.map(({ groupId, items }) => (
          <SidebarGroup key={groupId} className="gap-1 px-2 py-1">
            <SidebarGroupLabel className="px-2.5">
              {formatGroupLabel(groupId)}
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-1.5">
                {items.map((item) => {
                  const Icon = item.icon;

                  return (
                    <SidebarMenuItem key={item.id}>
                      <SidebarMenuButton
                        asChild
                        isActive={item.current}
                        tooltip={item.label}
                        className="rounded-lg px-2.5"
                      >
                        <NavLink
                          to={item.to}
                          onClick={handleNavigate}
                          aria-label={state === "collapsed" ? item.label : undefined}
                          title={state === "collapsed" ? item.label : undefined}
                        >
                          <Icon />
                          <span>{item.label}</span>
                        </NavLink>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  );
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarSeparator />

      <SidebarFooter className="gap-3 p-3">
        <div
          className={cn(
            "rounded-xl border border-sidebar-border/70 bg-sidebar-accent/35 p-3 text-sidebar-foreground shadow-sm",
            "group-data-[collapsible=icon]:hidden"
          )}
        >
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-sidebar-foreground/55">
                {messages.shell.profileRuntime}
              </p>
              <p className="mt-1 text-xs text-sidebar-foreground/65">
                {messages.shell.viewing} · {messages.shell.runtime}
              </p>
            </div>
            <Badge
              variant="outline"
              className={cn(
                "shrink-0 rounded-full px-2 py-1 text-[10px] font-medium uppercase tracking-[0.12em]",
                hasMismatch
                  ? "border-warning/40 bg-warning/15 text-sidebar-foreground"
                  : "border-success/35 bg-success/15 text-sidebar-foreground"
              )}
            >
              {hasMismatch ? messages.shell.mismatch : messages.shell.aligned}
            </Badge>
          </div>
          <dl className="mt-3 grid gap-2 text-xs">
            <div className="rounded-lg border border-sidebar-border/60 bg-sidebar/50 px-3 py-2">
              <dt className="text-[10px] font-semibold uppercase tracking-[0.14em] text-sidebar-foreground/50">
                {messages.shell.viewing}
              </dt>
              <dd className="mt-1 truncate text-sm font-medium text-sidebar-foreground/95">
                {selectedProfileName}
              </dd>
            </div>
            <div className="rounded-lg border border-sidebar-border/60 bg-sidebar/50 px-3 py-2">
              <dt className="text-[10px] font-semibold uppercase tracking-[0.14em] text-sidebar-foreground/50">
                {messages.shell.runtime}
              </dt>
              <dd className="mt-1 truncate text-sm font-medium text-sidebar-foreground/95">
                {activeProfileName}
              </dd>
            </div>
          </dl>
        </div>

      </SidebarFooter>
    </Sidebar>
  );
}
