import type { ChangeEvent, ReactNode, RefObject } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  RefreshCw,
  Upload,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
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
    unchangedCount: number;
    vendorCount: number;
  };
  catalogParsedImport: VendorCatalogImportRequest | null;
  catalogPreviewing: boolean;
  catalogPreviewInvalidationReason: "bundle_changed" | null;
  catalogPreviewReadyForSelection: boolean;
  catalogPreviewResult: VendorCatalogImportPreviewResponse | null;
  catalogSelectedFile: File | null;
  handleCatalogExport: () => Promise<void>;
  handleCatalogFileSelect: (event: ChangeEvent<HTMLInputElement>) => Promise<void>;
  handleCatalogImport: () => Promise<void>;
  handleCatalogPreview: () => Promise<void>;
}

interface PreviewRowProps {
  label: string;
  value: ReactNode;
}

function PreviewRow({ label, value }: PreviewRowProps) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border bg-background px-3 py-2 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="flex shrink-0 items-center gap-2">{value}</span>
    </div>
  );
}

export function VendorCatalogTransportCard({
  catalogExporting,
  catalogFileInputRef,
  catalogImporting,
  catalogImportSummary,
  catalogParsedImport,
  catalogPreviewing,
  catalogPreviewInvalidationReason,
  catalogPreviewReadyForSelection,
  catalogPreviewResult,
  catalogSelectedFile,
  handleCatalogExport,
  handleCatalogFileSelect,
  handleCatalogImport,
  handleCatalogPreview,
}: VendorCatalogTransportCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.vendorManagement;

  const mutationScopeItems = catalogPreviewResult
    ? [
        {
          label: copy.catalogPreviewTarget,
          value: copy.catalogPreviewGlobalTarget,
        },
        {
          label: copy.catalogPreviewCreateCount,
          value: formatNumber(catalogImportSummary.createCount),
        },
        {
          label: copy.catalogPreviewUpdateCount,
          value: formatNumber(catalogImportSummary.updateCount),
        },
        {
          label: copy.catalogPreviewUnchangedCount,
          value: formatNumber(catalogImportSummary.unchangedCount),
        },
      ]
    : [];

  const untouchedScopeItems: Array<{
    label: string;
    value: string;
    variant: "secondary" | "destructive";
  }> = catalogPreviewResult
    ? [
        {
          label: copy.catalogScopeProfiles,
          value: catalogPreviewResult.untouched_scope.profiles
            ? copy.catalogStatusUntouched
            : copy.catalogStatusAffected,
          variant: catalogPreviewResult.untouched_scope.profiles ? "secondary" : "destructive",
        },
        {
          label: copy.catalogScopeProfileScopedConfig,
          value: catalogPreviewResult.untouched_scope.profile_scoped_config
            ? copy.catalogStatusUntouched
            : copy.catalogStatusAffected,
          variant: catalogPreviewResult.untouched_scope.profile_scoped_config
            ? "secondary"
            : "destructive",
        },
        {
          label: copy.catalogScopeRequestLogs,
          value: catalogPreviewResult.untouched_scope.request_logs
            ? copy.catalogStatusUntouched
            : copy.catalogStatusAffected,
          variant: catalogPreviewResult.untouched_scope.request_logs ? "secondary" : "destructive",
        },
      ]
    : [];

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-sm">{copy.catalogSectionTitle}</CardTitle>
        <CardDescription className="text-xs">{copy.catalogSectionDescription}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4 xl:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)]">
        <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
          <div className="flex flex-col gap-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Download className="size-4" />
              {copy.catalogExportTitle}
            </CardTitle>
            <CardDescription className="text-xs">{copy.catalogExportDescription}</CardDescription>
          </div>
          <Button onClick={() => void handleCatalogExport()} disabled={catalogExporting} className="w-full">
            <Download data-icon="inline-start" />
            {catalogExporting ? copy.catalogExporting : copy.catalogExportAction}
          </Button>
        </div>

        <div className="flex flex-col gap-4 rounded-lg border p-4">
          <div className="flex flex-col gap-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Upload className="size-4" />
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
            data-testid="vendor-catalog-import-file"
            onChange={handleCatalogFileSelect}
          />

          {catalogSelectedFile && catalogParsedImport ? (
            <>
              <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-3 text-sm text-muted-foreground">
                <p>{copy.catalogLoadedSummary(catalogSelectedFile.name, formatNumber(catalogImportSummary.vendorCount))}</p>
                <p>{copy.catalogPreviewDescription}</p>
              </div>

              {!catalogPreviewResult ? (
                <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-3 text-sm text-foreground">
                  <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
                  <span>
                    {catalogPreviewInvalidationReason === "bundle_changed"
                      ? copy.catalogPreviewRequiresRefresh
                      : copy.catalogPreviewDescription}
                  </span>
                </div>
              ) : (
                <div
                  className={catalogPreviewReadyForSelection
                    ? "flex flex-col gap-2 rounded-lg border border-success/30 bg-success/10 px-3 py-3 text-sm text-foreground"
                    : "flex flex-col gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-3 text-sm text-foreground"
                  }
                >
                  <div className="flex items-center gap-2 font-medium">
                    {catalogPreviewReadyForSelection ? (
                      <CheckCircle2 className="size-4 text-success" />
                    ) : (
                      <AlertTriangle className="size-4 text-destructive" />
                    )}
                    <span>
                      {catalogPreviewReadyForSelection ? copy.catalogPreviewReady : copy.catalogPreviewNotReady}
                    </span>
                  </div>
                  <p className="text-muted-foreground">
                    {catalogPreviewReadyForSelection
                      ? copy.catalogPreviewReadyBoundToBundle(catalogSelectedFile.name)
                      : copy.catalogPreviewBlockingDescription}
                  </p>
                </div>
              )}

              <div className="flex flex-col gap-2 sm:flex-row">
                <Button
                  type="button"
                  className="w-full sm:flex-1"
                  data-testid="vendor-catalog-preview"
                  disabled={catalogPreviewing || catalogImporting}
                  onClick={() => void handleCatalogPreview()}
                  variant="outline"
                >
                  <RefreshCw data-icon="inline-start" className={catalogPreviewing ? "animate-spin" : undefined} />
                  {catalogPreviewing ? copy.catalogPreviewInProgress : copy.catalogPreviewAction}
                </Button>
                <Button
                  type="button"
                  className="w-full sm:flex-1"
                  data-testid="vendor-catalog-apply"
                  disabled={!catalogPreviewReadyForSelection || catalogPreviewing || catalogImporting}
                  onClick={() => void handleCatalogImport()}
                  variant="destructive"
                >
                  <Upload data-icon="inline-start" />
                  {catalogImporting ? copy.catalogImporting : copy.catalogImportAction}
                </Button>
              </div>

              {catalogPreviewResult ? (
                <>
                  <Separator />

                  <div className="grid gap-3 xl:grid-cols-2">
                    <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
                      <div className="flex items-center gap-2">
                        <Badge variant="outline">{copy.catalogPreviewMutationScope}</Badge>
                      </div>
                      <div className="flex flex-col gap-2">
                        {mutationScopeItems.map((item) => (
                          <PreviewRow
                            key={item.label}
                            label={item.label}
                            value={<Badge variant="secondary">{item.value}</Badge>}
                          />
                        ))}
                      </div>
                    </div>

                    <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
                      <div className="flex items-center gap-2">
                        <Badge variant="outline">{copy.catalogPreviewUntouchedScope}</Badge>
                      </div>
                      <div className="flex flex-col gap-2">
                        {untouchedScopeItems.map((item) => (
                          <PreviewRow
                            key={item.label}
                            label={item.label}
                            value={<Badge variant={item.variant}>{item.value}</Badge>}
                          />
                        ))}
                      </div>
                    </div>

                    {catalogPreviewResult.warnings.length ? (
                      <div className="flex flex-col gap-2 rounded-lg border border-warning/30 bg-warning/10 p-4 xl:col-span-2">
                        <div className="flex items-center gap-2 font-medium text-foreground">
                          <AlertTriangle className="size-4 text-warning" />
                          <span>{copy.catalogPreviewWarnings}</span>
                        </div>
                        <ul className="list-disc pl-5 text-sm text-foreground">
                          {catalogPreviewResult.warnings.map((warning) => (
                            <li key={warning}>{warning}</li>
                          ))}
                        </ul>
                      </div>
                    ) : null}

                    {catalogPreviewResult.blocking_errors.length ? (
                      <div className="flex flex-col gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-4 xl:col-span-2">
                        <div className="flex items-center gap-2 font-medium text-destructive">
                          <AlertTriangle className="size-4" />
                          <span>{copy.catalogPreviewBlockingErrors}</span>
                        </div>
                        <ul className="list-disc pl-5 text-sm text-foreground">
                          {catalogPreviewResult.blocking_errors.map((error) => (
                            <li key={error}>{error}</li>
                          ))}
                        </ul>
                      </div>
                    ) : null}
                  </div>
                </>
              ) : null}
            </>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
