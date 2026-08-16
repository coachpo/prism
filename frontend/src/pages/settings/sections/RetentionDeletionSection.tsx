import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import type { RetentionCoverageSummary, RetentionSettingsPolicies, RetentionSettingsResponse, RetentionSettingsResponsePolicies } from "@/lib/types";
import { Skeleton } from "@/components/ui/skeleton";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorSectionCard,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { useTimezone } from "@/hooks/useTimezone";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type RetentionSettingKey = keyof RetentionSettingsPolicies;

interface RetentionDeletionSectionProps {
  renderSectionSaveState: (section: "retention", isDirty: boolean) => ReactNode;
  retentionSettings: RetentionSettingsResponse | null;
  retentionSettingsDirty: boolean;
  retentionSettingsLoading: boolean;
  setRetentionDays: (key: RetentionSettingKey, value: number | null) => void;
  applyRecommendation: () => void;
  onInvalidCustomValueChange: (hasInvalid: boolean) => void;
}

const RETENTION_DAY_OPTIONS = [1, 7, 30, 90] as const;

const COVERAGE_DATASETS = [
  ["request_logs", "requestLogsPolicy"],
  ["usage_request_events", "statisticsPolicy"],
  ["audit_logs", "auditLogsPolicy"],
  ["loadbalance_events", "loadbalanceEventsPolicy"],
] as const;

function toRetentionSelectValue(value: number | null) {
  if (value === null) return "none";
  return RETENTION_DAY_OPTIONS.includes(value as (typeof RETENTION_DAY_OPTIONS)[number]) ? String(value) : "custom";
}

function formatCoverageTime(value: string | null | undefined, noCutoff: string, format: (value: string) => string) {
  if (!value) return noCutoff;
  return format(value);
}

function coverageState(coverage: RetentionCoverageSummary) {
  return coverage.complete && coverage.freshness === "fresh" ? "complete" : "partial";
}

type CoverageAxis = { start: number; end: number } | null;

/**
 * The shared time axis for the span bars: earliest known start to latest known
 * end across the datasets. Without a common axis the bars would be four
 * unrelated widths and could not be compared, which is the whole point of
 * showing them side by side.
 */
function coverageAxis(settings: RetentionSettingsResponse): CoverageAxis {
  const stamps: number[] = [];
  for (const [dataset] of COVERAGE_DATASETS) {
    const coverage = settings.actual_coverage?.[dataset as keyof RetentionSettingsResponse["actual_coverage"]];
    if (!coverage) continue;
    for (const value of [coverage.from_time, coverage.to_time]) {
      if (!value) continue;
      const parsed = new Date(value).getTime();
      if (Number.isFinite(parsed)) stamps.push(parsed);
    }
  }
  if (stamps.length < 2) return null;
  const start = Math.min(...stamps);
  const end = Math.max(...stamps);
  return end > start ? { start, end } : null;
}

function CoverageCard({
  axis,
  copy,
  coverage,
  format,
  label,
}: {
  axis: CoverageAxis;
  copy: ReturnType<typeof useLocale>["messages"]["settingsRetentionDeletion"];
  coverage: RetentionCoverageSummary | null;
  format: (value: string) => string;
  label: string;
}) {
  // Absent coverage is its own state: it does not mean the dataset is empty,
  // so it renders as unknown rather than as an empty span.
  if (!coverage) {
    return (
      <div className="flex flex-col gap-2 rounded-md border border-border bg-panel p-3" data-coverage-state="unknown">
        <div className="flex items-center gap-2">
          <OperatorStatusBadge intent="idle" preserveLabel label={copy.coverageStateUnknown} />
          <span className="text-xs font-medium">{label}</span>
        </div>
        <p className="text-xs text-muted-foreground">{copy.coverageStateUnknownReason}</p>
      </div>
    );
  }

  const state = coverageState(coverage);
  const fromLabel = formatCoverageTime(coverage.from_time, copy.noLogicalCutoff, format);
  const toLabel = formatCoverageTime(coverage.to_time, copy.notAvailable, format);
  const span = spanGeometry(axis, coverage);

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-panel p-3" data-coverage-state={state}>
      <div className="flex flex-wrap items-center gap-2">
        <OperatorStatusBadge
          intent={state === "complete" ? "healthy" : "degraded"}
          preserveLabel
          label={state === "complete" ? copy.coverageStateComplete : copy.coverageStatePartial}
        />
        <span className="text-xs font-medium">{label}</span>
      </div>

      {span ? (
        <div
          className="h-1.5 w-full rounded-full bg-inset"
          role="img"
          aria-label={copy.coverageSpanAria(label, fromLabel, toLabel)}
        >
          <div
            className={state === "complete" ? "h-full rounded-full bg-healthy" : "h-full rounded-full bg-degraded"}
            style={{ marginLeft: `${span.offsetPercent}%`, width: `${span.widthPercent}%` }}
          />
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{copy.coverageSpanUnknown}</p>
      )}

      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-xs">
        <dt className="text-muted-foreground">{copy.coverageFromLabel}</dt>
        <dd className="truncate font-mono tabular-nums">{fromLabel}</dd>
        <dt className="text-muted-foreground">{copy.coverageToLabel}</dt>
        <dd className="truncate font-mono tabular-nums">{toLabel}</dd>
        <dt className="text-muted-foreground">{copy.coverageBasisLabel}</dt>
        <dd className="truncate">{coverage.source}</dd>
        <dt className="text-muted-foreground">{copy.coverageGenerationLabel}</dt>
        <dd className="truncate font-mono tabular-nums">{coverage.retention_generation}</dd>
      </dl>
    </div>
  );
}

function spanGeometry(axis: CoverageAxis, coverage: RetentionCoverageSummary) {
  if (!axis) return null;
  const from = coverage.from_time ? new Date(coverage.from_time).getTime() : axis.start;
  const to = coverage.to_time ? new Date(coverage.to_time).getTime() : axis.end;
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return null;
  const total = axis.end - axis.start;
  const offsetPercent = Math.max(0, Math.min(100, ((from - axis.start) / total) * 100));
  const widthPercent = Math.max(2, Math.min(100 - offsetPercent, ((to - from) / total) * 100));
  return { offsetPercent, widthPercent };
}

function isValidCustomRetentionValue(value: string) {
  if (!/^\d+$/.test(value)) return false;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 1 && parsed <= 36500;
}

function editablePolicies(value: RetentionSettingsResponsePolicies | undefined): RetentionSettingsPolicies | null {
  if (!value) return null;
  if (Object.values(value).every((item) => typeof item === "number" || item === null)) {
    return value as RetentionSettingsPolicies;
  }
  const tagged = value as Record<keyof RetentionSettingsPolicies, { value?: number | null; raw_integer?: string }>;
  const editable = (item: { value?: number | null; raw_integer?: string }) => {
    if (item.value !== undefined && item.value !== null) return item.value;
    const parsed = item.raw_integer === undefined ? null : Number(item.raw_integer);
    return Number.isSafeInteger(parsed) ? parsed : null;
  };
  return {
    request_logs_retention_days: editable(tagged.request_logs_retention_days),
    statistics_retention_days: editable(tagged.statistics_retention_days),
    audit_logs_retention_days: editable(tagged.audit_logs_retention_days),
    loadbalance_events_retention_days: editable(tagged.loadbalance_events_retention_days),
  };
}

export function RetentionDeletionSection({
  renderSectionSaveState,
  retentionSettings,
  retentionSettingsDirty,
  retentionSettingsLoading,
  setRetentionDays,
  applyRecommendation,
  onInvalidCustomValueChange,
}: RetentionDeletionSectionProps) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.settingsRetentionDeletion;
  const policies = editablePolicies(retentionSettings?.policies);
  const [customKeys, setCustomKeys] = useState<Set<RetentionSettingKey>>(new Set());
  const [customValues, setCustomValues] = useState<Partial<Record<RetentionSettingKey, string>>>({});
  const hasInvalidCustomValue = Object.entries(customValues).some(([key, value]) =>
    customKeys.has(key as RetentionSettingKey) && value !== undefined && value !== "" && !isValidCustomRetentionValue(value));

  // The save button lives in the page header, so it needs to know whether this
  // card currently holds a value the server would reject.
  useEffect(() => {
    onInvalidCustomValueChange(hasInvalidCustomValue);
  }, [hasInvalidCustomValue, onInvalidCustomValueChange]);

  return (
    <section id="retention" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={(
          <span className="flex items-center gap-2">
            <Trash2 data-icon="inline-start" />
            {copy.retentionPolicyTitle}
          </span>
        )}
        description={copy.retentionPolicyDescription}
        actions={renderSectionSaveState("retention", retentionSettingsDirty)}
        contentClassName="flex flex-col gap-4"
      >
        {retentionSettingsLoading ? (
          <div className="flex flex-col gap-2" aria-hidden="true">
            <Skeleton className="h-9 rounded" />
            <Skeleton className="h-9 rounded" />
            <Skeleton className="h-9 rounded" />
            <Skeleton className="h-9 rounded" />
          </div>
        ) : retentionSettings && policies ? (
          <>
            {retentionSettings.state === "repair_required" ? (
              <OperatorCallout intent="danger" title={copy.repairRequired} description={copy.repairRequiredDescription} />
            ) : null}
            <OperatorInsetPanel title={copy.retentionPolicyTitle} description={copy.retentionPolicyDescription}>
              <div className="grid gap-4 md:grid-cols-2">
                {[
                  { key: "request_logs_retention_days", label: copy.requestLogsPolicy },
                  { key: "statistics_retention_days", label: copy.statisticsPolicy },
                  { key: "audit_logs_retention_days", label: copy.auditLogsPolicy },
                  { key: "loadbalance_events_retention_days", label: copy.loadbalanceEventsPolicy },
                ].map(({ key, label }) => {
                  const settingKey = key as RetentionSettingKey;
                  const value = policies[settingKey];
                  const selectValue = customKeys.has(settingKey) ? "custom" : toRetentionSelectValue(value);
                  return (
                    <Field key={key}>
                      <FieldLabel>{label}</FieldLabel>
                      <Select
                        value={selectValue}
                        onValueChange={(nextValue) => {
                          if (nextValue === "none") {
                            setCustomKeys((current) => { const next = new Set(current); next.delete(settingKey); return next; });
                            setCustomValues((current) => ({ ...current, [settingKey]: "" }));
                            setRetentionDays(settingKey, null);
                          } else if (nextValue === "custom") {
                            setCustomKeys((current) => new Set(current).add(settingKey));
                            setCustomValues((current) => ({ ...current, [settingKey]: value === null ? "" : String(value) }));
                            if (value === null) setRetentionDays(settingKey, 1);
                          } else {
                            setCustomKeys((current) => { const next = new Set(current); next.delete(settingKey); return next; });
                            setCustomValues((current) => ({ ...current, [settingKey]: nextValue }));
                            setRetentionDays(settingKey, Number.parseInt(nextValue, 10));
                          }
                        }}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="none">{copy.noAutomaticCleanup}</SelectItem>
                            {RETENTION_DAY_OPTIONS.map((days) => (
                              <SelectItem key={days} value={String(days)}>{copy.retentionDays(days)}</SelectItem>
                            ))}
                            <SelectItem value="custom">{copy.customRetention}</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      {selectValue === "custom" ? (
                        <Input
                          type="number"
                          min={1}
                          max={36500}
                          step={1}
                          value={customValues[settingKey] ?? (value ?? "")}
                          aria-label={copy.customRetentionValue(label)}
                          onChange={(event) => {
                            const next = event.target.value;
                            setCustomValues((current) => ({ ...current, [settingKey]: next }));
                            if (next === "") {
                              setRetentionDays(settingKey, null);
                              return;
                            }
                            if (isValidCustomRetentionValue(next)) {
                              const parsed = Number(next);
                              setRetentionDays(settingKey, parsed);
                            }
                          }}
                        />
                      ) : null}
                      <FieldDescription>
                        {selectValue === "custom" && customValues[settingKey] !== undefined && customValues[settingKey] !== "" && !isValidCustomRetentionValue(customValues[settingKey] ?? "")
                          ? copy.retentionRangeInvalid
                          : value === null ? copy.noAutomaticCleanupDescription : copy.logicalCutoffDescription}
                      </FieldDescription>
                    </Field>
                  );
                })}
              </div>
              {retentionSettings.recommendations[0] ? (
                <div className="mt-4 flex flex-col gap-2 rounded-lg border border-border bg-panel p-3 sm:flex-row sm:items-center sm:justify-between">
                  {/* 这块嵌在 OperatorInsetPanel（同样是 bg-inset + 边框）里，
                      用 panel 底色往上提一层，才不是同色套同色。 */}
                  <p className="text-sm text-muted-foreground">{copy.recommendationDescription}</p>
                  <Button type="button" variant="outline" size="sm" onClick={applyRecommendation}>{copy.applyRecommendation}</Button>
                </div>
              ) : null}
            </OperatorInsetPanel>
            <OperatorInsetPanel title={copy.actualCoverageTitle} description={copy.actualCoverageDescription}>
              <div className="grid gap-3 md:grid-cols-2">
                {COVERAGE_DATASETS.map(([dataset, labelKey]) => (
                  <CoverageCard
                    key={dataset}
                    axis={coverageAxis(retentionSettings)}
                    copy={copy}
                    coverage={
                      retentionSettings.actual_coverage?.[
                        dataset as keyof RetentionSettingsResponse["actual_coverage"]
                      ] ?? null
                    }
                    format={format}
                    label={copy[labelKey]}
                  />
                ))}
              </div>
            </OperatorInsetPanel>
            <OperatorCallout intent="info" description={copy.coverageConsequence} />
          </>
        ) : (
          <OperatorCallout intent="warning" description={copy.retentionLoadedFailed} />
        )}

      </OperatorSectionCard>
    </section>
  );
}
