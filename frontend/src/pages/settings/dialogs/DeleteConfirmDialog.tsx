import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  getCleanupRetentionLabel,
  getCleanupTypeLabel,
  type DeleteCleanupType,
} from "../manualCleanup";
import type { RetentionPreflightResponse } from "@/lib/types";
import { useTimezone } from "@/hooks/useTimezone";
import { OperatorCallout, OperatorDestructiveDialog } from "@/shared/design-system";

interface DeleteConfirmDialogProps {
  deleteConfirm: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null;
  displayedDeleteConfirm?: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null;
  open?: boolean;
  setDeleteConfirm: (confirm: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null) => void;
  deleteConfirmPhrase: string;
  setDeleteConfirmPhrase: (phrase: string) => void;
  handleBatchDelete: () => Promise<void>;
  deleting: boolean;
  isDeletePhraseValid: boolean;
  preflightSemanticsComplete: boolean;
  preflight?: RetentionPreflightResponse | null;
}

export function DeleteConfirmDialog({
  deleteConfirm,
  displayedDeleteConfirm,
  open,
  setDeleteConfirm,
  deleteConfirmPhrase,
  setDeleteConfirmPhrase,
  handleBatchDelete,
  deleting,
  isDeletePhraseValid,
  preflightSemanticsComplete,
  preflight,
}: DeleteConfirmDialogProps) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.settingsDialogs;
  const dialogConfirm = displayedDeleteConfirm ?? deleteConfirm;
  const dialogOpen = open ?? Boolean(deleteConfirm);
  const cleanupTypeLabel = dialogConfirm ? getCleanupTypeLabel(dialogConfirm.type) : "-";
  const retentionLabel = dialogConfirm
    ? getCleanupRetentionLabel(dialogConfirm.deleteAll, dialogConfirm.days)
    : "-";
  const impact = preflight?.affected_domains[0]?.impact;
  const matchedRows = impact?.matched_rows;
  const retainedRows = impact?.retained_rows;

  const resetDialog = () => {
    setDeleteConfirm(null);
    setDeleteConfirmPhrase("");
  };

  return (
    <OperatorDestructiveDialog
      open={dialogOpen}
      onOpenChange={(open) => {
        if (!open) {
          resetDialog();
        }
      }}
      title={copy.deleteConfirmTitle}
      description={copy.deleteConfirmDescription}
      cancelLabel={copy.cancel}
      confirmLabel={copy.delete}
      confirmingLabel={copy.deleting}
      confirming={deleting}
      confirmDisabled={!preflightSemanticsComplete || !isDeletePhraseValid}
      onCancel={resetDialog}
      onConfirm={handleBatchDelete}
      contentClassName="sm:max-w-md"
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-3 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
          <p className="text-sm font-medium text-foreground">{copy.deletionSummary}</p>
          <dl className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <dt className="text-xs font-medium text-muted-foreground">{copy.dataType}</dt>
              <dd className="text-sm font-medium text-foreground">{cleanupTypeLabel}</dd>
            </div>
            <div className="flex flex-col gap-1">
              <dt className="text-xs font-medium text-muted-foreground">{copy.retention}</dt>
              <dd className="text-sm font-medium text-foreground">{retentionLabel}</dd>
            </div>
          </dl>
          {impact ? (
            <div className="grid gap-2 border-t border-destructive/20 pt-3 text-sm sm:grid-cols-2">
              <div><span className="text-muted-foreground">{copy.impactRows}：</span>{formatImpactCount(matchedRows?.value, matchedRows?.accuracy, copy)}</div>
              <div><span className="text-muted-foreground">{copy.retainedRows}：</span>{formatImpactCount(retainedRows?.value, retainedRows?.accuracy, copy)}</div>
              <div className="sm:col-span-2 text-muted-foreground">{copy.previewTimestamp(preflight ? format(preflight.previewed_at) : "-")}</div>
              {impact.non_cascades.map((item) => (
                <div key={item.dataset} className="sm:col-span-2 text-muted-foreground">{copy.nonCascade(item.dataset)}</div>
              ))}
              {impact.warnings.map((warning) => <div key={warning} className="sm:col-span-2 text-muted-foreground">{warning}</div>)}
            </div>
          ) : null}
          {preflight && !preflightSemanticsComplete ? <p className="text-sm text-destructive" role="alert">{messages.settingsRetentionDeletion.semanticFactsUnavailable}</p> : null}
        </div>

        {/* The keyword comes from the preflight, so without one there is
            nothing the operator could type that the server would accept. */}
        {preflight ? (
          <div className="flex flex-col gap-3 rounded-md border border-border bg-inset p-4">
            <div className="flex flex-col gap-2">
              <Label htmlFor="delete-confirm-phrase">{copy.typeDeleteToProceed(preflight.confirmation_keyword)}</Label>
              <code className="inline-flex w-fit items-center rounded-md border border-border bg-panel px-2.5 py-1.5 text-sm font-medium text-foreground">
                {preflight.confirmation_keyword}
              </code>
            </div>

            <Input
              id="delete-confirm-phrase"
              name="delete_confirm_phrase"
              autoComplete="off"
              value={deleteConfirmPhrase}
              onChange={(event) => setDeleteConfirmPhrase(event.target.value)}
              placeholder={preflight.confirmation_keyword}
            />
          </div>
        ) : (
          <OperatorCallout intent="danger" description={messages.settingsRetentionDeletion.preflightDiscarded} />
        )}
      </div>
    </OperatorDestructiveDialog>
  );
}

function formatImpactCount(
  value: string | null | undefined,
  accuracy: string | undefined,
  copy: ReturnType<typeof useLocale>["messages"]["settingsDialogs"],
) {
  if (!value || accuracy === "unavailable") return copy.countUnavailable;
  return accuracy === "estimated" ? copy.estimatedCount(value) : value;
}
