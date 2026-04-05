import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
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
  return (
    <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{copy.deleteModel}</DialogTitle>
          <DialogDescription>{copy.deleteModelDescription(deleteTarget?.display_name || deleteTarget?.model_id || "")}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDeleteTarget(null)}>{messages.settingsDialogs.cancel}</Button>
          <Button variant="destructive" onClick={onDelete}>{messages.settingsDialogs.delete}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
