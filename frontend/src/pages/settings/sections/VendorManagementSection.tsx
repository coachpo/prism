import { Building2 } from "lucide-react";
import { VendorIcon } from "@/components/VendorIcon";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { Vendor } from "@/lib/types";

interface VendorManagementSectionProps {
  vendors: Vendor[];
  vendorsLoading: boolean;
  onCreateVendor: () => void;
  onDeleteVendor: (vendor: Vendor) => Promise<void>;
  onEditVendor: (vendor: Vendor) => void;
}

export function VendorManagementSection({
  vendors,
  vendorsLoading,
  onCreateVendor,
  onDeleteVendor,
  onEditVendor,
}: VendorManagementSectionProps) {
  const { messages } = useLocale();

  return (
    <section id="vendor-management" tabIndex={-1} className="scroll-mt-24">
      <Card>
        <CardHeader className="pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Building2 className="h-4 w-4" />
                {messages.vendorManagement.sectionTitle}
              </CardTitle>
              <CardDescription className="text-xs">
                {messages.vendorManagement.sectionDescription}
              </CardDescription>
            </div>
            <CardAction>
              <Button onClick={onCreateVendor}>{messages.vendorManagement.addVendor}</Button>
            </CardAction>
          </div>
        </CardHeader>

        <CardContent>
          {vendorsLoading ? (
            <div className="h-24 animate-pulse rounded-md bg-muted/50" />
          ) : vendors.length === 0 ? (
            <div className="rounded-lg border border-dashed bg-muted/20 p-6 text-sm">
              <p className="font-medium">{messages.vendorManagement.emptyTitle}</p>
              <p className="mt-1 text-muted-foreground">
                {messages.vendorManagement.emptyDescription}
              </p>
            </div>
          ) : (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{messages.vendorManagement.tableName}</TableHead>
                    <TableHead>{messages.vendorManagement.tableKey}</TableHead>
                    <TableHead>{messages.vendorManagement.tableDescription}</TableHead>
                    <TableHead>{messages.vendorManagement.actions}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {vendors.map((vendor) => (
                    <TableRow key={vendor.id}>
                      <TableCell className="font-medium">
                        <div className="flex items-center gap-2">
                          <VendorIcon vendor={vendor} size={18} />
                          <span>{vendor.name}</span>
                        </div>
                      </TableCell>
                      <TableCell>{vendor.key}</TableCell>
                      <TableCell className="max-w-[24rem] whitespace-normal text-muted-foreground">
                        {vendor.description || messages.vendorManagement.noDescription}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Button variant="outline" size="sm" onClick={() => onEditVendor(vendor)}>
                            {messages.vendorManagement.edit}
                          </Button>
                          <Button
                            variant="destructive"
                            size="sm"
                            onClick={() => void onDeleteVendor(vendor)}
                          >
                            {messages.vendorManagement.delete}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
