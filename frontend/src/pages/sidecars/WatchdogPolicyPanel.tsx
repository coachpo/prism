import { useEffect, useMemo, useState, type ReactNode, type SyntheticEvent } from "react";
import { AlertTriangle, CheckCircle2, Clock, Loader2, RotateCw, ShieldCheck } from "lucide-react";
import { TypeBadge, ValueBadge } from "@/components/StatusBadge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useLocale } from "@/i18n/useLocale";
import type { SidecarWatchdogPolicy, SidecarWatchdogPolicyRevision, SidecarWatchdogPolicyUpdate } from "@/lib/types";

type WatchdogPolicyFormSource = Omit<SidecarWatchdogPolicyRevision, "id" | "policy_id" | "created_at">;
type WatchdogPolicyFormUpdate = Omit<SidecarWatchdogPolicyUpdate, "expected_revision_id">;

interface WatchdogPolicyPanelProps {
  applying: boolean;
  loading: boolean;
  onApply: () => Promise<void>;
  onSave: (payload: WatchdogPolicyFormUpdate) => Promise<void>;
  policy: SidecarWatchdogPolicy | null;
  saving: boolean;
}

type WatchdogPolicyForm = {
  enabled: boolean;
  watchdog_sweep_interval_seconds: string;
  failure_threshold: string;
  failure_window_seconds: string;
  fallback_cooldown_seconds: string;
  working_priority: string;
  empty_quota_priority: string;
  initial_priority: string;
  error_priority: string;
  manual_override_pause_seconds: string;
  probe_concurrency: string;
  probe_timeout_seconds: string;
  probe_batch_cooldown_seconds: string;
  probe_jitter_min_ms: string;
  probe_jitter_max_ms: string;
  cooldown_jitter_percent: string;
  quota_inventory_enabled: boolean;
  initial_scan_enabled: boolean;
  rolling_refresh_enabled: boolean;
  rolling_refresh_after_seconds: string;
};

const DEFAULT_FORM: WatchdogPolicyForm = {
  enabled: true,
  watchdog_sweep_interval_seconds: "3600",
  failure_threshold: "3",
  failure_window_seconds: "3600",
  fallback_cooldown_seconds: "86400",
  working_priority: "99",
  empty_quota_priority: "90",
  initial_priority: "50",
  error_priority: "10",
  manual_override_pause_seconds: "1800",
  probe_concurrency: "3",
  probe_timeout_seconds: "8",
  probe_batch_cooldown_seconds: "30",
  probe_jitter_min_ms: "100",
  probe_jitter_max_ms: "1000",
  cooldown_jitter_percent: "20",
  quota_inventory_enabled: true,
  initial_scan_enabled: true,
  rolling_refresh_enabled: true,
  rolling_refresh_after_seconds: "3600",
};

function formSourceFromPolicy(policy: SidecarWatchdogPolicy | null): WatchdogPolicyFormSource | null {
  return policy?.pending_revision ?? policy?.active_revision ?? policy;
}

function formFromPolicy(policy: SidecarWatchdogPolicy | null): WatchdogPolicyForm {
  const source = formSourceFromPolicy(policy);
  if (!source) {
    return DEFAULT_FORM;
  }
  return {
    enabled: source.enabled,
    watchdog_sweep_interval_seconds: String(source.watchdog_sweep_interval_seconds),
    failure_threshold: String(source.failure_threshold),
    failure_window_seconds: String(source.failure_window_seconds),
    fallback_cooldown_seconds: String(source.fallback_cooldown_seconds),
    working_priority: String(source.working_priority),
    empty_quota_priority: String(source.empty_quota_priority),
    initial_priority: String(source.initial_priority),
    error_priority: String(source.error_priority),
    manual_override_pause_seconds: String(source.manual_override_pause_seconds),
    probe_concurrency: String(source.probe_concurrency),
    probe_timeout_seconds: String(source.probe_timeout_seconds),
    probe_batch_cooldown_seconds: String(source.probe_batch_cooldown_seconds),
    probe_jitter_min_ms: String(source.probe_jitter_min_ms),
    probe_jitter_max_ms: String(source.probe_jitter_max_ms),
    cooldown_jitter_percent: String(source.cooldown_jitter_percent),
    quota_inventory_enabled: source.quota_inventory_enabled,
    initial_scan_enabled: source.initial_scan_enabled,
    rolling_refresh_enabled: source.rolling_refresh_enabled,
    rolling_refresh_after_seconds: String(source.rolling_refresh_after_seconds),
  };
}

function parseWholeNumber(value: string, min: number) {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) {
    return null;
  }
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) && parsed >= min ? parsed : null;
}

function Field({
  description,
  error,
  id,
  label,
  min,
  onChange,
  value,
}: {
  description?: string;
  error: boolean;
  id: string;
  label: string;
  min: number;
  onChange: (value: string) => void;
  value: string;
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="number"
        min={min}
        step={1}
        value={value}
        aria-invalid={error}
        onChange={(event) => onChange(event.target.value)}
      />
      {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
    </div>
  );
}

function ToggleRow({
  checked,
  description,
  id,
  label,
  onChange,
}: {
  checked: boolean;
  description: string;
  id: string;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/20 p-4">
      <div className="space-y-1">
        <Label htmlFor={id} className="text-sm font-medium">{label}</Label>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onChange} aria-label={label} />
    </div>
  );
}

function SectionHeader({ description, title }: { description: string; title: string }) {
  return (
    <div className="space-y-1">
      <h3 className="text-sm font-medium">{title}</h3>
      <p className="text-xs text-muted-foreground">{description}</p>
    </div>
  );
}

function RevisionStat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-lg border bg-muted/20 p-3">
      <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <div className="mt-2 text-sm font-medium">{value}</div>
    </div>
  );
}

function formatTimestamp(value: string | undefined, locale: string, fallback: string) {
  if (!value) {
    return fallback;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return fallback;
  }
  return date.toLocaleString(locale);
}

export function WatchdogPolicyPanel({ applying, loading, onApply, onSave, policy, saving }: WatchdogPolicyPanelProps) {
  const { locale, messages } = useLocale();
  const copy = messages.sidecarsPage;
  const [form, setForm] = useState<WatchdogPolicyForm>(() => formFromPolicy(policy));

  useEffect(() => {
    setForm(formFromPolicy(policy));
  }, [policy]);

  const parsed = useMemo(() => ({
    watchdog_sweep_interval_seconds: parseWholeNumber(form.watchdog_sweep_interval_seconds, 1),
    failure_threshold: parseWholeNumber(form.failure_threshold, 1),
    failure_window_seconds: parseWholeNumber(form.failure_window_seconds, 1),
    fallback_cooldown_seconds: parseWholeNumber(form.fallback_cooldown_seconds, 1),
    working_priority: parseWholeNumber(form.working_priority, 1),
    empty_quota_priority: parseWholeNumber(form.empty_quota_priority, 1),
    initial_priority: parseWholeNumber(form.initial_priority, 1),
    error_priority: parseWholeNumber(form.error_priority, 1),
    manual_override_pause_seconds: parseWholeNumber(form.manual_override_pause_seconds, 1),
    probe_concurrency: parseWholeNumber(form.probe_concurrency, 1),
    probe_timeout_seconds: parseWholeNumber(form.probe_timeout_seconds, 1),
    probe_batch_cooldown_seconds: parseWholeNumber(form.probe_batch_cooldown_seconds, 1),
    probe_jitter_min_ms: parseWholeNumber(form.probe_jitter_min_ms, 0),
    probe_jitter_max_ms: parseWholeNumber(form.probe_jitter_max_ms, 0),
    cooldown_jitter_percent: parseWholeNumber(form.cooldown_jitter_percent, 0),
    rolling_refresh_after_seconds: parseWholeNumber(form.rolling_refresh_after_seconds, 1),
  }), [form]);

  const priorityOrderError = parsed.working_priority !== null
    && parsed.empty_quota_priority !== null
    && parsed.initial_priority !== null
    && parsed.error_priority !== null
    && (parsed.working_priority < parsed.empty_quota_priority
      || parsed.empty_quota_priority < parsed.initial_priority
      || parsed.initial_priority < parsed.error_priority);
  const batchSizeRangeError = parsed.probe_concurrency !== null && parsed.probe_concurrency > 8;
  const jitterOrderError = parsed.probe_jitter_min_ms !== null
    && parsed.probe_jitter_max_ms !== null
    && parsed.probe_jitter_min_ms > parsed.probe_jitter_max_ms;
  const cooldownJitterRangeError = parsed.cooldown_jitter_percent !== null && parsed.cooldown_jitter_percent > 100;
  const validationError = Object.values(parsed).some((value) => value === null) || priorityOrderError || batchSizeRangeError || jitterOrderError || cooldownJitterRangeError;

  const updateField = (field: keyof WatchdogPolicyForm, value: string | boolean) => {
    setForm((current) => ({ ...current, [field]: value }));
  };

  const handleSubmit = async (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (validationError) {
      return;
    }
    await onSave({
      enabled: form.enabled,
      watchdog_sweep_interval_seconds: parsed.watchdog_sweep_interval_seconds ?? undefined,
      failure_threshold: parsed.failure_threshold ?? undefined,
      failure_window_seconds: parsed.failure_window_seconds ?? undefined,
      fallback_cooldown_seconds: parsed.fallback_cooldown_seconds ?? undefined,
      working_priority: parsed.working_priority ?? undefined,
      empty_quota_priority: parsed.empty_quota_priority ?? undefined,
      initial_priority: parsed.initial_priority ?? undefined,
      error_priority: parsed.error_priority ?? undefined,
      manual_override_pause_seconds: parsed.manual_override_pause_seconds ?? undefined,
      probe_concurrency: parsed.probe_concurrency ?? undefined,
      probe_timeout_seconds: parsed.probe_timeout_seconds ?? undefined,
      probe_batch_cooldown_seconds: parsed.probe_batch_cooldown_seconds ?? undefined,
      probe_jitter_min_ms: parsed.probe_jitter_min_ms ?? undefined,
      probe_jitter_max_ms: parsed.probe_jitter_max_ms ?? undefined,
      cooldown_jitter_percent: parsed.cooldown_jitter_percent ?? undefined,
      quota_inventory_enabled: form.quota_inventory_enabled,
      initial_scan_enabled: form.initial_scan_enabled,
      rolling_refresh_enabled: form.rolling_refresh_enabled,
      rolling_refresh_after_seconds: parsed.rolling_refresh_after_seconds ?? undefined,
    });
  };

  const activeRevision = policy?.active_revision;
  const pendingRevision = policy?.pending_revision;
  const activeSweep = policy?.active_sweep;
  const canApply = Boolean(policy?.has_pending_changes && activeRevision && pendingRevision);
  const revisionModeLabel = policy?.has_pending_changes ? copy.watchdogPendingRevisionLabel : copy.watchdogActiveRevisionLabel;

  return (
    <Card data-testid="sidecar-watchdog-policy">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <ShieldCheck className="h-4 w-4" />
          {copy.watchdogTitle}
        </CardTitle>
        <CardDescription className="text-xs">{copy.watchdogDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-2">
            <div className="h-12 animate-pulse rounded-md bg-muted/50" />
            <div className="h-28 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : (
          <form className="space-y-5" onSubmit={(event) => void handleSubmit(event)}>
            <div className="grid gap-3 md:grid-cols-3">
              <RevisionStat label={copy.watchdogRevisionModeLabel} value={<TypeBadge label={revisionModeLabel} intent={policy?.has_pending_changes ? "warning" : "success"} preserveLabel />} />
              <RevisionStat label={copy.watchdogActiveRevisionIdLabel} value={<ValueBadge label={activeRevision ? `#${activeRevision.id}` : "—"} intent="info" />} />
              <RevisionStat label={copy.watchdogPendingRevisionIdLabel} value={<ValueBadge label={pendingRevision ? `#${pendingRevision.id}` : "—"} intent={pendingRevision ? "warning" : "muted"} />} />
            </div>

            {policy?.has_pending_changes ? (
              <Alert className="border-warning/30 bg-warning/10" data-testid="watchdog-pending-apply">
                <RotateCw className="h-4 w-4" />
                <AlertTitle>{copy.watchdogPendingApplyTitle}</AlertTitle>
                <AlertDescription className="space-y-3">
                  <p>{copy.watchdogPendingApplyDescription}</p>
                  <Button type="button" size="sm" onClick={() => void onApply()} disabled={!canApply || applying || saving} data-testid="watchdog-apply-policy">
                    {applying ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
                    {copy.watchdogApplyPending}
                  </Button>
                </AlertDescription>
              </Alert>
            ) : null}

            {activeSweep ? (
              <Alert className="border-info/30 bg-info/10">
                <Clock className="h-4 w-4" />
                <AlertTitle>{copy.watchdogActiveSweepTitle}</AlertTitle>
                <AlertDescription>
                  {copy.watchdogActiveSweepDescription(
                    activeSweep.status,
                    String(activeSweep.policy_revision_id),
                    String(activeSweep.next_item_index),
                    String(activeSweep.total_items),
                    formatTimestamp(activeSweep.next_batch_after, locale, messages.common.unavailable),
                  )}
                </AlertDescription>
              </Alert>
            ) : null}

            <ToggleRow
              id="watchdog-enabled"
              checked={form.enabled}
              label={copy.watchdogEnabledLabel}
              description={copy.watchdogEnabledDescription}
              onChange={(checked) => updateField("enabled", checked)}
            />

            <SectionHeader title={copy.watchdogSweepSectionTitle} description={copy.watchdogSweepSectionDescription} />
            <div className="grid gap-3 md:grid-cols-2">
              <Field id="watchdog-sweep-interval" label={copy.watchdogSweepIntervalSecondsLabel} min={1} value={form.watchdog_sweep_interval_seconds} error={parsed.watchdog_sweep_interval_seconds === null} description={copy.watchdogSweepIntervalDescription} onChange={(value) => updateField("watchdog_sweep_interval_seconds", value)} />
              <Field id="watchdog-probe-concurrency" label={copy.watchdogProbeConcurrencyLabel} min={1} value={form.probe_concurrency} error={parsed.probe_concurrency === null || batchSizeRangeError} description={copy.watchdogProbeConcurrencyDescription} onChange={(value) => updateField("probe_concurrency", value)} />
              <Field id="watchdog-probe-timeout" label={copy.watchdogProbeTimeoutSecondsLabel} min={1} value={form.probe_timeout_seconds} error={parsed.probe_timeout_seconds === null} onChange={(value) => updateField("probe_timeout_seconds", value)} />
              <Field id="watchdog-probe-batch-cooldown" label={copy.watchdogProbeBatchCooldownSecondsLabel} min={1} value={form.probe_batch_cooldown_seconds} error={parsed.probe_batch_cooldown_seconds === null} description={copy.watchdogProbeBatchCooldownDescription} onChange={(value) => updateField("probe_batch_cooldown_seconds", value)} />
              <Field id="watchdog-probe-jitter-min" label={copy.watchdogProbeJitterMinLabel} min={0} value={form.probe_jitter_min_ms} error={parsed.probe_jitter_min_ms === null || jitterOrderError} onChange={(value) => updateField("probe_jitter_min_ms", value)} />
              <Field id="watchdog-probe-jitter-max" label={copy.watchdogProbeJitterMaxLabel} min={0} value={form.probe_jitter_max_ms} error={parsed.probe_jitter_max_ms === null || jitterOrderError} description={copy.watchdogProbeJitterDescription} onChange={(value) => updateField("probe_jitter_max_ms", value)} />
              <Field id="watchdog-cooldown-jitter" label={copy.watchdogCooldownJitterLabel} min={0} value={form.cooldown_jitter_percent} error={parsed.cooldown_jitter_percent === null || cooldownJitterRangeError} description={copy.watchdogCooldownJitterDescription} onChange={(value) => updateField("cooldown_jitter_percent", value)} />
              <Field id="watchdog-rolling-refresh-after" label={copy.watchdogRollingRefreshAfterSecondsLabel} min={1} value={form.rolling_refresh_after_seconds} error={parsed.rolling_refresh_after_seconds === null} description={copy.watchdogRollingRefreshAfterDescription} onChange={(value) => updateField("rolling_refresh_after_seconds", value)} />
            </div>

            <SectionHeader title={copy.watchdogPriorityBandsSectionTitle} description={copy.watchdogPriorityBandsSectionDescription} />
            <div className="grid gap-3 md:grid-cols-2">
              <Field id="watchdog-working-priority" label={copy.watchdogWorkingPriorityLabel} min={1} value={form.working_priority} error={parsed.working_priority === null || priorityOrderError} description={copy.watchdogWorkingPriorityDescription} onChange={(value) => updateField("working_priority", value)} />
              <Field id="watchdog-empty-quota-priority" label={copy.watchdogEmptyQuotaPriorityLabel} min={1} value={form.empty_quota_priority} error={parsed.empty_quota_priority === null || priorityOrderError} description={copy.watchdogEmptyQuotaPriorityDescription} onChange={(value) => updateField("empty_quota_priority", value)} />
              <Field id="watchdog-initial-priority" label={copy.watchdogInitialPriorityLabel} min={1} value={form.initial_priority} error={parsed.initial_priority === null || priorityOrderError} description={copy.watchdogInitialPriorityDescription} onChange={(value) => updateField("initial_priority", value)} />
              <Field id="watchdog-error-priority" label={copy.watchdogErrorPriorityLabel} min={1} value={form.error_priority} error={parsed.error_priority === null || priorityOrderError} description={copy.watchdogErrorPriorityDescription} onChange={(value) => updateField("error_priority", value)} />
            </div>

            <SectionHeader title={copy.watchdogAutomationSectionTitle} description={copy.watchdogAutomationSectionDescription} />
            <div className="grid gap-3">
              <ToggleRow id="watchdog-quota-inventory-enabled" checked={form.quota_inventory_enabled} label={copy.watchdogQuotaInventoryEnabledLabel} description={copy.watchdogQuotaInventoryEnabledDescription} onChange={(checked) => updateField("quota_inventory_enabled", checked)} />
              <ToggleRow id="watchdog-initial-scan-enabled" checked={form.initial_scan_enabled} label={copy.watchdogInitialScanEnabledLabel} description={copy.watchdogInitialScanEnabledDescription} onChange={(checked) => updateField("initial_scan_enabled", checked)} />
              <ToggleRow id="watchdog-rolling-refresh-enabled" checked={form.rolling_refresh_enabled} label={copy.watchdogRollingRefreshEnabledLabel} description={copy.watchdogRollingRefreshEnabledDescription} onChange={(checked) => updateField("rolling_refresh_enabled", checked)} />
            </div>

            <Alert className="border-warning/30 bg-warning/10">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>{copy.watchdogPrioritySafetyTitle}</AlertTitle>
              <AlertDescription>{copy.watchdogPrioritySafetyDescription}</AlertDescription>
            </Alert>

            {validationError ? (
              <p className="text-sm text-destructive">
                {jitterOrderError ? copy.watchdogJitterOrderValidationError : cooldownJitterRangeError ? copy.watchdogCooldownJitterValidationError : batchSizeRangeError ? copy.watchdogBatchSizeValidationError : priorityOrderError ? copy.watchdogPriorityOrderValidationError : copy.watchdogValidationError}
              </p>
            ) : null}

            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={saving || applying || validationError}>
                {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                {copy.watchdogSave}
              </Button>
              <p className="text-xs text-muted-foreground">{copy.watchdogSaveCreatesPending}</p>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
