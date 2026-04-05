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

  return (
    <header className="shell-header sticky top-0 z-30 border-b bg-background/95 backdrop-blur-sm">
      <div className="mx-auto flex w-full max-w-screen-xl flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 items-center gap-2.5">
          <SidebarTrigger
            aria-label={messages.shell.openSidebar}
            title={messages.shell.openSidebar}
            className="-ml-1 lg:hidden"
          />
          <Separator orientation="vertical" className="mr-0.5 hidden h-4 md:block lg:hidden" />
          <Breadcrumb data-testid="shell-breadcrumb" className="min-w-0">
            <BreadcrumbList>
              {breadcrumbs.map((breadcrumb, index) => {
                const item = breadcrumb.current ? (
                  <BreadcrumbPage data-testid="shell-breadcrumb-current">
                    {breadcrumb.label}
                  </BreadcrumbPage>
                ) : breadcrumb.href ? (
                  <BreadcrumbLink asChild>
                    <Link to={breadcrumb.href}>{breadcrumb.label}</Link>
                  </BreadcrumbLink>
                ) : (
                  <span>{breadcrumb.label}</span>
                );

                return (
                  <Fragment key={breadcrumb.id}>
                    <BreadcrumbItem className="max-w-full truncate">{item}</BreadcrumbItem>
                    {index < breadcrumbs.length - 1 ? <BreadcrumbSeparator /> : null}
                  </Fragment>
                );
              })}
            </BreadcrumbList>
          </Breadcrumb>
        </div>

        <div className="flex w-full flex-col items-stretch gap-2 lg:w-auto lg:flex-row lg:flex-wrap lg:items-center lg:justify-end">
          {isProfileScopedPage && hasMismatch ? (
            <div className="max-w-[320px] rounded-md border border-amber-500/30 bg-amber-500/10 px-2.5 py-2 text-xs text-amber-800 dark:text-amber-200">
              <p className="inline-flex items-start gap-1.5">
                <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                <span>{messages.shell.mismatchWarning(selectedProfileName, activeProfileName)}</span>
              </p>
            </div>
          ) : null}

          <div className="flex w-full flex-col items-stretch gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-end lg:w-auto">
            <div
              data-testid="shell-profile-switcher"
              className="flex w-full flex-col items-stretch sm:w-auto sm:flex-row sm:items-center sm:justify-end"
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

            {hasMismatch ? (
              <Button className="h-9 px-3" onClick={openActivateDialog} disabled={isActivating}>
                {isActivating ? (
                  <>
                    <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
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
              <Button variant="outline" className="h-9 px-3" onClick={() => void handleLogout()}>
                <LogOut className="mr-2 h-3.5 w-3.5" />
                <span className="hidden sm:inline">{username || messages.shell.signOut}</span>
                <span className="sm:hidden">{messages.shell.out}</span>
              </Button>
            ) : null}

            <GlobalPreferencesControls />
          </div>
        </div>
      </div>
    </header>
  );
}
