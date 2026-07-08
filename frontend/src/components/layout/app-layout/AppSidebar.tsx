import { NavLink } from "react-router-dom";
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
import { useSidebar } from "@/components/ui/sidebar-context";
import { useLocale } from "@/i18n/useLocale";
import {
  SHELL_SIDEBAR_ITEMS,
  type ShellSidebarGroupId,
} from "./useShellNavigation";
import { NavUser } from "./NavUser";
import type { LocalizedShellSidebarItem } from "./useShellNavigation";
type Props = {
  authEnabled: boolean;
  handleLogout: () => Promise<void>;
  sidebarItems?: LocalizedShellSidebarItem[];
  username: string | null;
};

const SIDEBAR_GROUP_ORDER: ShellSidebarGroupId[] = [
  "overview",
  "configuration",
  "observability",
  "access",
];

export function AppSidebar({
  authEnabled,
  handleLogout,
  sidebarItems,
  username,
}: Props) {
  const { messages } = useLocale();
  const { isMobile, setOpenMobile } = useSidebar();

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
    if (isMobile) {
      setOpenMobile(false);
    }
  };

  return (
    <Sidebar
      data-testid="shell-sidebar"
      aria-label={messages.shell.primaryNavigation}
      variant="inset"
      collapsible="offcanvas"
      className="border-r-0"
    >
      <SidebarHeader className="border-b border-sidebar-border/70 p-3">
        <div className="px-2 py-1 text-sm font-semibold text-sidebar-foreground">Prism</div>
      </SidebarHeader>

      <SidebarContent className="gap-1 px-0 py-4">
        {groupedItems.map(({ groupId, items }) => (
          <SidebarGroup key={groupId} className="gap-1 px-2 py-1">
            <SidebarGroupLabel className="px-2.5">
              {messages.shell.groupLabels[groupId]}
            </SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu className="gap-1.5">
                {items.map((item) => (
                  <SidebarMenuItem key={item.id}>
                    <SidebarMenuButton
                      asChild
                      isActive={item.current}
                      tooltip={item.label}
                      className="rounded-lg px-2.5"
                    >
                      <NavLink to={item.to} onClick={handleNavigate}>
                        <span>{item.label}</span>
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarSeparator />

      <SidebarFooter className="gap-3 p-3">
        <NavUser
          authEnabled={authEnabled}
          handleLogout={handleLogout}
          username={username}
        />
      </SidebarFooter>
    </Sidebar>
  );
}
