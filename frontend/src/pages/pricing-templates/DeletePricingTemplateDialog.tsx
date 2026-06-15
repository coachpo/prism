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
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { PricingTemplate, PricingTemplateConnectionUsageItem } from "@/lib/types";

interface DeletePricingTemplateDialogProps {
  deletePricingTemplateConfirm: PricingTemplate | null;
  displayTemplate?: PricingTemplate | null;
  deletePricingTemplateConflict: PricingTemplateConnectionUsageItem[] | null;
  pricingTemplateUsageError: boolean;
  onClose: () => void;
  onDelete: () => Promise<void>;
  pricingTemplateDeleting: boolean;
  pricingTemplateUsageLoading: boolean;
  pricingTemplateUsageRows: PricingTemplateConnectionUsageItem[];
}

export function DeletePricingTemplateDialog({
  deletePricingTemplateConfirm,
  displayTemplate = deletePricingTemplateConfirm,
  deletePricingTemplateConflict,
  pricingTemplateUsageError,
  onClose,
  onDelete,
  pricingTemplateDeleting,
  pricingTemplateUsageLoading,
  pricingTemplateUsageRows,
}: DeletePricingTemplateDialogProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.pricingTemplatesUi;
  const dialogTemplate = deletePricingTemplateConfirm ?? displayTemplate;
  const dependencyRows = deletePricingTemplateConflict ?? pricingTemplateUsageRows;
  const hasDependencies = dependencyRows.length > 0;
  const deleteDisabled =
    pricingTemplateDeleting ||
    pricingTemplateUsageLoading ||
    pricingTemplateUsageError ||
    hasDependencies;

  return (
    <Dialog
      open={deletePricingTemplateConfirm !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent className="max-h-[calc(100vh-2rem)] sm:max-w-3xl" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.deletePricingTemplate}</DialogTitle>
          <DialogDescription>
            {copy.deletePricingTemplateDescription(dialogTemplate?.name ?? "")}
          </DialogDescription>
        </DialogHeader>

        <DialogBody className="min-h-0 flex-1 overflow-hidden">
          <div className="flex h-full flex-col gap-4">
            {dialogTemplate ? (
              <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
                <div className="flex flex-col gap-2">
                  <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-sm font-medium text-foreground">{dialogTemplate.name}</p>
                    <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                      v{dialogTemplate.version}
                    </code>
                    <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                      {dialogTemplate.pricing_currency_code}
                    </code>
                  </div>
                  {dialogTemplate.description ? (
                    <p className="text-sm text-muted-foreground">{dialogTemplate.description}</p>
                  ) : null}
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="flex min-w-0 flex-col gap-1">
                    <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.input}</p>
                    <p className="text-sm text-foreground">{dialogTemplate.input_price}</p>
                  </div>
                  <div className="flex min-w-0 flex-col gap-1">
                    <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.output}</p>
                    <p className="text-sm text-foreground">{dialogTemplate.output_price}</p>
                  </div>
                </div>

                <Separator />

                <p className="text-sm text-muted-foreground">{messages.common.thisActionCannotBeUndone}</p>
              </div>
            ) : null}

            <div className="min-h-0 flex-1 overflow-y-auto pr-1">
              {pricingTemplateUsageLoading ? (
                <div className="flex flex-col gap-3 py-1">
                  <Skeleton className="h-10 rounded-md" />
                  <Skeleton className="h-32 rounded-md" />
                </div>
              ) : pricingTemplateUsageError ? (
                <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                  {messages.pricingTemplatesData.loadUsageFailed}
                </div>
              ) : hasDependencies ? (
                <div className="flex flex-col gap-4 py-1">
                  <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
                    {copy.deletePricingTemplateInUse(formatNumber(dependencyRows.length))}
                  </div>

                  <div className="overflow-hidden rounded-lg border">
                    <div className="max-h-[260px] overflow-y-auto">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>{copy.model}</TableHead>
                            <TableHead>{copy.endpoint}</TableHead>
                            <TableHead>{copy.terminalTargetColumn}</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {dependencyRows.map((row) => (
                            <TableRow key={row.connection_id}>
                              <TableCell className="font-medium">{row.model_id}</TableCell>
                              <TableCell>{row.endpoint_name}</TableCell>
                              <TableCell>
                                {row.connection_name || (
                                  <span className="italic text-muted-foreground">{copy.unnamed}</span>
                                )}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          <Button variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button variant="destructive" onClick={() => void onDelete()} disabled={deleteDisabled}>
            {pricingTemplateDeleting ? messages.settingsDialogs.deleting : messages.settingsDialogs.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
