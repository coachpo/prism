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
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{copy.templateUsage}</DialogTitle>
          <DialogDescription>{copy.templateUsageDescription(pricingTemplateUsageTemplate?.name ?? "")}</DialogDescription>
        </DialogHeader>
        <div className="py-4">
          {pricingTemplateUsageLoading ? (
            <div className="space-y-2">
              <div className="h-10 animate-pulse rounded-md bg-muted/50" />
              <div className="h-10 animate-pulse rounded-md bg-muted/50" />
            </div>
          ) : pricingTemplateUsageRows.length === 0 ? (
            <div className="rounded-md border border-dashed p-8 text-center">
              <p className="text-sm text-muted-foreground">
                {copy.templateUnused}
              </p>
            </div>
          ) : (
            <div className="max-h-[400px] overflow-y-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{copy.model}</TableHead>
                    <TableHead>{copy.endpoint}</TableHead>
                    <TableHead>{messages.requestLogs.connection}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {pricingTemplateUsageRows.map((row) => (
                    <TableRow key={row.connection_id}>
                      <TableCell className="font-medium">{row.model_id}</TableCell>
                      <TableCell>{row.endpoint_name}</TableCell>
                        <TableCell>
                          {row.connection_name || (
                          <span className="text-muted-foreground italic">{copy.unnamed}</span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>{copy.close}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
