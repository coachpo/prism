import { Link } from "react-router-dom";
import { AlertTriangle, Loader2, LogOut } from "lucide-react";
import { Fragment } from "react";
import { GlobalPreferencesControls } from "@/components/GlobalPreferencesControls";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useSidebar } from "@/components/ui/sidebar-context";
import { useLocale } from "@/i18n/useLocale";
import { ProfileSwitcherPopover } from "./ProfileSwitcherPopover";
import type { ShellBreadcrumbItem } from "./useShellNavigation";

type Props = {
  activeProfileName: string;
  authEnabled: boolean;
  breadcrumbs?: ShellBreadcrumbItem[];
  canCreateProfile: boolean;
  deleteDisabledReason: string | null;
  editDisabledReason: string | null;
  filteredProfiles: Array<{
    id: number;
    name: string;
    description: string | null;
    is_active: boolean;
    is_default: boolean;
    is_editable: boolean;
    version: number;
    deleted_at: string | null;
    created_at: string;
    updated_at: string;
  }>;
  handleLogout: () => Promise<void>;
  handleManageProfiles: () => void;
  handleSelectProfile: (profileId: number) => void;
  hasMismatch: boolean;
  hasNoMatches: boolean;
  hasNoProfiles: boolean;
  isActivating: boolean;
  isProfileScopedPage: boolean;
  openActivateDialog: () => void;
  openCreateDialog: () => void;
  openDeleteDialog: () => void;
  openEditDialog: () => void;
  profileQuery: string;
  profileSearchInputRef: React.RefObject<HTMLInputElement | null>;
  profileSwitcherOpen: boolean;
  selectedIsActive: boolean;
  selectedProfileButtonRef: React.RefObject<HTMLButtonElement | null>;
  selectedProfileId: number | null;
  selectedProfileName: string;
  setProfileQuery: (value: string) => void;
  setProfileSwitcherOpen: (open: boolean) => void;
  username: string | null;
};

export function AppHeader({
  activeProfileName,
  authEnabled,
  breadcrumbs = [],
  canCreateProfile,
  deleteDisabledReason,
  editDisabledReason,
  filteredProfiles,
  handleLogout,
  handleManageProfiles,
  handleSelectProfile,
  hasMismatch,
  hasNoMatches,
  hasNoProfiles,
  isActivating,
  isProfileScopedPage,
  openActivateDialog,
  openCreateDialog,
  openDeleteDialog,
  openEditDialog,
  profileQuery,
  profileSearchInputRef,
  profileSwitcherOpen,
  selectedIsActive,
  selectedProfileButtonRef,
  selectedProfileId,
  selectedProfileName,
  setProfileQuery,
  setProfileSwitcherOpen,
  username,
}: Props) {
  const { messages } = useLocale();
  const { isMobile } = useSidebar();

  return (
    <header className="shell-header sticky top-0 z-30 border-b bg-background/95 backdrop-blur-sm">
      <div className="shell-frame flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <SidebarTrigger
            aria-label={isMobile ? messages.shell.openSidebar : undefined}
            title={isMobile ? messages.shell.openSidebar : undefined}
            className="-ml-0.5 shrink-0"
          />
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
        </div>

        <div className="flex w-full flex-wrap items-center justify-end gap-2 xl:w-auto xl:flex-nowrap">
          <div
            data-testid="shell-profile-switcher"
            className="min-w-0 flex-1 basis-full sm:flex-none sm:basis-auto"
          >
            <ProfileSwitcherPopover
              open={profileSwitcherOpen}
              onOpenChange={setProfileSwitcherOpen}
              isActivating={isActivating}
              selectedProfileName={selectedProfileName}
              activeProfileName={activeProfileName}
              hasNoProfiles={hasNoProfiles}
              selectedIsActive={selectedIsActive}
              profileQuery={profileQuery}
              setProfileQuery={setProfileQuery}
              selectedProfileId={selectedProfileId}
              filteredProfiles={filteredProfiles}
              hasNoMatches={hasNoMatches}
              canCreateProfile={canCreateProfile}
              editDisabledReason={editDisabledReason}
              deleteDisabledReason={deleteDisabledReason}
              selectedProfileButtonRef={selectedProfileButtonRef}
              profileSearchInputRef={profileSearchInputRef}
              onSelectProfile={handleSelectProfile}
              onOpenEditDialog={openEditDialog}
              onOpenDeleteDialog={openDeleteDialog}
              onOpenCreateDialog={openCreateDialog}
              onManageProfiles={handleManageProfiles}
            />
          </div>

          {isProfileScopedPage && hasMismatch ? (
            <div className="inline-flex max-w-full items-start gap-1.5 rounded-md border border-warning/35 bg-warning/10 px-2.5 py-1.5 text-xs leading-4 text-warning-foreground">
              <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-warning-foreground/80" />
              <span>{messages.shell.mismatchWarning(selectedProfileName, activeProfileName)}</span>
            </div>
          ) : null}

          {hasMismatch ? (
            <Button size="sm" className="shrink-0" onClick={openActivateDialog} disabled={isActivating}>
              {isActivating ? (
                <>
                  <Loader2 data-icon="inline-start" className="animate-spin" />
                  {messages.shell.activating}
                </>
              ) : (
                <>
                  <span className="sm:hidden">{messages.shell.activate}</span>
                  <span className="hidden sm:inline">{messages.shell.activateProfile}</span>
                </>
              )}
            </Button>
          ) : null}

          {authEnabled ? (
            <Button variant="outline" size="sm" className="shrink-0" onClick={() => void handleLogout()}>
              <LogOut data-icon="inline-start" />
              <span className="hidden sm:inline">{username || messages.shell.signOut}</span>
              <span className="sm:hidden">{messages.shell.out}</span>
            </Button>
          ) : null}

          <GlobalPreferencesControls className="shrink-0" />
        </div>
      </div>
    </header>
  );
}
