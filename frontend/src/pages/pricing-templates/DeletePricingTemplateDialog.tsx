import { useEffect, useState } from "react";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { PricingTemplate, PricingTemplateConnectionUsageItem } from "@/lib/types";

interface DeletePricingTemplateDialogProps {
  deletePricingTemplateConfirm: PricingTemplate | null;
  deletePricingTemplateConflict: PricingTemplateConnectionUsageItem[] | null;
  onClose: () => void;
  onDelete: () => Promise<void>;
  pricingTemplateDeleting: boolean;
  pricingTemplateUsageLoading: boolean;
  pricingTemplateUsageRows: PricingTemplateConnectionUsageItem[];
}

export function DeletePricingTemplateDialog({
  deletePricingTemplateConfirm,
  deletePricingTemplateConflict,
  onClose,
  onDelete,
  pricingTemplateDeleting,
  pricingTemplateUsageLoading,
  pricingTemplateUsageRows,
}: DeletePricingTemplateDialogProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const [displayTemplate, setDisplayTemplate] = useState<PricingTemplate | null>(deletePricingTemplateConfirm);

  useEffect(() => {
    if (deletePricingTemplateConfirm) {
      setDisplayTemplate(deletePricingTemplateConfirm);
    }
  }, [deletePricingTemplateConfirm]);

  const dialogTemplate = deletePricingTemplateConfirm ?? displayTemplate;

  return (
    <Dialog
      open={deletePricingTemplateConfirm !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{copy.deletePricingTemplate}</DialogTitle>
          <DialogDescription>{copy.deletePricingTemplateDescription(dialogTemplate?.name ?? "")}</DialogDescription>
        </DialogHeader>

        {pricingTemplateUsageLoading ? (
          <div className="py-4">
            <div className="h-10 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : deletePricingTemplateConflict ? (
          <div className="space-y-4 py-4">
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {copy.deletePricingTemplateInUse(formatNumber(deletePricingTemplateConflict.length))}
            </div>
            <div className="max-h-[200px] overflow-y-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{copy.model}</TableHead>
                    <TableHead>{copy.endpoint}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {deletePricingTemplateConflict.map((row) => (
                    <TableRow key={row.connection_id}>
                      <TableCell className="font-medium">{row.model_id}</TableCell>
                      <TableCell>{row.endpoint_name}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>
        ) : pricingTemplateUsageRows.length > 0 ? (
          <div className="space-y-4 py-4">
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {copy.deletePricingTemplateInUse(formatNumber(pricingTemplateUsageRows.length))}
            </div>
          </div>
        ) : (
          <div className="py-4">
            <p className="text-sm">{messages.vendorManagement.thisActionCannotBeUndone}</p>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            variant="destructive"
            onClick={() => void onDelete()}
            disabled={
              pricingTemplateDeleting ||
              pricingTemplateUsageLoading ||
              (deletePricingTemplateConflict !== null && deletePricingTemplateConflict.length > 0) ||
              pricingTemplateUsageRows.length > 0
            }
          >
            {pricingTemplateDeleting ? messages.settingsDialogs.deleting : messages.settingsDialogs.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
