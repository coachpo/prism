import type { ReactNode } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { useLocale } from "@/i18n/useLocale";
import type { RetentionSettingsResponse } from "@/lib/types";
import { Skeleton } from "@/components/ui/skeleton";
import { OperatorCallout, OperatorInsetPanel, OperatorSectionCard } from "@/shared/design-system";
import {
  type CleanupType,
  type RetentionPreset,
} from "../settingsPageHelpers";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type RetentionSettingKey = keyof Pick<
  RetentionSettingsResponse,
  | "request_logs_retention_days"
  | "statistics_retention_days"
  | "audit_logs_retention_days"
  | "loadbalance_events_retention_days"
>;

interface RetentionDeletionSectionProps {
  cleanupType: CleanupType;
  setCleanupType: (type: CleanupType) => void;
  retentionPreset: RetentionPreset;
  setRetentionPreset: (preset: RetentionPreset) => void;
  deleting: boolean;
  handleOpenDeleteConfirm: () => void;
  renderSectionSaveState: (section: "retention", isDirty: boolean) => ReactNode;
  handleSaveRetentionSettings: () => Promise<void>;
  retentionSettings: RetentionSettingsResponse | null;
  retentionSettingsDirty: boolean;
  retentionSettingsLoading: boolean;
  retentionSettingsSaving: boolean;
  setRetentionDays: (
    key: RetentionSettingKey,
    value: number | null,
  ) => void;
}

const RETENTION_DAY_OPTIONS = [1, 7, 30, 90, 365] as const;

function toRetentionSelectValue(value: number | null) {
  return value === null ? "forever" : String(value);
}

export function RetentionDeletionSection({
  cleanupType,
  setCleanupType,
  retentionPreset,
  setRetentionPreset,
  deleting,
  handleOpenDeleteConfirm,
  renderSectionSaveState,
  handleSaveRetentionSettings,
  retentionSettings,
  retentionSettingsDirty,
  retentionSettingsLoading,
  retentionSettingsSaving,
  setRetentionDays,
}: RetentionDeletionSectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsRetentionDeletion;
  const dialogCopy = messages.settingsDialogs;

  return (
    <section id="retention-deletion" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={(
          <span className="flex items-center gap-2">
            <Trash2 data-icon="inline-start" />
            {copy.title}
          </span>
        )}
        description={copy.description}
        actions={(
          <div className="flex items-center gap-2">
            {renderSectionSaveState("retention", retentionSettingsDirty)}
            <Button
              type="button"
              size="sm"
              onClick={() => void handleSaveRetentionSettings()}
              disabled={retentionSettingsLoading || retentionSettingsSaving || !retentionSettingsDirty || retentionSettings === null}
            >
              {retentionSettingsSaving ? copy.savingRetention : copy.saveRetention}
            </Button>
          </div>
        )}
        contentClassName="flex flex-col gap-4"
      >
          <OperatorInsetPanel title={copy.retentionPolicyTitle} description={copy.retentionPolicyDescription}>
            {retentionSettingsLoading ? (
              <div className="flex flex-col gap-2" aria-hidden="true">
                <Skeleton className="h-9 rounded" />
                <Skeleton className="h-9 rounded" />
                <Skeleton className="h-9 rounded" />
                <Skeleton className="h-9 rounded" />
              </div>
            ) : retentionSettings ? (
              <div className="grid gap-3 md:grid-cols-4">
                {[
                  { key: "request_logs_retention_days", label: copy.requestLogsPolicy, value: retentionSettings.request_logs_retention_days },
                  { key: "statistics_retention_days", label: copy.statisticsPolicy, value: retentionSettings.statistics_retention_days },
                  { key: "audit_logs_retention_days", label: copy.auditLogsPolicy, value: retentionSettings.audit_logs_retention_days },
                  { key: "loadbalance_events_retention_days", label: copy.loadbalanceEventsPolicy, value: retentionSettings.loadbalance_events_retention_days },
                ].map(({ key, label, value }) => (
                  <Field key={key}>
                    <FieldLabel>{label}</FieldLabel>
                    <Select
                      value={toRetentionSelectValue(value)}
                      onValueChange={(nextValue) => setRetentionDays(key as RetentionSettingKey, nextValue === "forever" ? null : Number.parseInt(nextValue, 10))}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          <SelectItem value="forever">{copy.keepForever}</SelectItem>
                          {RETENTION_DAY_OPTIONS.map((days) => (
                            <SelectItem key={days} value={String(days)}>{copy.retentionDays(days)}</SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                ))}
              </div>
            ) : (
              <OperatorCallout intent="warning" description={copy.retentionLoadedFailed} />
            )}
          </OperatorInsetPanel>

          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              <FieldLabel>{copy.dataType}</FieldLabel>
              <Select value={cleanupType} onValueChange={(value) => setCleanupType(value as CleanupType)}>
                <SelectTrigger>
                  <SelectValue placeholder={copy.selectDataType} />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="requests">{dialogCopy.cleanupTypeRequests}</SelectItem>
                    <SelectItem value="statistics">{dialogCopy.cleanupTypeStatistics}</SelectItem>
                    <SelectItem value="audits">{dialogCopy.cleanupTypeAudits}</SelectItem>
                    <SelectItem value="loadbalance_events">{dialogCopy.cleanupTypeLoadbalanceEvents}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>

            <Field>
              <FieldLabel>{copy.deleteOlderThan}</FieldLabel>
              <Select value={retentionPreset} onValueChange={(value) => setRetentionPreset(value as RetentionPreset)}>
                <SelectTrigger>
                  <SelectValue placeholder={copy.selectRetention} />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="1">{copy.retentionDays(1)}</SelectItem>
                    <SelectItem value="7">{copy.retentionDays(7)}</SelectItem>
                    <SelectItem value="30">{copy.retentionDays(30)}</SelectItem>
                    <SelectItem value="90">{copy.retentionDays(90)}</SelectItem>
                    <SelectItem value="all" className="text-destructive">{copy.allData}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>

            <div className="flex items-end">
              <Button type="button" variant="destructive" className="w-full" disabled={deleting || !cleanupType || !retentionPreset} onClick={handleOpenDeleteConfirm}>
                {copy.deleteData}
              </Button>
            </div>
          </div>

      </OperatorSectionCard>
    </section>
  );
}
