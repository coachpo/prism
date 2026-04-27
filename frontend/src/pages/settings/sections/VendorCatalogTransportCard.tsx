import type { ChangeEvent, RefObject } from "react";
import { Download, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import type {
  VendorCatalogImportPreviewResponse,
  VendorCatalogImportRequest,
} from "@/lib/types";

interface VendorCatalogTransportCardProps {
  catalogExporting: boolean;
  catalogFileInputRef: RefObject<HTMLInputElement | null>;
  catalogImporting: boolean;
  catalogImportSummary: {
    createCount: number;
    updateCount: number;
    vendorCount: number;
  };
  catalogParsedImport: VendorCatalogImportRequest | null;
  catalogPreviewResult: VendorCatalogImportPreviewResponse | null;
  catalogSelectedFile: File | null;
  handleCatalogExport: () => Promise<void>;
  handleCatalogFileSelect: (event: ChangeEvent<HTMLInputElement>) => Promise<void>;
  handleCatalogImport: () => Promise<void>;
}

export function VendorCatalogTransportCard({
  catalogExporting,
  catalogFileInputRef,
  catalogImporting,
  catalogImportSummary,
  catalogParsedImport,
  catalogPreviewResult,
  catalogSelectedFile,
  handleCatalogExport,
  handleCatalogFileSelect,
  handleCatalogImport,
}: VendorCatalogTransportCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.vendorManagement;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">{copy.catalogSectionTitle}</CardTitle>
        <CardDescription className="text-xs">{copy.catalogSectionDescription}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 xl:grid-cols-2">
        <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
          <div className="flex flex-col gap-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Download className="h-4 w-4" />
              {copy.catalogExportTitle}
            </CardTitle>
            <CardDescription className="text-xs">{copy.catalogExportDescription}</CardDescription>
          </div>
          <Button onClick={() => void handleCatalogExport()} disabled={catalogExporting} className="w-full">
            {catalogExporting ? copy.catalogExporting : copy.catalogExportAction}
          </Button>
        </div>

        <div className="flex flex-col gap-4 rounded-lg border p-4">
          <div className="flex flex-col gap-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Upload className="h-4 w-4" />
              {copy.catalogImportTitle}
            </CardTitle>
            <CardDescription className="text-xs">{copy.catalogImportDescription}</CardDescription>
          </div>

          <Input
            ref={catalogFileInputRef}
            name="vendor_catalog_import_file"
            type="file"
            autoComplete="off"
            accept=".json"
            onChange={handleCatalogFileSelect}
          />

          {catalogSelectedFile && catalogParsedImport ? (
            <div className="flex flex-col gap-2 text-sm text-muted-foreground">
              <p>{copy.catalogLoadedSummary(catalogSelectedFile.name, formatNumber(catalogImportSummary.vendorCount))}</p>
              <p>{copy.catalogPreviewSummary(formatNumber(catalogImportSummary.createCount), formatNumber(catalogImportSummary.updateCount))}</p>

              {catalogPreviewResult?.ready ? (
                <p className="text-emerald-700 dark:text-emerald-400">{copy.catalogPreviewReady}</p>
              ) : null}

              {catalogPreviewResult?.warnings.length ? (
                <div className="flex flex-col gap-1 text-amber-700 dark:text-amber-400">
                  <p className="font-medium">{copy.catalogPreviewWarnings}</p>
                  <ul className="list-disc pl-5">
                    {catalogPreviewResult.warnings.map((warning) => (
                      <li key={warning}>{warning}</li>
                    ))}
                  </ul>
                </div>
              ) : null}

              {catalogPreviewResult?.blocking_errors.length ? (
                <div className="flex flex-col gap-1 text-destructive">
                  <p className="font-medium">{copy.catalogPreviewBlockingErrors}</p>
                  <ul className="list-disc pl-5">
                    {catalogPreviewResult.blocking_errors.map((error) => (
                      <li key={error}>{error}</li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </div>
          ) : null}

          <Button
            onClick={() => void handleCatalogImport()}
            disabled={!catalogParsedImport || !catalogPreviewResult?.ready || catalogImporting}
            variant="secondary"
            className="w-full"
          >
            {catalogImporting ? copy.catalogImporting : copy.catalogImportAction}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
