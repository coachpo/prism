import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useAuth } from "@/context/useAuth";
import { useProfileContext } from "@/context/ProfileContext";
import { useLocale } from "@/i18n/useLocale";
import { readSidebarCollapsed, writeSidebarCollapsed } from "./sidebarPersistence";
import { useShellNavigation } from "./useShellNavigation";
import { useProfileDialogState } from "./useProfileDialogState";
import { useProfileSwitcherState } from "./useProfileSwitcherState";

export function useAppLayoutState() {
  const navigate = useNavigate();
  const { authEnabled, username, logout } = useAuth();
  const { messages } = useLocale();
  const shellNavigation = useShellNavigation();
  const {
    profiles,
    activeProfile,
    maxProfiles,
    selectedProfile,
    selectedProfileId,
    selectProfile,
    createProfile,
    updateProfile,
    activateProfile,
    deleteProfile,
  } = useProfileContext();

  const [desktopSidebarCollapsed, setDesktopSidebarCollapsed] = useState(() => readSidebarCollapsed());

  const canCreateProfile = profiles.length < maxProfiles;
  const selectedIsActive =
    selectedProfile !== null && activeProfile !== null && selectedProfile.id === activeProfile.id;
  const hasMismatch = selectedProfile !== null && activeProfile !== null && !selectedIsActive;
  const selectedIsDefault = selectedProfile?.is_default ?? false;
  const selectedIsEditable = selectedProfile?.is_editable ?? true;
  const selectedProfileName = selectedProfile?.name ?? messages.common.unavailable;
  const activeProfileName = activeProfile?.name ?? messages.common.unavailable;
  const deleteDisabledReason = selectedIsDefault
    ? messages.profiles.defaultProfileDeleteDisabled
    : selectedIsActive
      ? messages.profiles.activeProfileDeleteDisabled
      : !selectedProfile
        ? messages.profiles.selectProfileToDelete
        : null;

  const editDisabledReason = !selectedProfile
    ? messages.profiles.selectProfileToEdit
    : !selectedIsEditable
      ? messages.profiles.lockedProfileEditDisabled
      : null;

  const isProfileScopedPage = shellNavigation.isProfileScopedPage;

  const switcherState = useProfileSwitcherState({
    profiles,
    selectedProfileId,
    selectProfile,
  });

  const dialogState = useProfileDialogState({
    activateProfile,
    canCreateProfile,
    closeProfileSwitcher: switcherState.closeProfileSwitcher,
    createProfile,
    deleteProfile,
    hasMismatch,
    selectProfile,
    selectedIsActive,
    selectedIsDefault,
    selectedProfile,
    updateProfile,
  });

  useEffect(() => {
    writeSidebarCollapsed(desktopSidebarCollapsed);
  }, [desktopSidebarCollapsed]);

  const setDesktopSidebarOpen = (open: boolean) => {
    if (open) {
      switcherState.closeProfileSwitcher();
    }

    setDesktopSidebarCollapsed(!open);
  };

  const toggleDesktopSidebar = () => {
    setDesktopSidebarCollapsed((current) => !current);
  };

  const handleManageProfiles = () => {
    switcherState.closeProfileSwitcher();
    navigate("/settings");
  };

  const handleLogout = async () => {
    try {
      await logout();
      navigate("/login", { replace: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.shell.logoutFailed);
    }
  };

  return {
    activeProfileName,
    authEnabled,
    breadcrumbs: shellNavigation.breadcrumbs,
    canCreateProfile,
    desktopSidebarCollapsed,
    deleteDisabledReason,
    editDisabledReason,
    handleLogout,
    handleManageProfiles,
    hasMismatch,
    isProfileScopedPage,
    selectedIsActive,
    selectedProfile,
    selectedProfileId,
    selectedProfileName,
    setDesktopSidebarOpen,
    sidebarItems: shellNavigation.sidebarItems,
    toggleDesktopSidebar,
    username,
    ...switcherState,
    ...dialogState,
  };
}
