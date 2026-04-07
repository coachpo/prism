import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
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
import { Separator } from "@/components/ui/separator";
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
  const matchTypeLabel = dialogRule?.match_type === "prefix" ? copy.prefixMatch : copy.exactMatch;

  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={(open) => {
        if (!open) {
          setDeleteRuleConfirm(null);
        }
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.deleteRuleTitle}</DialogTitle>
          <DialogDescription>
            {copy.deleteRuleDescription(dialogRule?.name ?? "")}
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
            <div className="flex items-start justify-between gap-3">
              <div className="flex min-w-0 flex-col gap-1">
                <p className="truncate text-sm font-medium text-foreground">{dialogRule?.name ?? ""}</p>
                <p className="text-sm text-muted-foreground">{copy.pattern}</p>
              </div>
              {dialogRule ? <Badge variant="outline">{matchTypeLabel}</Badge> : null}
            </div>
            <Separator />
            <code className="overflow-x-auto rounded-md border bg-background px-3 py-2 text-sm font-medium text-foreground">
              {dialogRule?.pattern ?? ""}
            </code>
          </div>
        </DialogBody>

        <DialogFooter className="sm:justify-between">
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
