import type { ChangeEvent, RefObject } from "react";
import { AlertTriangle, Download, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import type { ConfigImportPreviewResponse, ConfigImportRequest } from "@/lib/types";

interface BackupSectionProps {
  selectedProfileLabel: string;
  exporting: boolean;
  handleExport: () => Promise<void>;
  fileInputRef: RefObject<HTMLInputElement | null>;
  handleFileSelect: (event: ChangeEvent<HTMLInputElement>) => Promise<void>;
  selectedFile: File | null;
  parsedConfig: ConfigImportRequest | null;
  previewResult: ConfigImportPreviewResponse | null;
  importSummary: {
    endpointsCount: number;
    strategiesCount: number;
    modelsCount: number;
    connectionsCount: number;
  };
  importing: boolean;
  handleImport: () => Promise<void>;
}

export function BackupSection({
  selectedProfileLabel,
  exporting,
  handleExport,
  fileInputRef,
  handleFileSelect,
  selectedFile,
  parsedConfig,
  previewResult,
  importSummary,
  importing,
  handleImport,
}: BackupSectionProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.settingsBackup;
  return (
    <section id="backup" tabIndex={-1} className="scroll-mt-24 space-y-4">
      <div className="space-y-1">
        <h2 className="text-base font-semibold">{copy.title}</h2>
        <p className="text-sm text-muted-foreground">
          {copy.exportRestoreSnapshots(selectedProfileLabel)}
        </p>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Download className="h-4 w-4" />
              {copy.export}
            </CardTitle>
            <CardDescription className="text-xs">
              {copy.exportDescription}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex items-start gap-2 rounded-md border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-400">
              <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{copy.exportsContainApiKeys}</span>
            </div>

            <Button
              onClick={() => void handleExport()}
              disabled={exporting}
              className="w-full"
            >
              {exporting ? copy.exportInProgress : copy.exportConfiguration}
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Upload className="h-4 w-4" />
              {copy.import}
            </CardTitle>
            <CardDescription className="text-xs">
              {copy.importDescription}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Input
              ref={fileInputRef}
              name="config_import_file"
              type="file"
              autoComplete="off"
              accept=".json"
              onChange={handleFileSelect}
            />

            {selectedFile && parsedConfig && (
              <div className="space-y-2 text-sm text-muted-foreground">
                <p>
                  {copy.loadedSummary(
                    selectedFile.name,
                    formatNumber(importSummary.endpointsCount),
                    formatNumber(importSummary.strategiesCount),
                    formatNumber(importSummary.modelsCount),
                    formatNumber(importSummary.connectionsCount),
                  )}
                </p>

                {previewResult?.ready ? (
                  <p className="text-emerald-700 dark:text-emerald-400">{copy.previewReady}</p>
                ) : null}

                {previewResult?.warnings.length ? (
                  <div className="space-y-1 text-amber-700 dark:text-amber-400">
                    <p className="font-medium">{copy.previewWarnings}</p>
                    <ul className="list-disc pl-5">
                      {previewResult.warnings.map((warning) => (
                        <li key={warning}>{warning}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}

                {previewResult?.blocking_errors.length ? (
                  <div className="space-y-1 text-destructive">
                    <p className="font-medium">{copy.previewBlockingErrors}</p>
                    <ul className="list-disc pl-5">
                      {previewResult.blocking_errors.map((error) => (
                        <li key={error}>{error}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </div>
            )}

            <Button
              onClick={() => void handleImport()}
              disabled={!parsedConfig || !previewResult?.ready || importing}
              variant="destructive"
              className="w-full"
            >
              {importing ? copy.importInProgress : copy.importConfiguration}
            </Button>
          </CardContent>
        </Card>
      </div>
    </section>
  );
}
