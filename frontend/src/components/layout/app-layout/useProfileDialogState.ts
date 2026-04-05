import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { useLocale } from "@/i18n/useLocale";
import type { Profile } from "@/lib/types";
import { parseConflictMessage } from "./profileConflictMessageParser";

interface UseProfileDialogStateInput {
  activateProfile: (id: number) => Promise<Profile>;
  canCreateProfile: boolean;
  closeProfileSwitcher: () => void;
  createProfile: (data: { name: string; description: string | null }) => Promise<Profile>;
  deleteProfile: (id: number) => Promise<void>;
  hasMismatch: boolean;
  selectProfile: (profileId: number) => void;
  selectedIsActive: boolean;
  selectedIsDefault: boolean;
  selectedProfile: Profile | null;
  updateProfile: (id: number, data: { name: string; description: string | null }) => Promise<Profile>;
}

export function useProfileDialogState({
  activateProfile,
  canCreateProfile,
  closeProfileSwitcher,
  createProfile,
  deleteProfile,
  hasMismatch,
  selectProfile,
  selectedIsActive,
  selectedIsDefault,
  selectedProfile,
  updateProfile,
}: UseProfileDialogStateInput) {
  const { messages } = useLocale();
  const [createOpen, setCreateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [activateOpen, setActivateOpen] = useState(false);
  const [deleteConfirmInput, setDeleteConfirmInput] = useState("");
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [nameInput, setNameInput] = useState("");
  const [descriptionInput, setDescriptionInput] = useState("");
  const [isSaving, setIsSaving] = useState(false);
  const [isActivating, setIsActivating] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const deleteConfirmTarget = useMemo(
    () => messages.profiles.deleteConfirmPhrase(selectedProfile?.name ?? "").trim().toLowerCase(),
    [messages.profiles, selectedProfile?.name]
  );
  const isDeleteConfirmMatch = deleteConfirmInput.trim().toLowerCase() === deleteConfirmTarget;

  useEffect(() => {
    if (!editOpen || !selectedProfile) return;
    setNameInput(selectedProfile.name);
    setDescriptionInput(selectedProfile.description ?? "");
  }, [editOpen, selectedProfile]);

  useEffect(() => {
    if (activateOpen && !hasMismatch) {
      setActivateOpen(false);
    }
  }, [activateOpen, hasMismatch]);

  const resetFormFields = () => {
    setNameInput("");
    setDescriptionInput("");
  };

  const openCreateDialog = () => {
    resetFormFields();
    closeProfileSwitcher();
    setCreateOpen(true);
  };

  const openEditDialog = () => {
    if (!selectedProfile) return;
    closeProfileSwitcher();
    setEditOpen(true);
  };

  const openDeleteDialog = () => {
    if (!selectedProfile || selectedIsActive || selectedIsDefault) return;
    closeProfileSwitcher();
    setDeleteConfirmInput("");
    setDeleteError(null);
    setDeleteOpen(true);
  };

  const openActivateDialog = () => {
    if (!hasMismatch) return;
    closeProfileSwitcher();
    setActivateOpen(true);
  };

  const handleCreateProfile = async () => {
    const name = nameInput.trim();
    if (!name) {
      toast.error(messages.profiles.nameRequired);
      return;
    }
    if (!canCreateProfile) {
      toast.error(messages.profiles.limitReached);
      return;
    }

    setIsSaving(true);
    try {
      const created = await createProfile({
        name,
        description: descriptionInput.trim() || null,
      });
      selectProfile(created.id);
      toast.success(messages.profiles.createdProfile(created.name));
      setCreateOpen(false);
      resetFormFields();
    } catch (error) {
      const conflictMessage = parseConflictMessage(error, messages.profiles.limitReached);
      if (conflictMessage) {
        toast.error(conflictMessage);
      } else {
        toast.error(error instanceof Error ? error.message : messages.profiles.createFailed);
      }
    } finally {
      setIsSaving(false);
    }
  };

  const handleEditProfile = async () => {
    if (!selectedProfile) return;

    const name = nameInput.trim();
    if (!name) {
      toast.error(messages.profiles.nameRequired);
      return;
    }

    setIsSaving(true);
    try {
      await updateProfile(selectedProfile.id, {
        name,
        description: descriptionInput.trim() || null,
      });
      toast.success(messages.profiles.updatedProfile);
      setEditOpen(false);
      resetFormFields();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.profiles.updateFailed);
    } finally {
      setIsSaving(false);
    }
  };

  const handleActivateProfile = async () => {
    if (!selectedProfile || selectedIsActive) return;

    setIsActivating(true);
    try {
      await activateProfile(selectedProfile.id);
      toast.success(messages.profiles.activatedProfile(selectedProfile.name));
      setActivateOpen(false);
    } catch (error) {
      const message = error instanceof Error ? error.message : messages.profiles.activateFailed;
      if (message.includes("409") || message.toLowerCase().includes("conflict")) {
        toast.error(messages.profiles.activateConflict);
      } else {
        toast.error(message);
      }
    } finally {
      setIsActivating(false);
    }
  };

  const handleDeleteProfile = async () => {
    if (!selectedProfile || !isDeleteConfirmMatch) return;

    setDeleteError(null);
    setIsDeleting(true);
    try {
      await deleteProfile(selectedProfile.id);
      toast.success(messages.profiles.deletedProfile(selectedProfile.name));
      setDeleteOpen(false);
      setDeleteConfirmInput("");
      setDeleteError(null);
    } catch (error) {
      setDeleteError(error instanceof Error ? error.message : messages.profiles.deleteFailed);
    } finally {
      setIsDeleting(false);
    }
  };

  return {
    activateOpen,
    createOpen,
    deleteConfirmInput,
    deleteConfirmTarget,
    deleteError,
    deleteOpen,
    descriptionInput,
    editOpen,
    handleActivateProfile,
    handleCreateProfile,
    handleDeleteProfile,
    handleEditProfile,
    isActivating,
    isDeleteConfirmMatch,
    isDeleting,
    isSaving,
    nameInput,
    openActivateDialog,
    openCreateDialog,
    openDeleteDialog,
    openEditDialog,
    setActivateOpen,
    setCreateOpen,
    setDeleteConfirmInput,
    setDeleteError,
    setDeleteOpen,
    setDescriptionInput,
    setEditOpen,
    setNameInput,
  };
}
