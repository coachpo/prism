import { Check, ChevronsUpDown, Pencil, Plus, Settings2, Trash2 } from "lucide-react";
import type { Profile } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import { useSidebar } from "@/components/ui/sidebar-context";
import { useLocale } from "@/i18n/useLocale";

type Props = {
  activeProfileId: number | null;
  activeProfileName: string;
  canCreateProfile: boolean;
  deleteDisabledReason: string | null;
  editDisabledReason: string | null;
  profiles: Profile[];
  handleManageProfiles: () => void;
  handleSelectProfile: (profileId: number) => void;
  hasNoProfiles: boolean;
  isActivating: boolean;
  openCreateDialog: () => void;
  openDeleteDialog: () => void;
  openEditDialog: () => void;
  profileSwitcherOpen: boolean;
  selectedIsActive: boolean;
  selectedProfileId: number | null;
  selectedProfileName: string;
  setProfileSwitcherOpen: (open: boolean) => void;
};

export function ProfileSwitcher({
  activeProfileId,
  activeProfileName,
  canCreateProfile,
  deleteDisabledReason,
  editDisabledReason,
  profiles,
  handleManageProfiles,
  handleSelectProfile,
  hasNoProfiles,
  isActivating,
  openCreateDialog,
  openDeleteDialog,
  openEditDialog,
  profileSwitcherOpen,
  selectedIsActive,
  selectedProfileId,
  selectedProfileName,
  setProfileSwitcherOpen,
}: Props) {
  const { messages } = useLocale();
  const { isMobile } = useSidebar();

  const triggerLabel = hasNoProfiles
    ? messages.profiles.noProfilesTitle
    : messages.profiles.profileTriggerTitle(selectedProfileName, activeProfileName);

  return (
    <SidebarMenu>
      <SidebarMenuItem data-testid="shell-profile-switcher">
          <DropdownMenu open={profileSwitcherOpen} onOpenChange={setProfileSwitcherOpen}>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                size="lg"
                tooltip={triggerLabel}
                disabled={isActivating}
                className="rounded-lg border border-sidebar-border/70 bg-sidebar-accent/35 px-2.5 py-2 hover:bg-sidebar-accent/60 data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
              >
                <div className="grid min-w-0 flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
                  <span className="truncate text-[11px] text-sidebar-foreground/60">
                    {messages.shell.profile}
                  </span>
                  <span className="truncate text-sm font-medium">{selectedProfileName}</span>
                </div>
                <div className="ml-auto flex items-center gap-2 group-data-[collapsible=icon]:hidden">
                  {selectedIsActive ? (
                    <Badge variant="secondary" className="h-5 px-1.5 text-[10px] uppercase tracking-[0.12em]">
                      {messages.profiles.active}
                    </Badge>
                  ) : null}
                  <ChevronsUpDown className="text-sidebar-foreground/60" />
                </div>
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              align="start"
              side={isMobile ? "bottom" : "right"}
              className="w-[min(22rem,calc(100vw-1rem))]"
            >
              <DropdownMenuGroup>
                <DropdownMenuLabel className="grid gap-1">
                  <span>{messages.profiles.selectProfile}</span>
                  <span className="truncate text-xs font-normal text-muted-foreground">
                    {messages.profiles.activeShort(activeProfileName)}
                  </span>
                </DropdownMenuLabel>
                {hasNoProfiles ? (
                  <div className="px-2 py-2">
                    <p className="text-sm font-medium">{messages.profiles.noProfilesTitle}</p>
                    <p className="text-xs text-muted-foreground">
                      {messages.profiles.noProfilesDescription}
                    </p>
                  </div>
                ) : (
                  profiles.map((profile) => {
                    const isSelected = selectedProfileId === profile.id;

                    return (
                      <DropdownMenuItem
                        key={profile.id}
                        onSelect={() => handleSelectProfile(profile.id)}
                        className="items-start gap-3"
                      >
                        <div className="grid min-w-0 flex-1 gap-1">
                          <span className="truncate font-medium">{profile.name}</span>
                          <span className="truncate text-xs text-muted-foreground">
                            {profile.description?.trim() || messages.profiles.noDescription}
                          </span>
                          <div className="flex flex-wrap items-center gap-1">
                            {profile.id === activeProfileId ? (
                              <Badge variant="secondary" className="h-5 px-1.5 text-[10px] uppercase tracking-[0.12em]">
                                {messages.profiles.active}
                              </Badge>
                            ) : null}
                            {profile.is_default ? <Badge variant="outline">{messages.profiles.default}</Badge> : null}
                            {!profile.is_editable ? <Badge variant="outline">{messages.profiles.locked}</Badge> : null}
                          </div>
                        </div>
                        <Check className={cn("mt-0.5 text-primary", isSelected ? "opacity-100" : "opacity-0")} />
                      </DropdownMenuItem>
                    );
                  })
                )}
              </DropdownMenuGroup>
              {!hasNoProfiles ? (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuItem disabled={Boolean(editDisabledReason)} title={editDisabledReason ?? undefined} onSelect={openEditDialog}>
                      <Pencil />
                      {messages.profiles.editSelected}
                    </DropdownMenuItem>
                    <DropdownMenuItem variant="destructive" disabled={Boolean(deleteDisabledReason)} title={deleteDisabledReason ?? undefined} onSelect={openDeleteDialog}>
                      <Trash2 />
                      {messages.profiles.deleteSelected}
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </>
              ) : null}
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem onSelect={handleManageProfiles}>
                  <Settings2 />
                  {messages.profiles.manageProfiles}
                </DropdownMenuItem>
                <DropdownMenuItem disabled={!canCreateProfile} onSelect={openCreateDialog}>
                  <Plus />
                  {messages.profiles.createNewProfile}
                </DropdownMenuItem>
              </DropdownMenuGroup>
              {!canCreateProfile ? <p className="px-2 py-1 text-xs text-muted-foreground">{messages.profiles.limitReached}</p> : null}
            </DropdownMenuContent>
          </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
