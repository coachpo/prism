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
import type { ModelConfigListItem } from "@/lib/types";

type Props = {
  deleteTarget: ModelConfigListItem | null;
  onDelete: () => void;
  setDeleteTarget: (model: ModelConfigListItem | null) => void;
};

export function DeleteModelDialog({ deleteTarget, onDelete, setDeleteTarget }: Props) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;
  const fieldCopy = messages.common;
  const displayName = deleteTarget?.display_name?.trim() || deleteTarget?.model_id || "";

  return (
    <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.deleteModel}</DialogTitle>
          <DialogDescription>{copy.deleteModelDescription(deleteTarget?.display_name || deleteTarget?.model_id || "")}</DialogDescription>
        </DialogHeader>

        <DialogBody>
          {deleteTarget ? (
            <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
              <div className="flex flex-col gap-2">
                <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate text-sm font-medium text-foreground">{displayName}</p>
                  <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                    {deleteTarget.model_id}
                  </code>
                </div>
              </div>

              <div className="grid gap-3 sm:grid-cols-3">
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{fieldCopy.vendor}</p>
                  <p className="truncate text-sm text-foreground">{deleteTarget.vendor?.name ?? copy.unknownVendor}</p>
                </div>
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{fieldCopy.apiFamily}</p>
                  <p className="truncate text-sm text-foreground">{deleteTarget.api_family}</p>
                </div>
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Access targets</p>
                  <p className="truncate text-sm text-foreground">{deleteTarget.access_targets.length}</p>
                </div>
              </div>
            </div>
          ) : null}
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          <Button variant="outline" onClick={() => setDeleteTarget(null)}>{messages.settingsDialogs.cancel}</Button>
          <Button variant="destructive" onClick={onDelete}>{messages.settingsDialogs.delete}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
