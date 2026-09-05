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
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system";

export interface KeyDecision {
  mode: "none" | "manual";
  /** Operator-typed key, trimmed at confirmation and non-empty in manual mode. */
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
  /** 已选模型里已知的代价，与页面上的风险摘要同源。 */
  riskSummary?: { costOmitted: number; metadataIncomplete: number };
  error: string | null;
  confirmDisabled?: boolean;
  onClose: () => void;
  onConfirm: (decision: KeyDecision) => Promise<void>;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const [mode, setMode] = useState<KeyDecision["mode"]>("none");
  const [manualKey, setManualKey] = useState("");
  const [busy, setBusy] = useState(false);
  const manualKeyInvalid = mode === "manual" && manualKey.trim() === "";

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
    if (manualKeyInvalid) return;
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
        size="md"
        data-testid="export-key-dialog"
      >
        <DialogHeader>
          <DialogTitle>{copy.keyDialogTitle}</DialogTitle>
          <DialogDescription>{copy.keyDialogDescription}</DialogDescription>
        </DialogHeader>
        {/* 真 <form>：密码框里敲回车必须能提交，而不是必须去够按钮。 */}
        <form
          className="flex min-h-0 flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            void handleConfirm();
          }}
        >
          {/* 最后一次能反悔的步骤：先复述本次导出的范围与已知代价。 */}
          <OperatorInsetPanel title={copy.keyDialogImpactTitle}>
            <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
              <li>
                {copy.keyDialogImpactSelected.replace(
                  "{count}",
                  String(props.selectedCount),
                )}
              </li>
              {props.riskSummary && props.riskSummary.costOmitted > 0 ? (
                <li>
                  {copy.keyDialogImpactCostOmitted.replace(
                    "{count}",
                    String(props.riskSummary.costOmitted),
                  )}
                </li>
              ) : null}
              {props.riskSummary && props.riskSummary.metadataIncomplete > 0 ? (
                <li>
                  {copy.keyDialogImpactMetadataMissing.replace(
                    "{count}",
                    String(props.riskSummary.metadataIncomplete),
                  )}
                </li>
              ) : null}
            </ul>
          </OperatorInsetPanel>
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
            <Field data-invalid={manualKeyInvalid || undefined}>
              <FieldLabel htmlFor="export-manual-key" required>
                {copy.manualKeyLabel}
              </FieldLabel>
              <Input
                id="export-manual-key"
                type="password"
                autoComplete="off"
                value={manualKey}
                aria-required="true"
                aria-invalid={manualKeyInvalid}
                onChange={(event) => setManualKey(event.target.value)}
              />
              <FieldDescription>
                {manualKeyInvalid ? copy.manualKeyRequired : copy.manualKeyHint}
              </FieldDescription>
            </Field>
          )}
          {props.error ? (
            <OperatorCallout intent="danger" description={props.error} />
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={handleClose}
              disabled={busy}
            >
              {copy.cancel}
            </Button>
            <Button
              type="submit"
              disabled={busy || manualKeyInvalid || props.confirmDisabled}
            >
              {busy ? <Spinner data-icon="inline-start" /> : null}
              {busy ? copy.generating : copy.confirmGenerate}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
