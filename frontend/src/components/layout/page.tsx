import type { CSSProperties, ReactNode } from "react";
import { Outlet } from "react-router-dom";
import { useLocale } from "@/i18n/useLocale";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { AppSidebar } from "./app-layout/AppSidebar";
import { ProfileDialogs } from "./app-layout/ProfileDialogs";
import { SiteHeader } from "./app-layout/SiteHeader";
import { useAppLayoutState } from "./app-layout/useAppLayoutState";

export function Page({ children }: { children?: ReactNode }) {
  const state = useAppLayoutState();
  const { messages } = useLocale();

  return (
    <>
      <SidebarProvider
        open={!state.desktopSidebarCollapsed}
        onOpenChange={state.setDesktopSidebarOpen}
        style={{ "--sidebar-width": "20rem" } as CSSProperties}
      >
        <a
          href="#prism-main-content"
          className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground shadow-operator-panel focus:not-sr-only focus:absolute focus:left-4 focus:top-4"
        >
          {messages.shell.skipToMainContent}
        </a>
        <AppSidebar
          activeProfileId={state.activeProfileId}
          activeProfileName={state.activeProfileName}
          authEnabled={state.authEnabled}
          canCreateProfile={state.canCreateProfile}
          closeProfileSwitcher={state.closeProfileSwitcher}
          deleteDisabledReason={state.deleteDisabledReason}
          editDisabledReason={state.editDisabledReason}
          handleLogout={state.handleLogout}
          handleManageProfiles={state.handleManageProfiles}
          handleSelectProfile={state.handleSelectProfile}
          hasMismatch={state.hasMismatch}
          hasNoProfiles={state.hasNoProfiles}
          isActivating={state.isActivating}
          openActivateDialog={state.openActivateDialog}
          openCreateDialog={state.openCreateDialog}
          openDeleteDialog={state.openDeleteDialog}
          openEditDialog={state.openEditDialog}
          profileSwitcherOpen={state.profileSwitcherOpen}
          profiles={state.profiles}
          selectedIsActive={state.selectedIsActive}
          selectedProfileId={state.selectedProfileId}
          selectedProfileName={state.selectedProfileName}
          setProfileSwitcherOpen={state.setProfileSwitcherOpen}
          sidebarItems={state.sidebarItems}
          username={state.username}
        />

        <SidebarInset className="min-h-svh overflow-hidden">
          <SiteHeader breadcrumbs={state.breadcrumbs} scopeBadge={state.shellScopeBadge} />
          <div
            id="prism-main-content"
            tabIndex={-1}
            className="flex flex-1 flex-col gap-[var(--density-page-gap)] p-[var(--density-page-pad-y)] px-[var(--density-page-pad-x)] outline-none"
          >
            {children ?? <Outlet />}
          </div>
        </SidebarInset>
      </SidebarProvider>

      <ProfileDialogs
        activateOpen={state.activateOpen}
        setActivateOpen={state.setActivateOpen}
        createOpen={state.createOpen}
        setCreateOpen={state.setCreateOpen}
        editOpen={state.editOpen}
        setEditOpen={state.setEditOpen}
        deleteOpen={state.deleteOpen}
        setDeleteOpen={state.setDeleteOpen}
        selectedProfileName={state.selectedProfile?.name ?? messages.common.profileFallback}
        activeProfileName={state.activeProfileName}
        hasMismatch={state.hasMismatch}
        isActivating={state.isActivating}
        activationError={state.activationError}
        onActivate={state.handleActivateProfile}
        nameInput={state.nameInput}
        setNameInput={state.setNameInput}
        descriptionInput={state.descriptionInput}
        setDescriptionInput={state.setDescriptionInput}
        isSaving={state.isSaving}
        canCreateProfile={state.canCreateProfile}
        hasSelectedProfile={Boolean(state.selectedProfile)}
        onCreate={state.handleCreateProfile}
        onEdit={state.handleEditProfile}
        deleteError={state.deleteError}
        deleteConfirmTarget={state.deleteConfirmTarget}
        deleteConfirmInput={state.deleteConfirmInput}
        setDeleteConfirmInput={state.setDeleteConfirmInput}
        isDeleteConfirmMatch={state.isDeleteConfirmMatch}
        isDeleting={state.isDeleting}
        onDelete={state.handleDeleteProfile}
        clearDeleteError={() => state.setDeleteError(null)}
      />
    </>
  );
}
