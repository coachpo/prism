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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import type { PricingTemplate, PricingTemplateConnectionUsageItem } from "@/lib/types";
import { OperatorEmptyState, OperatorInsetPanel } from "@/shared/design-system";

interface PricingTemplateUsageDialogProps {
  onOpenChange: (open: boolean) => void;
  open: boolean;
  pricingTemplateUsageLoading: boolean;
  pricingTemplateUsageRows: PricingTemplateConnectionUsageItem[];
  pricingTemplateUsageTemplate: PricingTemplate | null;
}

export function PricingTemplateUsageDialog({
  onOpenChange,
  open,
  pricingTemplateUsageLoading,
  pricingTemplateUsageRows,
  pricingTemplateUsageTemplate,
}: PricingTemplateUsageDialogProps) {
  const { messages } = useLocale();
  const copy = messages.pricingTemplatesUi;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100vh-2rem)] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{copy.templateUsage}</DialogTitle>
          <DialogDescription>{copy.templateUsageDescription(pricingTemplateUsageTemplate?.name ?? "")}</DialogDescription>
        </DialogHeader>

        <DialogBody className="min-h-0 flex-1 overflow-hidden">
          <div className="flex h-full flex-col gap-4">
            {pricingTemplateUsageTemplate ? (
              <OperatorInsetPanel>
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-sm font-medium text-foreground">{pricingTemplateUsageTemplate.name}</p>
                    <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                      v{pricingTemplateUsageTemplate.version}
                    </code>
                    <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                      {pricingTemplateUsageTemplate.pricing_currency_code}
                    </code>
                  </div>
                  {pricingTemplateUsageTemplate.description ? (
                    <p className="text-sm text-muted-foreground">{pricingTemplateUsageTemplate.description}</p>
                  ) : null}
                </div>
              </OperatorInsetPanel>
            ) : null}

            <div className="min-h-0 flex-1 overflow-y-auto pr-1">
              {pricingTemplateUsageLoading ? (
                <div className="flex flex-col gap-3 py-1">
                  <Skeleton className="h-10 rounded-md" />
                  <Skeleton className="h-10 rounded-md" />
                </div>
              ) : pricingTemplateUsageRows.length === 0 ? (
                <OperatorEmptyState title={copy.templateUnused} />
              ) : (
                <div className="operator-table-shell overflow-hidden rounded-lg border border-outline-variant">
                  <div className="max-h-[320px] overflow-y-auto">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>{copy.model}</TableHead>
                          <TableHead>{copy.endpoint}</TableHead>
                          <TableHead>{copy.terminalTargetColumn}</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {pricingTemplateUsageRows.map((row) => (
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
              )}
            </div>
          </div>
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {copy.close}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
