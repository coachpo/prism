import type { ReactNode, RefObject } from "react";
import { ShieldCheck } from "lucide-react";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { ApiFamily, AuditAPIFamilySetting, AuditStorageSummary } from "@/lib/types";
import { OperatorLoadingState, OperatorSectionCard } from "@/shared/design-system";

interface AuditConfigurationAPIFamilyCardProps {
  apiFamilyAuditSettings: AuditAPIFamilySetting[];
  apiFamilyAuditSettingsDirty: boolean;
  cardRef: RefObject<HTMLDivElement | null>;
  className?: string;
  loadingAPIFamilyAuditSettings: boolean;
  renderSectionSaveState: (section: "audit", isDirty: boolean) => ReactNode;
  savingAPIFamilyAuditSettings: boolean;
  setAPIFamilyAuditCaptureBodies: (apiFamily: ApiFamily, checked: boolean) => void;
  setAPIFamilyAuditEnabled: (apiFamily: ApiFamily, checked: boolean) => void;
  auditStorageSummary?: AuditStorageSummary | null;
  auditStorageLoading?: boolean;
}

export function AuditConfigurationAPIFamilyCard({
  apiFamilyAuditSettings,
  apiFamilyAuditSettingsDirty,
  cardRef,
  className,
  loadingAPIFamilyAuditSettings,
  renderSectionSaveState,
  savingAPIFamilyAuditSettings,
  setAPIFamilyAuditCaptureBodies,
  setAPIFamilyAuditEnabled,
  auditStorageSummary = null,
  auditStorageLoading = false,
}: AuditConfigurationAPIFamilyCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAudit;

  return (
    <OperatorSectionCard
      ref={cardRef}
      className={className}
      data-testid="audit-api-family-card"
      title={(
        <span className="flex items-center gap-2">
          <ShieldCheck data-icon="inline-start" />
          {copy.apiFamilyAuditControls}
        </span>
      )}
      description={copy.apiFamilyAuditDescription}
      actions={renderSectionSaveState("audit", apiFamilyAuditSettingsDirty)}
    >
      {loadingAPIFamilyAuditSettings ? (
        <OperatorLoadingState title={copy.loadingAPIFamilyAuditSettings} />
      ) : (
        <>
        {/* 卡片自己的边框就是这张表的边框，不再套第二圈。 */}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.apiFamily}</TableHead>
              <TableHead className="w-[140px]">{copy.auditEnabled}</TableHead>
              <TableHead className="w-[140px]">{copy.captureBodies}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {apiFamilyAuditSettings.map((setting) => {
              const familyLabel = getAPIFamilyLabel(setting.api_family, copy);
              return (
                <TableRow key={setting.api_family} data-testid={`audit-api-family-row-${setting.api_family}`}>
                  <TableCell>
                    <div className="flex items-center gap-2 font-medium">
                      <ApiFamilyIcon apiFamily={setting.api_family} />
                      {familyLabel}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Switch
                      aria-label={`${familyLabel} ${copy.auditEnabled}`}
                      checked={setting.audit_enabled}
                      disabled={savingAPIFamilyAuditSettings}
                      onCheckedChange={(checked) =>
                        setAPIFamilyAuditEnabled(setting.api_family, checked)
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <Switch
                      aria-label={`${familyLabel} ${copy.captureBodies}`}
                      checked={setting.audit_capture_bodies}
                      disabled={!setting.audit_enabled || savingAPIFamilyAuditSettings}
                      onCheckedChange={(checked) =>
                        setAPIFamilyAuditCaptureBodies(setting.api_family, checked)
                      }
                    />
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
        {auditStorageLoading ? (
          <p className="text-sm text-muted-foreground">{copy.loadingStorageSummary}</p>
        ) : auditStorageSummary ? (
          <div className="grid gap-3 rounded-lg border border-border bg-inset p-4 sm:grid-cols-2 lg:grid-cols-4">
            <StorageFact label={copy.retainedAuditRows} value={auditStorageSummary.retained_rows ?? copy.storageUnavailable} />
            <StorageFact label={copy.logicalHeaderBytes} value={formatBytes(auditStorageSummary.logical_header_bytes, copy.storageUnavailable)} />
            <StorageFact label={copy.logicalBodyBytes} value={formatBytes(auditStorageSummary.logical_body_bytes, copy.storageUnavailable)} />
            <StorageFact label={copy.storageFreshness} value={auditStorageSummary.freshness === "fresh" ? copy.storageFresh : copy.storagePartial} />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{copy.storageSummaryUnavailable}</p>
        )}
        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer font-medium text-foreground">{copy.captureLimitsDescription}</summary>
          <p className="pt-2">{copy.captureLimitsDescriptionDetails}</p>
        </details>
        </>
      )}
    </OperatorSectionCard>
  );
}

function StorageFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="truncate text-sm font-semibold text-foreground">{value}</p>
    </div>
  );
}

function formatBytes(value: string | null, unavailable: string) {
  if (value === null) return unavailable;
  const bytes = Number(value);
  if (!Number.isSafeInteger(bytes)) return value;
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

function getAPIFamilyLabel(apiFamily: ApiFamily, copy: ReturnType<typeof useLocale>["messages"]["settingsAudit"]) {
  switch (apiFamily) {
    case "openai":
      return copy.openaiFamily;
    case "anthropic":
      return copy.anthropicFamily;
    case "gemini":
      return copy.geminiFamily;
  }
}
