import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import type { RetentionPreflightResponse } from "@/lib/types";
import { OperatorCallout } from "@/shared/design-system";

interface RetentionPolicyPreflightDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  preflight: RetentionPreflightResponse | null;
  phrase: string;
  setPhrase: (value: string) => void;
  valid: boolean;
  preflightSemanticsComplete: boolean;
  submitting: boolean;
  onSubmit: () => Promise<void>;
}

export function RetentionPolicyPreflightDialog({
  open,
  onOpenChange,
  preflight,
  phrase,
  setPhrase,
  valid,
  preflightSemanticsComplete,
  submitting,
  onSubmit,
}: RetentionPolicyPreflightDialogProps) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.settingsRetentionDeletion;
  const dialogCopy = messages.settingsDialogs;

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent aria-describedby="retention-policy-preflight-description">
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <AlertTriangle className="text-degraded" aria-hidden="true" />
            {copy.policyPreflightTitle}
          </AlertDialogTitle>
          <AlertDialogDescription id="retention-policy-preflight-description">
            {copy.policyPreflightDescription}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {preflight ? (
          <div className="flex flex-col gap-3">
            <OperatorCallout intent="warning" description={copy.policyPreflightWarning} />
            {!preflightSemanticsComplete ? <OperatorCallout intent="danger" description={copy.semanticFactsUnavailable} /> : null}
            <div className="grid gap-2 rounded-lg border border-border bg-inset p-4 text-sm">
              {preflight.affected_domains.map((domain) => {
                const count = domain.impact.matched_rows;
                return (
                  <div key={domain.dataset} className="flex flex-col gap-1 border-b border-border pb-3 last:border-0 last:pb-0">
                    <div className="flex flex-wrap justify-between gap-2">
                      <span className="font-medium">{datasetLabel(domain.dataset, dialogCopy)}</span>
                      <span className="text-muted-foreground">
                        {count.accuracy === "unavailable" ? copy.countUnavailable : count.accuracy === "estimated" ? copy.estimatedCount(count.value ?? "-") : count.value ?? "-"}
                      </span>
                    </div>
                    <p className="text-xs text-muted-foreground">{copy.retainedRows}: {formatCount(domain.impact.retained_rows, copy)}</p>
                    <p className="text-xs text-muted-foreground">{copy.preflightCutoff}: {formatSettingTime(domain.impact.resolved_cutoff, format, copy.notAvailable)}</p>
                    <p className="text-xs text-muted-foreground">
                      {copy.coverageAfter}: {formatSettingTime(domain.impact.logical_coverage_after.from_time, format, copy.notAvailable)} → {formatSettingTime(domain.impact.logical_coverage_after.to_time, format, copy.notAvailable)}
                    </p>
                    {domain.impact.physical_reclaim_not_before ? <p className="text-xs text-muted-foreground">{copy.physicalReclaimAt}: {formatSettingTime(domain.impact.physical_reclaim_not_before, format, copy.notAvailable)}</p> : null}
                    {domain.impact.non_cascades.length > 0 ? <p className="text-xs text-muted-foreground">{copy.nonCascades}: {domain.impact.non_cascades.map((item) => datasetLabel(item.dataset, dialogCopy)).join("、")}</p> : null}
                    {domain.impact.warnings.map((warning) => <p key={warning} className="text-xs text-degraded">{warning}</p>)}
                  </div>
                );
              })}
              <p className="text-xs text-muted-foreground">{copy.previewTimestamp(formatSettingTime(preflight.previewed_at, format, copy.notAvailable))}</p>
              <p className="text-xs text-muted-foreground">{copy.expiration(formatSettingTime(preflight.expires_at, format, copy.notAvailable))}</p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="retention-policy-confirm-phrase">{dialogCopy.typeDeleteToProceed(preflight.confirmation_keyword)}</Label>
              <Input
                id="retention-policy-confirm-phrase"
                name="retention_policy_confirm_phrase"
                autoComplete="off"
                value={phrase}
                onChange={(event) => setPhrase(event.target.value)}
                aria-invalid={phrase.length > 0 && !valid ? true : undefined}
              />
            </div>
          </div>
        ) : (
          // A discarded preflight leaves no keyword and no impact facts, so the
          // dialog says the confirmation is void instead of rendering blank.
          <OperatorCallout intent="danger" description={copy.preflightDiscarded} />
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>{dialogCopy.cancel}</AlertDialogCancel>
		  <Button type="button" variant="destructive" disabled={!preflight || !preflightSemanticsComplete || !valid || submitting} onClick={() => void onSubmit()}>
            {submitting ? copy.savingRetention : copy.confirmPolicyChange}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function formatCount(count: RetentionPreflightResponse["affected_domains"][number]["impact"]["retained_rows"], copy: ReturnType<typeof useLocale>["messages"]["settingsRetentionDeletion"]) {
  if (count.accuracy === "unavailable") return copy.countUnavailable;
  if (count.accuracy === "estimated") return copy.estimatedCount(count.value ?? "-");
  return count.value ?? copy.notAvailable;
}

function formatSettingTime(value: string | null | undefined, format: (value: string) => string, fallback: string) {
  return value ? format(value) : fallback;
}

function datasetLabel(dataset: string, copy: ReturnType<typeof useLocale>["messages"]["settingsDialogs"]) {
  switch (dataset) {
    case "request_logs": return copy.cleanupTypeRequests;
    case "usage_request_events": return copy.cleanupTypeStatistics;
    case "audit_logs": return copy.cleanupTypeAudits;
    case "loadbalance_events": return copy.cleanupTypeLoadbalanceEvents;
    default: return dataset;
  }
}
