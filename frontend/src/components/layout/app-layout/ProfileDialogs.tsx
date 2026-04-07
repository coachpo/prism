import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { useLocale } from "@/i18n/useLocale";

interface ProfileDialogsProps {
  activateOpen: boolean;
  setActivateOpen: (open: boolean) => void;
  createOpen: boolean;
  setCreateOpen: (open: boolean) => void;
  editOpen: boolean;
  setEditOpen: (open: boolean) => void;
  deleteOpen: boolean;
  setDeleteOpen: (open: boolean) => void;
  selectedProfileName: string;
  activeProfileName: string;
  hasMismatch: boolean;
  isActivating: boolean;
  onActivate: () => void;
  nameInput: string;
  setNameInput: (value: string) => void;
  descriptionInput: string;
  setDescriptionInput: (value: string) => void;
  isSaving: boolean;
  canCreateProfile: boolean;
  hasSelectedProfile: boolean;
  onCreate: () => void;
  onEdit: () => void;
  deleteError: string | null;
  deleteConfirmTarget: string;
  deleteConfirmInput: string;
  setDeleteConfirmInput: (value: string) => void;
  isDeleteConfirmMatch: boolean;
  isDeleting: boolean;
  onDelete: () => void;
  clearDeleteError: () => void;
}

export function ProfileDialogs({
  activateOpen,
  setActivateOpen,
  createOpen,
  setCreateOpen,
  editOpen,
  setEditOpen,
  deleteOpen,
  setDeleteOpen,
  selectedProfileName,
  activeProfileName,
  hasMismatch,
  isActivating,
  onActivate,
  nameInput,
  setNameInput,
  descriptionInput,
  setDescriptionInput,
  isSaving,
  canCreateProfile,
  hasSelectedProfile,
  onCreate,
  onEdit,
  deleteError,
  deleteConfirmTarget,
  deleteConfirmInput,
  setDeleteConfirmInput,
  isDeleteConfirmMatch,
  isDeleting,
  onDelete,
  clearDeleteError,
}: ProfileDialogsProps) {
  const { messages } = useLocale();

  return (
    <>
      <Dialog open={activateOpen} onOpenChange={setActivateOpen}>
        <DialogContent className="sm:max-w-md" showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>{messages.profiles.activateTitle(selectedProfileName)}</DialogTitle>
            <DialogDescription>{messages.profiles.activateDescription}</DialogDescription>
          </DialogHeader>
          <DialogBody>
            <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-sm text-muted-foreground">{messages.profiles.currentActive}</p>
                  <p className="font-medium text-foreground">{activeProfileName}</p>
                </div>
                <Badge variant="secondary">
                  {messages.profiles.active}
                </Badge>
              </div>
              <Separator />
              <div className="flex min-w-0 flex-col gap-1">
                <p className="text-sm text-muted-foreground">{messages.profiles.newActive}</p>
                <p className="font-medium text-foreground">{selectedProfileName}</p>
              </div>
            </div>
          </DialogBody>
          <DialogFooter className="sm:justify-between">
            <Button variant="outline" onClick={() => setActivateOpen(false)}>
              {messages.profiles.cancel}
            </Button>
            <Button onClick={onActivate} disabled={isActivating || !hasMismatch}>
              {isActivating ? messages.profiles.activating : messages.profiles.activate}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{messages.profiles.createTitle}</DialogTitle>
            <DialogDescription>{messages.profiles.createDescription}</DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-5"
            onSubmit={(event) => {
              event.preventDefault();
              onCreate();
            }}
          >
            <DialogBody>
              <div className="flex flex-col gap-2">
                <Label htmlFor="profile-create-name">{messages.profiles.name}</Label>
                <Input
                  id="profile-create-name"
                  name="profile_name"
                  autoComplete="off"
                  value={nameInput}
                  onChange={(event) => setNameInput(event.target.value)}
                  placeholder={messages.profiles.profileNamePlaceholder}
                />
              </div>
              <div className="flex flex-col gap-2 rounded-lg border bg-muted/20 p-4">
                <Label htmlFor="profile-create-description">{messages.profiles.descriptionOptional}</Label>
                <Input
                  id="profile-create-description"
                  name="profile_description"
                  autoComplete="off"
                  value={descriptionInput}
                  onChange={(event) => setDescriptionInput(event.target.value)}
                  placeholder={messages.profiles.optionalPlaceholder}
                />
              </div>
            </DialogBody>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>
                {messages.profiles.cancel}
              </Button>
              <Button type="submit" disabled={isSaving || !canCreateProfile}>
                {isSaving ? messages.profiles.creating : messages.profiles.create}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={editOpen} onOpenChange={setEditOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{messages.profiles.editTitle}</DialogTitle>
            <DialogDescription>{messages.profiles.editDescription}</DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-5"
            onSubmit={(event) => {
              event.preventDefault();
              onEdit();
            }}
          >
            <DialogBody>
              <div className="flex flex-col gap-2">
                <Label htmlFor="profile-edit-name">{messages.profiles.name}</Label>
                <Input
                  id="profile-edit-name"
                  name="profile_name"
                  autoComplete="off"
                  value={nameInput}
                  onChange={(event) => setNameInput(event.target.value)}
                  placeholder={messages.profiles.profileNamePlaceholder}
                />
              </div>
              <div className="flex flex-col gap-2 rounded-lg border bg-muted/20 p-4">
                <Label htmlFor="profile-edit-description">{messages.profiles.descriptionOptional}</Label>
                <Input
                  id="profile-edit-description"
                  name="profile_description"
                  autoComplete="off"
                  value={descriptionInput}
                  onChange={(event) => setDescriptionInput(event.target.value)}
                  placeholder={messages.profiles.optionalPlaceholder}
                />
              </div>
            </DialogBody>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditOpen(false)}>
                {messages.profiles.cancel}
              </Button>
              <Button type="submit" disabled={isSaving || !hasSelectedProfile}>
                {isSaving ? messages.profiles.saving : messages.profiles.save}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent className="sm:max-w-md" showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>{messages.profiles.deleteTitle}</DialogTitle>
            <DialogDescription>{messages.profiles.deleteDescription(selectedProfileName)}</DialogDescription>
          </DialogHeader>
          <DialogBody>
            <div className="flex flex-col gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4">
              <div className="flex min-w-0 flex-col gap-1">
                <p className="text-sm font-medium text-foreground">{selectedProfileName}</p>
                <p className="text-sm text-muted-foreground">{messages.profiles.typeToConfirm(deleteConfirmTarget)}</p>
              </div>
              <Separator />
              <code className="inline-flex w-fit items-center rounded-md border bg-background px-2.5 py-1.5 text-sm font-medium text-foreground">
                {deleteConfirmTarget}
              </code>
            </div>
            {deleteError ? (
              <p className="rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {deleteError}
              </p>
            ) : null}
            <div className="flex flex-col gap-2">
              <Label htmlFor="profile-delete-confirm">
                {messages.profiles.typeToConfirm(deleteConfirmTarget)}
              </Label>
              <Input
                id="profile-delete-confirm"
                name="profile_delete_confirm"
                autoComplete="off"
                value={deleteConfirmInput}
                onChange={(event) => {
                  setDeleteConfirmInput(event.target.value);
                  if (deleteError) clearDeleteError();
                }}
              />
            </div>
          </DialogBody>
          <DialogFooter className="sm:justify-between">
            <Button variant="outline" onClick={() => setDeleteOpen(false)}>
              {messages.profiles.cancel}
            </Button>
            <Button
              variant="destructive"
              onClick={onDelete}
              disabled={isDeleting || !hasSelectedProfile || !isDeleteConfirmMatch}
            >
              {isDeleting ? messages.profiles.deleting : messages.profiles.delete}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
