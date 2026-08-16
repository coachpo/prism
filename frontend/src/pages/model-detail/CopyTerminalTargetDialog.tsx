import { useEffect, useMemo, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { terminalTargetCopies, type TerminalTargetCopyResponse } from "@/lib/api/observability";
import type { ModelConfigListItem, OpenAITextCapability } from "@/lib/types";
import { OperatorCallout, OperatorSwitchField } from "@/shared/design-system";

/**
 * Batch Terminal Target copy (MC-B4): copy one Terminal Target to selected
 * same-profile same-family (OpenAI: same-mode) destination models in one
 * transaction. New access targets default to not participating in routing;
 * the explicit switch opts them in per destination.
 */
export function CopyTerminalTargetDialog({
  isOpen,
  modelConfigId,
  connectionId,
  connectionName,
  ownerMode,
  models,
  onClose,
  onCopied,
}: {
  isOpen: boolean;
  modelConfigId: number;
  connectionId: number;
  connectionName: string;
  ownerMode: OpenAITextCapability | null | undefined;
  models: ModelConfigListItem[];
  onClose: () => void;
  onCopied: (response: TerminalTargetCopyResponse) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [enableCopies, setEnableCopies] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setSelected(new Set());
    setEnableCopies(false);
    setFormError(null);
    setSubmitting(false);
  }, [isOpen]);

  const candidates = useMemo(
    () =>
      models.filter((model) => model.id !== modelConfigId && (ownerMode == null || model.openai_accepted_format === ownerMode)),
    [models, modelConfigId, ownerMode],
  );
  const modeMismatched = useMemo(
    () =>
      ownerMode == null
        ? []
        : models.filter((model) => model.id !== modelConfigId && model.openai_accepted_format !== ownerMode),
    [models, modelConfigId, ownerMode],
  );

  const handleSubmit = async () => {
    setFormError(null);
    if (selected.size === 0) {
      setFormError(copy.copySelectDestinationRequired);
      return;
    }
    setSubmitting(true);
    try {
      const response = await terminalTargetCopies.create(modelConfigId, connectionId, {
        destination_model_config_ids: Array.from(selected),
        enable_copies: enableCopies,
      });
      onCopied(response);
      onClose();
    } catch (error) {
      setFormError(error instanceof Error ? error.message : copy.copyFailed);
    } finally {
      setSubmitting(false);
    }
  };

  const toggle = (modelId: number) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(modelId)) next.delete(modelId);
      else next.add(modelId);
      return next;
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !submitting && onClose()}>
      <DialogContent data-testid="copy-terminal-target-dialog">
        <DialogHeader>
          <DialogTitle>{copy.copyTargetTitle}</DialogTitle>
          <DialogDescription>
            {copy.copyTargetDescription(connectionName)}
          </DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label>{copy.copyDestinationModels}</Label>
            <div className="flex max-h-72 flex-col gap-1 overflow-y-auto rounded-lg border border-border p-2">
              {candidates.map((model) => (
                <label
                  key={model.id}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-inset"
                >
                  <Checkbox checked={selected.has(model.id)} onCheckedChange={() => toggle(model.id)} aria-label={model.display_name ?? model.model_id} />
                  <span className="min-w-0 flex-1 truncate">{model.display_name ?? model.model_id}</span>
                  <span className="font-mono text-xs text-muted-foreground">{model.model_id}</span>
                  {model.is_enabled === false ? (
                    <span className="text-xs text-muted-foreground">{copy.copyDestinationDisabled}</span>
                  ) : null}
                </label>
              ))}
              {candidates.length === 0 ? (
                <p className="px-2 py-3 text-sm text-muted-foreground">{copy.copyNoCandidates}</p>
              ) : null}
            </div>
          </div>
          {modeMismatched.length > 0 ? (
            <div className="flex flex-col gap-1" data-testid="copy-mode-mismatch-list">
              <p className="text-xs text-muted-foreground">{copy.copyModeMismatchNote}</p>
              {modeMismatched.slice(0, 5).map((model) => (
                <div key={model.id} className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span className="truncate">{model.display_name ?? model.model_id}</span>
                  <span className="text-destructive">{copy.copyModeMismatchLabel}</span>
                </div>
              ))}
            </div>
          ) : null}
          <OperatorSwitchField
            checked={enableCopies}
            onCheckedChange={setEnableCopies}
            label={copy.copyEnableLabel}
            description={copy.copyEnableDescription}
          />
          {formError ? <OperatorCallout intent="danger">{formError}</OperatorCallout> : null}
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={submitting}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button onClick={() => void handleSubmit()} disabled={submitting || selected.size === 0} data-testid="copy-submit">
            {submitting ? "…" : copy.copySubmit}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
