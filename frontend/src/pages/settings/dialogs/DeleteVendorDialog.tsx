import { VendorIcon } from "@/components/VendorIcon";
import { Button } from "@/components/ui/button";
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
  const referencedRows = vendorUsageRows.length > 0 ? vendorUsageRows : (deleteVendorConflict ?? []);
  const hasReferences = referencedRows.length > 0;
  const isReadonlyVendor = dialogVendor?.is_readonly === true;

  return (
    <Dialog open={dialogOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[calc(100vh-2rem)] sm:max-w-3xl" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{messages.vendorManagement.deleteTitle}</DialogTitle>
          <DialogDescription>
            {messages.vendorManagement.deleteDescription(dialogVendor?.name ?? "")}
          </DialogDescription>
        </DialogHeader>

        <DialogBody className="min-h-0 flex-1 overflow-hidden">
          <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
            <div className="flex items-start gap-4">
              {dialogVendor ? <VendorIcon vendor={dialogVendor} size={40} className="rounded-lg" /> : null}
              <div className="flex min-w-0 flex-1 flex-col gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate text-sm font-medium text-foreground">{dialogVendor?.name ?? ""}</p>
                  {dialogVendor?.key ? (
                    <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                      {dialogVendor.key}
                    </code>
                  ) : null}
                </div>
                <p className="text-sm leading-6 text-muted-foreground">
                  {dialogVendor?.description || messages.vendorManagement.noDescription}
                </p>
              </div>
            </div>
            <Separator />
            <p className="text-sm text-muted-foreground">{messages.vendorManagement.thisActionCannotBeUndone}</p>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto pr-1">
            {vendorUsageLoading ? (
              <div className="flex flex-col gap-3 py-1">
                <div className="h-10 animate-pulse rounded-md bg-muted/50" />
                <div className="h-32 animate-pulse rounded-md bg-muted/35" />
              </div>
            ) : hasReferences ? (
              <div className="flex flex-col gap-4 py-1">
                <div className="rounded-lg border border-border/70 bg-muted/30 px-4 py-3 text-sm leading-6 text-muted-foreground">
                  {messages.vendorManagement.deleteInUse(String(referencedRows.length))}
                </div>

                <div className="overflow-hidden rounded-lg border">
                  <div className="max-h-[260px] overflow-y-auto">
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
                        {referencedRows.map((row) => (
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
              </div>
            ) : null}
          </div>
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          <Button variant="outline" onClick={onClose}>
            {messages.vendorManagement.cancel}
          </Button>
          <Button
            variant="destructive"
            onClick={() => void onDelete()}
            disabled={vendorDeleting || vendorUsageLoading || isReadonlyVendor}
          >
            {vendorDeleting ? messages.vendorManagement.saving : messages.vendorManagement.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
