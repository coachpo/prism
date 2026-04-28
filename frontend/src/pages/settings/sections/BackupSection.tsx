import type { ChangeEvent, ReactNode, RefObject } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  RefreshCw,
  Shield,
  ShieldAlert,
  Upload,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { useLocale } from "@/i18n/useLocale";
import type { ConfigImportPreviewResponse, ConfigImportRequest } from "@/lib/types";

interface BackupSectionProps {
  selectedProfileLabel: string;
  exportSecretsAcknowledged: boolean;
  exportingMode: "safe" | "dangerous" | null;
  fileInputRef: RefObject<HTMLInputElement | null>;
  handleDangerousExport: () => void;
  handleFileSelect: (event: ChangeEvent<HTMLInputElement>) => Promise<void>;
  handleImport: () => Promise<void>;
  handlePreviewImport: () => Promise<void>;
  handleSafeExport: () => void;
  importing: boolean;
  importSummary: {
    endpointsCount: number;
    strategiesCount: number;
    modelsCount: number;
    connectionsCount: number;
  };
  parsedConfig: ConfigImportRequest | null;
  previewInvalidationReason: "bundle_changed" | "profile_changed" | null;
  previewResult: ConfigImportPreviewResponse | null;
  previewing: boolean;
  selectedFile: File | null;
  setExportSecretsAcknowledged: (checked: boolean) => void;
}

interface PreviewRowProps {
  label: string;
  value: ReactNode;
}

function PreviewRow({ label, value }: PreviewRowProps) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="flex shrink-0 items-center gap-2">{value}</span>
    </div>
  );
}

export function BackupSection({
  selectedProfileLabel,
  exportSecretsAcknowledged,
  exportingMode,
  fileInputRef,
  handleDangerousExport,
  handleFileSelect,
  handleImport,
  handlePreviewImport,
  handleSafeExport,
  importing,
  importSummary,
  parsedConfig,
  previewInvalidationReason,
  previewResult,
  previewing,
  selectedFile,
  setExportSecretsAcknowledged,
}: BackupSectionProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.settingsBackup;

  const replacementScopeItems = previewResult
    ? [
        {
          label: copy.scopeEndpoints,
          value: formatNumber(previewResult.replacement_scope.endpoints),
        },
        {
          label: copy.scopePricingTemplates,
          value: formatNumber(previewResult.replacement_scope.pricing_templates),
        },
        {
          label: copy.scopeStrategies,
          value: formatNumber(previewResult.replacement_scope.loadbalance_strategies),
        },
        {
          label: copy.scopeModels,
          value: formatNumber(previewResult.replacement_scope.models),
        },
        {
          label: copy.scopeConnections,
          value: formatNumber(previewResult.replacement_scope.connections),
        },
        {
          label: copy.scopeHeaderBlocklistRules,
          value: formatNumber(previewResult.replacement_scope.header_blocklist_rules),
        },
        {
          label: copy.scopeUserAgentClientRules,
          value: formatNumber(previewResult.replacement_scope.user_agent_client_rules),
        },
        {
          label: copy.scopeProfileSettings,
          value: previewResult.replacement_scope.profile_settings
            ? copy.statusIncluded
            : copy.statusNotIncluded,
        },
      ]
    : [];

  const untouchedScopeItems: Array<{
    label: string;
    value: string;
    variant: "secondary" | "destructive";
  }> = previewResult
    ? [
        {
          label: copy.scopeOtherProfiles,
          value: previewResult.untouched_scope.other_profiles
            ? copy.statusUntouched
            : copy.statusAffected,
          variant: previewResult.untouched_scope.other_profiles ? "secondary" : "destructive",
        },
        {
          label: copy.scopeExistingGlobalVendorMetadata,
          value: previewResult.untouched_scope.existing_global_vendor_metadata
            ? copy.statusUntouched
            : copy.statusAffected,
          variant: previewResult.untouched_scope.existing_global_vendor_metadata
            ? "secondary"
            : "destructive",
        },
        {
          label: copy.scopeRequestLogs,
          value: previewResult.untouched_scope.request_logs
            ? copy.statusUntouched
            : copy.statusAffected,
          variant: previewResult.untouched_scope.request_logs ? "secondary" : "destructive",
        },
      ]
    : [];

  const vendorSummaryItems = previewResult
    ? [
        {
          label: copy.vendorSummaryCreateCount,
          value: formatNumber(previewResult.vendor_summary.create_count),
        },
        {
          label: copy.vendorSummaryReuseCount,
          value: formatNumber(previewResult.vendor_summary.reuse_count),
        },
        {
          label: copy.vendorSummaryWarningCount,
          value: formatNumber(previewResult.vendor_summary.warning_count),
        },
      ]
    : [];

  const secretSummaryItems = previewResult
    ? [
        {
          label: copy.scopeEndpointSecretRefs,
          value: formatNumber(previewResult.secret_summary.endpoint_secret_refs),
        },
        {
          label: copy.scopeSecretPayloadEntries,
          value: formatNumber(previewResult.secret_summary.secret_payload_entries),
        },
        {
          label: copy.scopeDecryptableSecretRefs,
          value: formatNumber(previewResult.secret_summary.decryptable_secret_refs),
        },
      ]
    : [];

  return (
    <section id="backup" tabIndex={-1} className="scroll-mt-24 flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold">{copy.title}</h2>
        <p className="text-sm text-muted-foreground">
          {copy.exportRestoreSnapshots(selectedProfileLabel)}
        </p>
      </div>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm">{copy.title}</CardTitle>
          <CardDescription className="text-xs">
            {copy.exportRestoreSnapshots(selectedProfileLabel)}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4 xl:grid-cols-2">
          <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
            <div className="flex flex-col gap-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Download className="h-4 w-4" />
                {copy.export}
              </CardTitle>
              <CardDescription className="text-xs">{copy.exportDescription}</CardDescription>
            </div>

            <div className="flex flex-col gap-3 rounded-lg border bg-background p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-1">
                  <div className="flex items-center gap-2">
                    <Shield className="h-4 w-4 text-primary" />
                    <span className="text-sm font-medium">{copy.exportWithoutSecrets}</span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {copy.exportWithoutSecretsDescription}
                  </p>
                </div>
                <Badge variant="secondary">{copy.safeDefault}</Badge>
              </div>

              <Button
                type="button"
                className="w-full"
                data-testid="profile-export-safe"
                disabled={exportingMode !== null}
                onClick={handleSafeExport}
              >
                <Download data-icon="inline-start" />
                {exportingMode === "safe" ? copy.exportInProgress : copy.exportWithoutSecrets}
              </Button>
            </div>

            <div className="flex flex-col gap-3 rounded-lg border border-destructive/30 bg-destructive/5 p-4">
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-1">
                  <div className="flex items-center gap-2">
                    <ShieldAlert className="h-4 w-4 text-destructive" />
                    <span className="text-sm font-medium">{copy.exportWithSecrets}</span>
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {copy.exportWithSecretsDescription}
                  </p>
                </div>
                <Badge variant="destructive">{copy.dangerous}</Badge>
              </div>

              <div className="flex items-start gap-2 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-foreground">
                <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
                <span>{copy.dangerousExportDescription}</span>
              </div>

              <div className="flex items-center justify-between gap-3 rounded-md border bg-background px-3 py-3">
                <Label htmlFor="profile-export-acknowledgement" className="text-sm leading-5">
                  {copy.acknowledgement}
                </Label>
                <Switch
                  id="profile-export-acknowledgement"
                  checked={exportSecretsAcknowledged}
                  data-testid="profile-export-dangerous-acknowledgement"
                  onCheckedChange={setExportSecretsAcknowledged}
                />
              </div>

              <Button
                type="button"
                className="w-full"
                data-testid="profile-export-dangerous"
                disabled={exportingMode !== null || !exportSecretsAcknowledged}
                onClick={handleDangerousExport}
                variant="destructive"
              >
                <ShieldAlert data-icon="inline-start" />
                {exportingMode === "dangerous" ? copy.exportInProgress : copy.exportWithSecrets}
              </Button>
            </div>
          </div>

          <div className="flex flex-col gap-4 rounded-lg border p-4">
            <div className="flex flex-col gap-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Upload className="h-4 w-4" />
                {copy.import}
              </CardTitle>
              <CardDescription className="text-xs">{copy.importDescription}</CardDescription>
            </div>

            <Input
              ref={fileInputRef}
              accept=".json"
              autoComplete="off"
              data-testid="profile-import-file"
              name="config_import_file"
              onChange={handleFileSelect}
              type="file"
            />

            {selectedFile && parsedConfig ? (
              <>
                <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-3 text-sm text-muted-foreground">
                  <p>
                    {copy.loadedSummary(
                      selectedFile.name,
                      formatNumber(importSummary.endpointsCount),
                      formatNumber(importSummary.strategiesCount),
                      formatNumber(importSummary.modelsCount),
                      formatNumber(importSummary.connectionsCount),
                    )}
                  </p>
                  <p>{copy.previewDescription}</p>
                </div>

                {!previewResult ? (
                  <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-3 text-sm text-foreground">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                    <span>
                      {previewInvalidationReason === "profile_changed"
                        ? copy.previewRequiresRefreshAfterProfileChange(selectedProfileLabel)
                        : copy.previewRequiresRefresh}
                    </span>
                  </div>
                ) : (
                  <div
                    className={previewResult.ready
                      ? "flex flex-col gap-2 rounded-lg border border-success/30 bg-success/10 px-3 py-3 text-sm text-foreground"
                      : "flex flex-col gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-3 text-sm text-foreground"
                    }
                  >
                    <div className="flex items-center gap-2 font-medium">
                      {previewResult.ready ? (
                        <CheckCircle2 className="h-4 w-4 text-success" />
                      ) : (
                        <AlertTriangle className="h-4 w-4 text-destructive" />
                      )}
                      <span>{copy.previewReady}</span>
                    </div>
                    <p className="text-muted-foreground">
                      {copy.previewReadyBoundToProfile(selectedProfileLabel)}
                    </p>
                  </div>
                )}

                <div className="flex flex-col gap-2 sm:flex-row">
                  <Button
                    type="button"
                    className="w-full sm:flex-1"
                    data-testid="profile-import-preview"
                    disabled={previewing || importing}
                    onClick={() => void handlePreviewImport()}
                    variant="outline"
                  >
                    <RefreshCw data-icon="inline-start" className={previewing ? "animate-spin" : undefined} />
                    {previewing ? copy.previewInProgress : copy.previewAction}
                  </Button>
                  <Button
                    type="button"
                    className="w-full sm:flex-1"
                    data-testid="profile-import-apply"
                    disabled={!previewResult?.ready || previewing || importing}
                    onClick={() => void handleImport()}
                    variant="destructive"
                  >
                    <Upload data-icon="inline-start" />
                    {importing ? copy.importInProgress : copy.applyImport}
                  </Button>
                </div>

                {previewResult ? (
                  <>
                    <Separator />

                    <div className="grid gap-3 xl:grid-cols-2">
                      <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
                        <div className="flex items-center gap-2">
                          <Badge variant="outline">{copy.previewReplacementScope}</Badge>
                        </div>
                        <div className="flex flex-col gap-2">
                          {replacementScopeItems.map((item) => (
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
                          <Badge variant="outline">{copy.previewUntouchedScope}</Badge>
                        </div>
                        <div className="flex flex-col gap-2">
                          {untouchedScopeItems.map((item) => (
                            <PreviewRow
                              key={item.label}
                              label={item.label}
                              value={<Badge variant={item.variant as "secondary" | "destructive"}>{item.value}</Badge>}
                            />
                          ))}
                        </div>
                      </div>

                      <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4">
                        <div className="flex items-center gap-2">
                          <Badge variant="outline">{copy.previewVendorSummary}</Badge>
                        </div>
                        <div className="flex flex-col gap-2">
                          {vendorSummaryItems.map((item) => (
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
                          <Badge variant="outline">{copy.previewSecretSummary}</Badge>
                        </div>
                        <div className="flex flex-col gap-2">
                          {secretSummaryItems.map((item) => (
                            <PreviewRow
                              key={item.label}
                              label={item.label}
                              value={<Badge variant="secondary">{item.value}</Badge>}
                            />
                          ))}
                        </div>
                      </div>

                      {previewResult.vendor_resolutions.length ? (
                        <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-4 xl:col-span-2">
                          <div className="flex items-center gap-2">
                            <Badge variant="outline">{copy.previewVendorResolutions}</Badge>
                          </div>
                          <div className="flex flex-col gap-2">
                            {previewResult.vendor_resolutions.map((resolution) => (
                              <div key={resolution.vendor_key} className="rounded-md border bg-background px-3 py-3">
                                <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                                  <div className="font-medium">{resolution.vendor_key}</div>
                                  <Badge variant={resolution.resolution === "create" ? "default" : "secondary"}>
                                    {resolution.resolution === "create"
                                      ? copy.vendorResolutionCreate
                                      : copy.vendorResolutionReuse}
                                  </Badge>
                                </div>
                                {resolution.warning ? (
                                  <p className="mt-2 text-sm text-warning">{resolution.warning}</p>
                                ) : null}
                              </div>
                            ))}
                          </div>
                        </div>
                      ) : null}

                      {previewResult.warnings.length ? (
                        <div className="flex flex-col gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-3 text-sm text-foreground xl:col-span-2">
                          <div className="flex items-center gap-2 font-medium">
                            <AlertTriangle className="h-4 w-4 text-warning" />
                            <span>{copy.previewWarnings}</span>
                          </div>
                          <ul className="list-disc pl-5 text-muted-foreground">
                            {previewResult.warnings.map((warning) => (
                              <li key={warning}>{warning}</li>
                            ))}
                          </ul>
                        </div>
                      ) : null}

                      {previewResult.blocking_errors.length ? (
                        <div className="flex flex-col gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-3 text-sm text-foreground xl:col-span-2">
                          <div className="flex items-center gap-2 font-medium">
                            <AlertTriangle className="h-4 w-4 text-destructive" />
                            <span>{copy.previewBlockingErrors}</span>
                          </div>
                          <ul className="list-disc pl-5 text-muted-foreground">
                            {previewResult.blocking_errors.map((error) => (
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
    </section>
  );
}
