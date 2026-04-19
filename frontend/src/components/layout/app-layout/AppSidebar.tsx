import { NavLink } from "react-router-dom";
import type { Profile } from "@/lib/types";
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
} from "./navigationProfileConfig";
import { MismatchFooter } from "./MismatchFooter";
import { NavUser } from "./NavUser";
import { ProfileSwitcher } from "./ProfileSwitcher";
import type { LocalizedShellSidebarItem } from "./useShellNavigation";
type Props = {
  activeProfileId: number | null;
  activeProfileName: string;
  authEnabled: boolean;
  canCreateProfile: boolean;
  closeProfileSwitcher: () => void;
  deleteDisabledReason: string | null;
  editDisabledReason: string | null;
  handleLogout: () => Promise<void>;
  handleManageProfiles: () => void;
  handleSelectProfile: (profileId: number) => void;
  hasMismatch: boolean;
  hasNoProfiles: boolean;
  isActivating: boolean;
  openActivateDialog: () => void;
  openCreateDialog: () => void;
  openDeleteDialog: () => void;
  openEditDialog: () => void;
  profileSwitcherOpen: boolean;
  profiles: Profile[];
  selectedIsActive: boolean;
  selectedProfileId: number | null;
  selectedProfileName: string;
  setProfileSwitcherOpen: (open: boolean) => void;
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
  activeProfileId,
  activeProfileName,
  authEnabled,
  canCreateProfile,
  closeProfileSwitcher,
  deleteDisabledReason,
  editDisabledReason,
  handleLogout,
  handleManageProfiles,
  handleSelectProfile,
  hasMismatch,
  hasNoProfiles,
  isActivating,
  openActivateDialog,
  openCreateDialog,
  openDeleteDialog,
  openEditDialog,
  profileSwitcherOpen,
  profiles,
  selectedIsActive,
  selectedProfileId,
  selectedProfileName,
  setProfileSwitcherOpen,
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
      collapsible="offcanvas"
      className="border-r-0"
    >
      <SidebarHeader className="border-b border-sidebar-border/70 p-3">
        <ProfileSwitcher
          activeProfileId={activeProfileId}
          activeProfileName={activeProfileName}
          canCreateProfile={canCreateProfile}
          deleteDisabledReason={deleteDisabledReason}
          editDisabledReason={editDisabledReason}
          handleManageProfiles={handleManageProfiles}
          handleSelectProfile={handleSelectProfile}
          hasNoProfiles={hasNoProfiles}
          isActivating={isActivating}
          openCreateDialog={openCreateDialog}
          openDeleteDialog={openDeleteDialog}
          openEditDialog={openEditDialog}
          profileSwitcherOpen={profileSwitcherOpen}
          profiles={profiles}
          selectedIsActive={selectedIsActive}
          selectedProfileId={selectedProfileId}
          selectedProfileName={selectedProfileName}
          setProfileSwitcherOpen={setProfileSwitcherOpen}
        />
      </SidebarHeader>

      <SidebarContent className="gap-1 px-0 py-4">
        {groupedItems.map(({ groupId, items }) => (
          <SidebarGroup key={groupId} className="gap-1 px-2 py-1">
            <SidebarGroupLabel className="px-2.5">
              {messages.shell.groupLabels[groupId]}
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
                        <NavLink to={item.to} onClick={handleNavigate}>
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
        <MismatchFooter
          activeProfileName={activeProfileName}
          hasMismatch={hasMismatch}
          isActivating={isActivating}
          openActivateDialog={openActivateDialog}
          selectedProfileName={selectedProfileName}
        />
        <NavUser
          authEnabled={authEnabled}
          handleLogout={handleLogout}
          username={username}
        />
      </SidebarFooter>
    </Sidebar>
  );
}
