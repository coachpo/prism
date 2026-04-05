import { Button } from "@/components/ui/button";
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
import { useLocale } from "@/i18n/useLocale";
import type { Vendor, VendorModelUsageItem } from "@/lib/types";
import { formatApiFamily } from "@/lib/utils";

interface DeleteVendorDialogProps {
  deleteVendorConfirm: Vendor | null;
  deleteVendorConflict: VendorModelUsageItem[] | null;
  displayedDeleteVendorConfirm?: Vendor | null;
  onClose: () => void;
  onDelete: () => Promise<void>;
  open?: boolean;
  vendorDeleting: boolean;
  vendorUsageLoading: boolean;
  vendorUsageRows: VendorModelUsageItem[];
}

export function DeleteVendorDialog({
  deleteVendorConfirm,
  deleteVendorConflict,
  displayedDeleteVendorConfirm,
  onClose,
  onDelete,
  open,
  vendorDeleting,
  vendorUsageLoading,
  vendorUsageRows,
}: DeleteVendorDialogProps) {
  const { messages } = useLocale();
  const dialogVendor = displayedDeleteVendorConfirm ?? deleteVendorConfirm;
  const dialogOpen = open ?? deleteVendorConfirm !== null;
  const modelTypeLabel = (modelType: string) =>
    modelType === "proxy" ? messages.modelDetail.typeProxy : messages.modelDetail.typeNative;
  const blockedRows = deleteVendorConflict?.length ? deleteVendorConflict : vendorUsageRows;
  const isBlocked = blockedRows.length > 0;

  return (
    <Dialog open={dialogOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{messages.vendorManagement.deleteTitle}</DialogTitle>
          <DialogDescription>
            {messages.vendorManagement.deleteDescription(dialogVendor?.name ?? "")}
          </DialogDescription>
        </DialogHeader>

        {vendorUsageLoading ? (
          <div className="py-4">
            <div className="h-10 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : isBlocked ? (
          <div className="space-y-4 py-4">
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {messages.vendorManagement.deleteInUse(String(blockedRows.length))}
            </div>

            <div className="max-h-[220px] overflow-y-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{messages.vendorManagement.dependencyProfile}</TableHead>
                    <TableHead>{messages.vendorManagement.dependencyModelId}</TableHead>
                    <TableHead>{messages.vendorManagement.dependencyApiFamily}</TableHead>
                    <TableHead>{messages.vendorManagement.dependencyModelType}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {blockedRows.map((row) => (
                    <TableRow key={`${row.model_config_id}-${row.profile_id}`}>
                      <TableCell>{row.profile_name}</TableCell>
                      <TableCell className="font-medium">{row.model_id}</TableCell>
                      <TableCell>{formatApiFamily(row.api_family)}</TableCell>
                      <TableCell>{modelTypeLabel(row.model_type)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>
        ) : (
          <div className="py-4">
            <p className="text-sm">{messages.vendorManagement.thisActionCannotBeUndone}</p>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {messages.vendorManagement.cancel}
          </Button>
          <Button
            variant="destructive"
            onClick={() => void onDelete()}
            disabled={vendorDeleting || vendorUsageLoading || isBlocked}
          >
            {vendorDeleting ? messages.vendorManagement.saving : messages.vendorManagement.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
