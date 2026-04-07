import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
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
import type { FormEvent } from "react";
import {
  getCleanupRetentionLabel,
  getCleanupTypeLabel,
  type DeleteCleanupType,
} from "../settingsPageHelpers";

interface DeleteConfirmDialogProps {
  deleteConfirm: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null;
  displayedDeleteConfirm?: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null;
  open?: boolean;
  setDeleteConfirm: (confirm: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null) => void;
  selectedProfileLabel: string;
  deleteConfirmPhrase: string;
  setDeleteConfirmPhrase: (phrase: string) => void;
  handleBatchDelete: () => Promise<void>;
  deleting: boolean;
  isDeletePhraseValid: boolean;
}

export function DeleteConfirmDialog({
  deleteConfirm,
  displayedDeleteConfirm,
  open,
  setDeleteConfirm,
  selectedProfileLabel,
  deleteConfirmPhrase,
  setDeleteConfirmPhrase,
  handleBatchDelete,
  deleting,
  isDeletePhraseValid,
}: DeleteConfirmDialogProps) {
  const { messages } = useLocale();
  const copy = messages.settingsDialogs;
  const dialogConfirm = displayedDeleteConfirm ?? deleteConfirm;
  const dialogOpen = open ?? Boolean(deleteConfirm);
  const cleanupTypeLabel = dialogConfirm ? getCleanupTypeLabel(dialogConfirm.type) : "-";
  const retentionLabel = dialogConfirm
    ? getCleanupRetentionLabel(dialogConfirm.deleteAll, dialogConfirm.days)
    : "-";

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void handleBatchDelete();
  };

  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={(open) => {
        if (!open) {
          setDeleteConfirm(null);
          setDeleteConfirmPhrase("");
        }
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <form onSubmit={handleSubmit} className="flex flex-col gap-5">
          <DialogHeader>
            <DialogTitle>{copy.deleteConfirmTitle}</DialogTitle>
            <DialogDescription>{copy.deleteConfirmDescription(selectedProfileLabel)}</DialogDescription>
          </DialogHeader>

          <DialogBody>
            <div className="flex flex-col gap-3 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
              <p className="text-sm font-medium text-foreground">{copy.deletionSummary}</p>
              <dl className="grid gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1">
                  <dt className="text-xs font-medium text-muted-foreground">{copy.dataType}</dt>
                  <dd className="text-sm font-medium text-foreground">{cleanupTypeLabel}</dd>
                </div>
                <div className="flex flex-col gap-1">
                  <dt className="text-xs font-medium text-muted-foreground">{copy.retention}</dt>
                  <dd className="text-sm font-medium text-foreground">{retentionLabel}</dd>
                </div>
              </dl>
            </div>

            <div className="flex flex-col gap-3 rounded-lg border p-4">
              <div className="flex flex-col gap-2">
                <Label htmlFor="delete-confirm-phrase">{copy.typeDeleteToProceed(copy.deleteConfirmKeyword)}</Label>
                <code className="inline-flex w-fit items-center rounded-md border bg-muted px-2.5 py-1.5 text-sm font-medium text-foreground">
                  {copy.deleteConfirmKeyword}
                </code>
              </div>

              <Input
                id="delete-confirm-phrase"
                name="delete_confirm_phrase"
                autoComplete="off"
                value={deleteConfirmPhrase}
                onChange={(event) => setDeleteConfirmPhrase(event.target.value)}
                placeholder={copy.deleteConfirmKeyword}
              />
            </div>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setDeleteConfirm(null);
                setDeleteConfirmPhrase("");
              }}
            >
              {copy.cancel}
            </Button>
            <Button type="submit" variant="destructive" disabled={deleting || !isDeletePhraseValid}>
              {deleting ? copy.deleting : copy.delete}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
