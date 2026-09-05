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
import {
  OperatorHelpHint,
  OperatorLoadingState,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorTypeBadge,
} from "@/shared/design-system";

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
              // 「捕获正文」要先开同行的「启用审计」才可用。禁用而不给理由，
              // 与「已关闭」在屏幕上完全同色，操作者只会反复点一个不响应的开关。
              const captureBodiesLocked = !setting.audit_enabled;
              const captureBodiesReasonId = `audit-${setting.api_family}-capture-bodies-reason`;
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
                    <div className="flex items-center gap-1">
                      <Switch
                        aria-label={`${familyLabel} ${copy.captureBodies}`}
                        aria-describedby={captureBodiesLocked ? captureBodiesReasonId : undefined}
                        checked={setting.audit_capture_bodies}
                        disabled={captureBodiesLocked || savingAPIFamilyAuditSettings}
                        onCheckedChange={(checked) =>
                          setAPIFamilyAuditCaptureBodies(setting.api_family, checked)
                        }
                      />
                      {captureBodiesLocked ? (
                        <>
                          {/* 禁用的开关聚焦不了，理由必须另有一个可 Tab 的载体。 */}
                          <span id={captureBodiesReasonId} className="sr-only">
                            {copy.captureBodiesRequiresAudit}
                          </span>
                          <OperatorHelpHint label={copy.captureBodiesRequiresAudit} />
                        </>
                      ) : null}
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
        {auditStorageLoading ? (
          <p className="text-sm text-muted-foreground">{copy.loadingStorageSummary}</p>
        ) : auditStorageSummary ? (
          /* 摘要状态是这块的元字段，不是第四个数值：它挂在摘要条上，
             三个数值位只放真正的数字或缺值的破折号。 */
          <div className="flex flex-col gap-3 rounded-lg border border-border bg-inset p-4">
            <OperatorTypeBadge
              className="w-fit font-normal"
              intent={auditStorageSummary.freshness === "fresh" ? "neutral" : "degraded"}
              preserveLabel
              label={copy.storageFreshnessBadge(
                auditStorageSummary.freshness === "fresh" ? copy.storageFresh : copy.storagePartial,
              )}
            />
            <div className="grid min-w-0 gap-3 @xl/main:grid-cols-2 @4xl/main:grid-cols-3">
              <StorageFact label={copy.retainedAuditRows} reason={copy.storageSummaryUnavailable} value={auditStorageSummary.retained_rows} />
              <StorageFact label={copy.logicalHeaderBytes} reason={copy.storageSummaryUnavailable} value={formatBytes(auditStorageSummary.logical_header_bytes)} />
              <StorageFact label={copy.logicalBodyBytes} reason={copy.storageSummaryUnavailable} value={formatBytes(auditStorageSummary.logical_body_bytes)} />
            </div>
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

/** 缺值走破折号 + text-muted，绝不能和一个真实数字长得一模一样。 */
function StorageFact({ label, reason, value }: { label: string; reason: string; value: string | null }) {
  return (
    <div className="min-w-0">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      {value === null ? (
        <OperatorMissingValue className="text-sm" reason={reason} />
      ) : (
        <p className="truncate text-sm font-semibold text-foreground">{value}</p>
      )}
    </div>
  );
}

function formatBytes(value: string | null) {
  if (value === null) return null;
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
