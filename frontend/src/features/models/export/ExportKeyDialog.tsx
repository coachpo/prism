import { useEffect, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

export interface KeyDecision {
  mode: "none" | "manual";
  /** Operator-typed key, trimmed at confirmation; an explicit empty key stays explicit. */
  manualKey: string;
}

/**
 * The final credential dialog. Whatever key ends up in the generated file is
 * chosen and trimmed here (manual mode trims on confirm); nothing is
 * persisted anywhere else.
 */
export function ExportKeyDialog(props: {
  open: boolean;
  selectedCount: number;
  onClose: () => void;
  onConfirm: (decision: KeyDecision) => Promise<void>;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const [mode, setMode] = useState<KeyDecision["mode"]>("none");
  const [manualKey, setManualKey] = useState("");
  const [busy, setBusy] = useState(false);

  const clearCredential = () => {
    setMode("none");
    setManualKey("");
  };

  useEffect(() => {
    if (!props.open) {
      setMode("none");
      setManualKey("");
    }
  }, [props.open]);

  const handleClose = () => {
    clearCredential();
    props.onClose();
  };

  const handleConfirm = async () => {
    setBusy(true);
    try {
      await props.onConfirm({ mode, manualKey: manualKey.trim() });
      handleClose();
    } catch {
      // Render failures stay on the page; the dialog stays open.
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) {
          handleClose();
        }
      }}
    >
      <DialogContent
        className="sm:max-w-[560px]"
        data-testid="export-key-dialog"
      >
        <DialogHeader>
          <DialogTitle>{copy.keyDialogTitle}</DialogTitle>
          <DialogDescription>{copy.keyDialogDescription}</DialogDescription>
        </DialogHeader>
        <FieldSet>
          <FieldLegend variant="label">{copy.keyModeLegend}</FieldLegend>
          <FieldGroup className="gap-3">
            {(
              [
                {
                  value: "none",
                  title: copy.keyModeNone,
                  hint: copy.keyModeNoneHint,
                },
                {
                  value: "manual",
                  title: copy.keyModeManual,
                  hint: copy.keyModeManualHint,
                },
              ] as const
            ).map((option) => (
              <Field key={option.value} orientation="horizontal">
                <input
                  id={`export-key-mode-${option.value}`}
                  type="radio"
                  name="export-key-mode"
                  checked={mode === option.value}
                  onChange={() => setMode(option.value)}
                />
                <FieldLabel
                  htmlFor={`export-key-mode-${option.value}`}
                  className="flex-col items-start gap-0"
                >
                  <span>{option.title}</span>
                  <span className="text-xs font-normal text-muted-foreground">
                    {option.hint}
                  </span>
                </FieldLabel>
              </Field>
            ))}
          </FieldGroup>
        </FieldSet>
        {mode === "manual" && (
          <Field>
            <FieldLabel htmlFor="export-manual-key">
              {copy.manualKeyLabel}
            </FieldLabel>
            <Input
              id="export-manual-key"
              type="password"
              autoComplete="off"
              value={manualKey}
              onChange={(event) => setManualKey(event.target.value)}
            />
            <FieldDescription>{copy.manualKeyHint}</FieldDescription>
          </Field>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={busy}>
            {copy.cancel}
          </Button>
          <Button onClick={() => void handleConfirm()} disabled={busy}>
            {busy ? <Spinner data-icon="inline-start" /> : null}
            {busy ? copy.generating : copy.confirmGenerate}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
