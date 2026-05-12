import { useEffect, useMemo, useState, type SyntheticEvent } from "react";
import { AlertTriangle, Loader2, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useLocale } from "@/i18n/useLocale";
import type { SidecarWatchdogPolicy, SidecarWatchdogPolicyUpdate } from "@/lib/types";

interface WatchdogPolicyPanelProps {
  loading: boolean;
  onSave: (payload: SidecarWatchdogPolicyUpdate) => Promise<void>;
  policy: SidecarWatchdogPolicy | null;
  saving: boolean;
}

type WatchdogPolicyForm = {
  enabled: boolean;
  failure_threshold: string;
  failure_window_seconds: string;
  fallback_cooldown_seconds: string;
  deprioritized_priority: string;
  prioritized_priority: string;
  manual_override_pause_seconds: string;
  probe_batch_size: string;
  probe_timeout_seconds: string;
  probe_batch_cooldown_seconds: string;
  quota_inventory_enabled: boolean;
  initial_scan_enabled: boolean;
  rolling_refresh_enabled: boolean;
  rolling_refresh_after_seconds: string;
};

const DEFAULT_FORM: WatchdogPolicyForm = {
  enabled: true,
  failure_threshold: "3",
  failure_window_seconds: "3600",
  fallback_cooldown_seconds: "86400",
  deprioritized_priority: "0",
  prioritized_priority: "1",
  manual_override_pause_seconds: "1800",
  probe_batch_size: "3",
  probe_timeout_seconds: "8",
  probe_batch_cooldown_seconds: "30",
  quota_inventory_enabled: true,
  initial_scan_enabled: true,
  rolling_refresh_enabled: true,
  rolling_refresh_after_seconds: "86400",
};

function formFromPolicy(policy: SidecarWatchdogPolicy | null): WatchdogPolicyForm {
  if (!policy) {
    return DEFAULT_FORM;
  }
  return {
    enabled: policy.enabled,
    failure_threshold: String(policy.failure_threshold),
    failure_window_seconds: String(policy.failure_window_seconds),
    fallback_cooldown_seconds: String(policy.fallback_cooldown_seconds),
    deprioritized_priority: String(policy.deprioritized_priority),
    prioritized_priority: String(policy.prioritized_priority),
    manual_override_pause_seconds: String(policy.manual_override_pause_seconds),
    probe_batch_size: String(policy.probe_batch_size),
    probe_timeout_seconds: String(policy.probe_timeout_seconds),
    probe_batch_cooldown_seconds: String(policy.probe_batch_cooldown_seconds),
    quota_inventory_enabled: policy.quota_inventory_enabled,
    initial_scan_enabled: policy.initial_scan_enabled,
    rolling_refresh_enabled: policy.rolling_refresh_enabled,
    rolling_refresh_after_seconds: String(policy.rolling_refresh_after_seconds),
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

export function WatchdogPolicyPanel({ loading, onSave, policy, saving }: WatchdogPolicyPanelProps) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;
  const [form, setForm] = useState<WatchdogPolicyForm>(() => formFromPolicy(policy));

  useEffect(() => {
    setForm(formFromPolicy(policy));
  }, [policy]);

  const parsed = useMemo(() => ({
    failure_threshold: parseWholeNumber(form.failure_threshold, 1),
    failure_window_seconds: parseWholeNumber(form.failure_window_seconds, 1),
    fallback_cooldown_seconds: parseWholeNumber(form.fallback_cooldown_seconds, 1),
    deprioritized_priority: parseWholeNumber(form.deprioritized_priority, 0),
    prioritized_priority: parseWholeNumber(form.prioritized_priority, 0),
    manual_override_pause_seconds: parseWholeNumber(form.manual_override_pause_seconds, 1),
    probe_batch_size: parseWholeNumber(form.probe_batch_size, 1),
    probe_timeout_seconds: parseWholeNumber(form.probe_timeout_seconds, 1),
    probe_batch_cooldown_seconds: parseWholeNumber(form.probe_batch_cooldown_seconds, 1),
    rolling_refresh_after_seconds: parseWholeNumber(form.rolling_refresh_after_seconds, 1),
  }), [form]);

  const priorityOrderError = parsed.deprioritized_priority !== null
    && parsed.prioritized_priority !== null
    && parsed.deprioritized_priority >= parsed.prioritized_priority;
  const validationError = Object.values(parsed).some((value) => value === null) || priorityOrderError;

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
      failure_threshold: parsed.failure_threshold ?? undefined,
      failure_window_seconds: parsed.failure_window_seconds ?? undefined,
      fallback_cooldown_seconds: parsed.fallback_cooldown_seconds ?? undefined,
      deprioritized_priority: parsed.deprioritized_priority ?? undefined,
      prioritized_priority: parsed.prioritized_priority ?? undefined,
      manual_override_pause_seconds: parsed.manual_override_pause_seconds ?? undefined,
      probe_batch_size: parsed.probe_batch_size ?? undefined,
      probe_timeout_seconds: parsed.probe_timeout_seconds ?? undefined,
      probe_batch_cooldown_seconds: parsed.probe_batch_cooldown_seconds ?? undefined,
      quota_inventory_enabled: form.quota_inventory_enabled,
      initial_scan_enabled: form.initial_scan_enabled,
      rolling_refresh_enabled: form.rolling_refresh_enabled,
      rolling_refresh_after_seconds: parsed.rolling_refresh_after_seconds ?? undefined,
    });
  };

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
          <form className="space-y-4" onSubmit={(event) => void handleSubmit(event)}>
            <ToggleRow
              id="watchdog-enabled"
              checked={form.enabled}
              label={copy.watchdogEnabledLabel}
              description={copy.watchdogEnabledDescription}
              onChange={(checked) => updateField("enabled", checked)}
            />

            <div className="grid gap-3 md:grid-cols-2">
              <Field id="watchdog-failure-threshold" label={copy.watchdogFailureThresholdLabel} min={1} value={form.failure_threshold} error={parsed.failure_threshold === null} onChange={(value) => updateField("failure_threshold", value)} />
              <Field id="watchdog-failure-window" label={copy.watchdogFailureWindowLabel} min={1} value={form.failure_window_seconds} error={parsed.failure_window_seconds === null} onChange={(value) => updateField("failure_window_seconds", value)} />
              <Field id="watchdog-fallback-cooldown" label={copy.watchdogFallbackCooldownLabel} min={1} value={form.fallback_cooldown_seconds} error={parsed.fallback_cooldown_seconds === null} onChange={(value) => updateField("fallback_cooldown_seconds", value)} />
              <Field id="watchdog-manual-pause" label={copy.watchdogManualPauseLabel} min={1} value={form.manual_override_pause_seconds} error={parsed.manual_override_pause_seconds === null} onChange={(value) => updateField("manual_override_pause_seconds", value)} />
              <Field id="watchdog-deprioritized-priority" label={copy.watchdogDeprioritizedPriorityLabel} min={0} value={form.deprioritized_priority} error={parsed.deprioritized_priority === null || priorityOrderError} onChange={(value) => updateField("deprioritized_priority", value)} />
              <Field id="watchdog-prioritized-priority" label={copy.watchdogPrioritizedPriorityLabel} min={0} value={form.prioritized_priority} error={parsed.prioritized_priority === null || priorityOrderError} description={copy.watchdogPrioritizedPriorityDescription} onChange={(value) => updateField("prioritized_priority", value)} />
              <Field id="watchdog-probe-batch-size" label={copy.watchdogProbeBatchSizeLabel} min={1} value={form.probe_batch_size} error={parsed.probe_batch_size === null} onChange={(value) => updateField("probe_batch_size", value)} />
              <Field id="watchdog-probe-timeout" label={copy.watchdogProbeTimeoutSecondsLabel} min={1} value={form.probe_timeout_seconds} error={parsed.probe_timeout_seconds === null} onChange={(value) => updateField("probe_timeout_seconds", value)} />
              <Field id="watchdog-probe-batch-cooldown" label={copy.watchdogProbeBatchCooldownSecondsLabel} min={1} value={form.probe_batch_cooldown_seconds} error={parsed.probe_batch_cooldown_seconds === null} description={copy.watchdogProbeBatchCooldownDescription} onChange={(value) => updateField("probe_batch_cooldown_seconds", value)} />
              <Field id="watchdog-rolling-refresh-after" label={copy.watchdogRollingRefreshAfterSecondsLabel} min={1} value={form.rolling_refresh_after_seconds} error={parsed.rolling_refresh_after_seconds === null} description={copy.watchdogRollingRefreshAfterDescription} onChange={(value) => updateField("rolling_refresh_after_seconds", value)} />
            </div>

            <div className="grid gap-3">
              <ToggleRow
                id="watchdog-quota-inventory-enabled"
                checked={form.quota_inventory_enabled}
                label={copy.watchdogQuotaInventoryEnabledLabel}
                description={copy.watchdogQuotaInventoryEnabledDescription}
                onChange={(checked) => updateField("quota_inventory_enabled", checked)}
              />
              <ToggleRow
                id="watchdog-initial-scan-enabled"
                checked={form.initial_scan_enabled}
                label={copy.watchdogInitialScanEnabledLabel}
                description={copy.watchdogInitialScanEnabledDescription}
                onChange={(checked) => updateField("initial_scan_enabled", checked)}
              />
              <ToggleRow
                id="watchdog-rolling-refresh-enabled"
                checked={form.rolling_refresh_enabled}
                label={copy.watchdogRollingRefreshEnabledLabel}
                description={copy.watchdogRollingRefreshEnabledDescription}
                onChange={(checked) => updateField("rolling_refresh_enabled", checked)}
              />
            </div>

            <Alert className="border-warning/30 bg-warning/10">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>{copy.watchdogPrioritySafetyTitle}</AlertTitle>
              <AlertDescription>{copy.watchdogPrioritySafetyDescription}</AlertDescription>
            </Alert>

            {validationError ? (
              <p className="text-sm text-destructive">
                {priorityOrderError ? copy.watchdogPriorityOrderValidationError : copy.watchdogValidationError}
              </p>
            ) : null}

            <Button type="submit" disabled={saving || validationError}>
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              {copy.watchdogSave}
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
