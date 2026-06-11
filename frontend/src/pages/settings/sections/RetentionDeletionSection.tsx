import type { ReactNode } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { RetentionSettingsResponse } from "@/lib/types";
import { Label } from "@/components/ui/label";
import {
  type CleanupType,
  type RetentionPreset,
} from "../settingsPageHelpers";
import {
  Select,
  SelectContent,
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
      <Card>
        <CardHeader className="pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Trash2 className="h-4 w-4" />
                {copy.title}
              </CardTitle>
              <CardDescription className="text-xs">
                {copy.description}
              </CardDescription>
            </div>
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
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="rounded-lg border p-4">
            <div className="space-y-1">
              <h3 className="text-sm font-semibold">{copy.retentionPolicyTitle}</h3>
              <p className="text-xs text-muted-foreground">{copy.retentionPolicyDescription}</p>
            </div>

            {retentionSettingsLoading ? (
              <div className="mt-4 space-y-2">
                <div className="h-9 animate-pulse rounded bg-muted" />
                <div className="h-9 animate-pulse rounded bg-muted" />
                <div className="h-9 animate-pulse rounded bg-muted" />
                <div className="h-9 animate-pulse rounded bg-muted" />
              </div>
            ) : retentionSettings ? (
              <div className="mt-4 grid gap-3 md:grid-cols-4">
                {[
                  { key: "request_logs_retention_days", label: copy.requestLogsPolicy, value: retentionSettings.request_logs_retention_days },
                  { key: "statistics_retention_days", label: copy.statisticsPolicy, value: retentionSettings.statistics_retention_days },
                  { key: "audit_logs_retention_days", label: copy.auditLogsPolicy, value: retentionSettings.audit_logs_retention_days },
                  { key: "loadbalance_events_retention_days", label: copy.loadbalanceEventsPolicy, value: retentionSettings.loadbalance_events_retention_days },
                ].map(({ key, label, value }) => (
                  <div key={key} className="space-y-2">
                    <Label>{label}</Label>
                    <Select
                      value={toRetentionSelectValue(value)}
                      onValueChange={(nextValue) => setRetentionDays(key as RetentionSettingKey, nextValue === "forever" ? null : Number.parseInt(nextValue, 10))}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="forever">{copy.keepForever}</SelectItem>
                        {RETENTION_DAY_OPTIONS.map((days) => (
                          <SelectItem key={days} value={String(days)}>{copy.retentionDays(days)}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                ))}
              </div>
            ) : (
              <div className="mt-4 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
                {copy.retentionLoadedFailed}
              </div>
            )}
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <div className="space-y-2">
              <Label>{copy.dataType}</Label>
              <Select value={cleanupType} onValueChange={(value) => setCleanupType(value as CleanupType)}>
                <SelectTrigger>
                  <SelectValue placeholder={copy.selectDataType} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="requests">{dialogCopy.cleanupTypeRequests}</SelectItem>
                  <SelectItem value="statistics">{dialogCopy.cleanupTypeStatistics}</SelectItem>
                  <SelectItem value="audits">{dialogCopy.cleanupTypeAudits}</SelectItem>
                  <SelectItem value="loadbalance_events">{dialogCopy.cleanupTypeLoadbalanceEvents}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>{copy.deleteOlderThan}</Label>
              <Select value={retentionPreset} onValueChange={(value) => setRetentionPreset(value as RetentionPreset)}>
                <SelectTrigger>
                  <SelectValue placeholder={copy.selectRetention} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">{copy.retentionDays(1)}</SelectItem>
                  <SelectItem value="7">{copy.retentionDays(7)}</SelectItem>
                  <SelectItem value="30">{copy.retentionDays(30)}</SelectItem>
                  <SelectItem value="90">{copy.retentionDays(90)}</SelectItem>
                  <SelectItem value="all" className="text-destructive">{copy.allData}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-end">
              <Button type="button" variant="destructive" className="w-full" disabled={deleting || !cleanupType || !retentionPreset} onClick={handleOpenDeleteConfirm}>
                {copy.deleteData}
              </Button>
            </div>
          </div>

        </CardContent>
      </Card>
    </section>
  );
}
