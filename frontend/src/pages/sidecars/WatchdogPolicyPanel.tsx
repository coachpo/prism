import { useEffect, useMemo, useState, type FormEvent } from "react";
import { AlertTriangle, Loader2, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
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
  manual_override_pause_seconds: string;
};

const DEFAULT_FORM: WatchdogPolicyForm = {
  enabled: true,
  failure_threshold: "3",
  failure_window_seconds: "3600",
  fallback_cooldown_seconds: "86400",
  deprioritized_priority: "0",
  manual_override_pause_seconds: "1800",
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
    manual_override_pause_seconds: String(policy.manual_override_pause_seconds),
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
  error,
  id,
  label,
  min,
  onChange,
  value,
}: {
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
    </div>
  );
}

export function WatchdogPolicyPanel({ loading, onSave, policy, saving }: WatchdogPolicyPanelProps) {
  const [form, setForm] = useState<WatchdogPolicyForm>(() => formFromPolicy(policy));

  useEffect(() => {
    setForm(formFromPolicy(policy));
  }, [policy]);

  const parsed = useMemo(() => ({
    failure_threshold: parseWholeNumber(form.failure_threshold, 1),
    failure_window_seconds: parseWholeNumber(form.failure_window_seconds, 1),
    fallback_cooldown_seconds: parseWholeNumber(form.fallback_cooldown_seconds, 1),
    deprioritized_priority: parseWholeNumber(form.deprioritized_priority, 0),
    manual_override_pause_seconds: parseWholeNumber(form.manual_override_pause_seconds, 1),
  }), [form]);

  const validationError = Object.values(parsed).some((value) => value === null);

  const updateField = (field: keyof WatchdogPolicyForm, value: string | boolean) => {
    setForm((current) => ({ ...current, [field]: value }));
  };

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
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
      manual_override_pause_seconds: parsed.manual_override_pause_seconds ?? undefined,
    });
  };

  return (
    <Card data-testid="sidecar-watchdog-policy">
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <ShieldCheck className="h-4 w-4" />
          Watchdog policy
        </CardTitle>
        <CardDescription className="text-xs">
          Controls when Prism lowers a degraded auth file to the configured fallback priority and how long manual changes pause reconciliation.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="space-y-2">
            <div className="h-12 animate-pulse rounded-md bg-muted/50" />
            <div className="h-28 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : (
          <form className="space-y-4" onSubmit={(event) => void handleSubmit(event)}>
            <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/20 p-4">
              <div className="space-y-1">
                <p className="text-sm font-medium">Enable watchdog</p>
                <p className="text-xs text-muted-foreground">When enabled, Prism can deprioritize quota-failed auth files without disabling refresh.</p>
              </div>
              <Switch checked={form.enabled} onCheckedChange={(checked) => updateField("enabled", checked)} aria-label="Enable watchdog" />
            </div>

            <div className="grid gap-3 md:grid-cols-2">
              <Field id="watchdog-failure-threshold" label="Failure threshold" min={1} value={form.failure_threshold} error={parsed.failure_threshold === null} onChange={(value) => updateField("failure_threshold", value)} />
              <Field id="watchdog-failure-window" label="Failure window (seconds)" min={1} value={form.failure_window_seconds} error={parsed.failure_window_seconds === null} onChange={(value) => updateField("failure_window_seconds", value)} />
              <Field id="watchdog-fallback-cooldown" label="Fallback cooldown (seconds)" min={1} value={form.fallback_cooldown_seconds} error={parsed.fallback_cooldown_seconds === null} onChange={(value) => updateField("fallback_cooldown_seconds", value)} />
              <Field id="watchdog-manual-pause" label="Manual override pause (seconds)" min={1} value={form.manual_override_pause_seconds} error={parsed.manual_override_pause_seconds === null} onChange={(value) => updateField("manual_override_pause_seconds", value)} />
              <Field id="watchdog-deprioritized-priority" label="Deprioritized priority" min={0} value={form.deprioritized_priority} error={parsed.deprioritized_priority === null} onChange={(value) => updateField("deprioritized_priority", value)} />
            </div>

            <Alert className="border-warning/30 bg-warning/10">
              <AlertTriangle className="h-4 w-4" />
              <AlertTitle>Priority 0 safety note</AlertTitle>
              <AlertDescription>
                Priority 0 is the lowest, last-resort priority, not guaranteed exclusion. CLIProxyAPI may still use it if no higher-priority auth is available.
              </AlertDescription>
            </Alert>

            {validationError ? <p className="text-sm text-destructive">Thresholds and timeouts must be positive whole numbers; fallback priority must be zero or greater.</p> : null}

            <Button type="submit" disabled={saving || validationError}>
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
              Save watchdog policy
            </Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
}
