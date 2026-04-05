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
import type { HeaderBlocklistRule } from "@/lib/types";

interface DeleteRuleConfirmDialogProps {
  deleteRuleConfirm: HeaderBlocklistRule | null;
  displayedDeleteRuleConfirm?: HeaderBlocklistRule | null;
  open?: boolean;
  setDeleteRuleConfirm: (rule: HeaderBlocklistRule | null) => void;
  handleDeleteRule: () => Promise<void>;
}

export function DeleteRuleConfirmDialog({
  deleteRuleConfirm,
  displayedDeleteRuleConfirm,
  open,
  setDeleteRuleConfirm,
  handleDeleteRule,
}: DeleteRuleConfirmDialogProps) {
  const { messages } = useLocale();
  const copy = messages.settingsDialogs;
  const dialogRule = displayedDeleteRuleConfirm ?? deleteRuleConfirm;
  const dialogOpen = open ?? Boolean(deleteRuleConfirm);
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={(open) => {
        if (!open) {
          setDeleteRuleConfirm(null);
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{copy.deleteRuleTitle}</DialogTitle>
          <DialogDescription>
            {copy.deleteRuleDescription(dialogRule?.name ?? "")}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDeleteRuleConfirm(null)}>
            {copy.cancel}
          </Button>
          <Button variant="destructive" onClick={() => void handleDeleteRule()}>
            {copy.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
