import type { CSSProperties } from "react";
import { Outlet } from "react-router-dom";
import { useLocale } from "@/i18n/useLocale";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { ProfileDialogs } from "./app-layout/ProfileDialogs";
import { AppHeader } from "./app-layout/AppHeader";
import { AppSidebar } from "./app-layout/AppSidebar";
import { useAppLayoutState } from "./app-layout/useAppLayoutState";

export function AppLayout() {
  const state = useAppLayoutState();
  const { messages } = useLocale();

  return (
    <>
      <SidebarProvider
        open={!state.desktopSidebarCollapsed}
        onOpenChange={state.setDesktopSidebarOpen}
        style={
          {
            "--sidebar-width": "20rem",
            "--sidebar-width-icon": "4.5rem",
            "--sidebar-width-mobile": "min(88vw, 20rem)",
          } as CSSProperties
        }
      >
        <AppSidebar
          activeProfileName={state.activeProfileName}
          closeProfileSwitcher={state.closeProfileSwitcher}
          hasMismatch={state.hasMismatch}
          selectedProfileName={state.selectedProfileName}
          sidebarItems={state.sidebarItems}
        />

        <SidebarInset className="min-h-screen">
          <AppHeader
            activeProfileName={state.activeProfileName}
            authEnabled={state.authEnabled}
            breadcrumbs={state.breadcrumbs}
            canCreateProfile={state.canCreateProfile}
            deleteDisabledReason={state.deleteDisabledReason}
            editDisabledReason={state.editDisabledReason}
            filteredProfiles={state.filteredProfiles}
            handleLogout={state.handleLogout}
            handleManageProfiles={state.handleManageProfiles}
            handleSelectProfile={state.handleSelectProfile}
            hasMismatch={state.hasMismatch}
            hasNoMatches={state.hasNoMatches}
            hasNoProfiles={state.hasNoProfiles}
            isActivating={state.isActivating}
            isProfileScopedPage={state.isProfileScopedPage}
            openActivateDialog={state.openActivateDialog}
            openCreateDialog={state.openCreateDialog}
            openDeleteDialog={state.openDeleteDialog}
            openEditDialog={state.openEditDialog}
            profileQuery={state.profileQuery}
            profileSearchInputRef={state.profileSearchInputRef}
            profileSwitcherOpen={state.profileSwitcherOpen}
            selectedIsActive={state.selectedIsActive}
            selectedProfileButtonRef={state.selectedProfileButtonRef}
            selectedProfileId={state.selectedProfileId}
            selectedProfileName={state.selectedProfileName}
            setProfileQuery={state.setProfileQuery}
            setProfileSwitcherOpen={state.setProfileSwitcherOpen}
            username={state.username}
          />

          <main className="flex-1 overflow-y-auto scrollbar-thin">
            <div className="shell-page mx-auto max-w-screen-xl">
              <Outlet />
            </div>
          </main>
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
