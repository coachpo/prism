import { NavLink } from "react-router-dom";
import { X, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
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
  useSidebar,
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
      collapsible="icon"
      className="border-r border-sidebar-border/60"
    >
      <SidebarHeader className="gap-3 border-b border-sidebar-border/70 p-3">
        <div className="flex items-center gap-3 px-1">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            onClick={toggleSidebar}
            aria-label={desktopToggleLabel}
            title={desktopToggleLabel}
            className="hidden rounded-xl bg-sidebar-primary text-sidebar-primary-foreground shadow-sm hover:bg-sidebar-primary/90 hover:text-sidebar-primary-foreground lg:inline-flex"
          >
            <Zap className="h-4 w-4" />
          </Button>
          <div className="flex size-8 items-center justify-center rounded-xl bg-sidebar-primary text-sidebar-primary-foreground shadow-sm lg:hidden">
            <Zap className="h-4 w-4" />
          </div>
          <div className="grid min-w-0 flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
            <span className="truncate text-sm font-semibold">Prism</span>
            <span className="truncate text-[11px] text-sidebar-foreground/55">{VERSION_LABEL}</span>
          </div>
          <div className="ml-auto flex items-center gap-1.5">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              onClick={() => setOpenMobile(false)}
              aria-label={messages.shell.closeSidebar}
              title={messages.shell.closeSidebar}
              className="text-sidebar-foreground/55 hover:bg-sidebar-accent hover:text-sidebar-foreground lg:hidden"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </SidebarHeader>

      <SidebarContent className="gap-4 px-0 py-3">
        {groupedItems.map(({ groupId, items }) => (
          <SidebarGroup key={groupId} className="px-2">
            <SidebarGroupLabel>
              {groupId.replace(/-/g, " ")}
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {items.map((item) => {
                  const Icon = item.icon;

                  return (
                    <SidebarMenuItem key={item.id}>
                      <SidebarMenuButton asChild isActive={item.current} tooltip={item.label}>
                        <NavLink
                          to={item.to}
                          onClick={handleNavigate}
                          aria-label={state === "collapsed" ? item.label : undefined}
                          title={state === "collapsed" ? item.label : undefined}
                        >
                          <Icon className="h-4 w-4 shrink-0" />
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
            "rounded-xl border border-sidebar-border/70 bg-sidebar-accent/40 p-3 text-sidebar-foreground",
            "group-data-[collapsible=icon]:hidden"
          )}
        >
          <div className="flex items-center justify-between gap-2">
            <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-sidebar-foreground/55">
              {messages.shell.profileRuntime}
            </p>
            <span
              className={cn(
                "rounded-full border px-1.5 py-0.5 text-[10px] font-medium",
                hasMismatch
                  ? "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-200"
                  : "border-emerald-500/40 bg-emerald-500/15 text-emerald-700 dark:text-emerald-200"
              )}
            >
              {hasMismatch ? messages.shell.mismatch : messages.shell.aligned}
            </span>
          </div>
          <dl className="mt-2 grid grid-cols-[72px_minmax(0,1fr)] items-center gap-x-3 gap-y-1.5 text-xs">
            <dt className="text-sidebar-foreground/60">{messages.shell.viewing}</dt>
            <dd className="truncate font-medium text-sidebar-foreground/95">{selectedProfileName}</dd>
            <dt className="text-sidebar-foreground/60">{messages.shell.runtime}</dt>
            <dd className="truncate font-medium text-sidebar-foreground/95">{activeProfileName}</dd>
          </dl>
        </div>

      </SidebarFooter>
    </Sidebar>
  );
}
