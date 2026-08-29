import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import {
  OperatorCallout,
  OperatorDestructiveDialog,
  OperatorInsetPanel,
} from "@/shared/design-system";
import type { ExportSourceModelRow } from "./exportTypes";
import {
  buildPiOverrideFields,
  formatPiBindingMetadataValue,
  PI_OVERRIDE_FIELD_ORDER,
  piBindingMetadataValue,
  piOverrideValueToRaw,
  type PiOverrideDraft,
  type PiOverrideDraftError,
  type PiOverrideField,
} from "./piOverrideDraft";
import {
  isModelExportSourceReconciliationError,
  type ModelExportSourceState,
} from "./useModelExportSource";

type Copy = Record<string, string>;

const FIELD_LABEL_KEYS: Record<PiOverrideField, string> = {
  name: "overrideNameLabel",
  reasoning: "overrideReasoningLabel",
  input: "overrideInputLabel",
  context_window: "overrideContextWindowLabel",
  max_tokens: "overrideMaxTokensLabel",
  thinking_level_map: "overrideThinkingLevelMapLabel",
  compat: "overrideCompatLabel",
};

const ERROR_LABEL_KEYS: Record<PiOverrideDraftError, string> = {
  boolean_required: "overrideBooleanRequired",
  input_invalid: "overrideInputInvalid",
  integer_invalid: "overrideIntegerInvalid",
  json_invalid: "overrideJsonInvalid",
  name_required: "overrideNameRequired",
  object_required: "overrideObjectRequired",
  thinking_map_invalid: "overrideThinkingMapInvalid",
};

function apiErrorDetail(error: unknown, copy: Copy): string {
  if (isModelExportSourceReconciliationError(error)) {
    return copy.sourceReconciliationFailed;
  }
  return error instanceof Error ? error.message : String(error);
}

function valueOrAbsent(value: string | null, copy: Copy): string {
  return value ?? copy.overrideValueAbsent;
}

export function PiBindingOverrideDialog({
  copy,
  model,
  onClose,
  sourceState,
}: {
  copy: Copy;
  model: ExportSourceModelRow;
  onClose: () => void;
  sourceState: ModelExportSourceState;
}) {
  const { clearOverrideMutation, overrideMutation } = sourceState;
  const [draft, setDraft] = useState<PiOverrideDraft>({});
  const [saveError, setSaveError] = useState<string | null>(null);
  const [clearAllError, setClearAllError] = useState<string | null>(null);
  const [clearAllOpen, setClearAllOpen] = useState(false);
  const result = useMemo(() => buildPiOverrideFields(draft), [draft]);
  const hasErrors = Object.keys(result.errors).length > 0;
  const hasChanges = Object.keys(result.fields).length > 0;
  const busy = overrideMutation.isPending || clearOverrideMutation.isPending;

  function setMode(
    field: PiOverrideField,
    mode: "unchanged" | "value" | "restore",
  ) {
    setDraft((current) => {
      if (mode === "unchanged") {
        const next = { ...current };
        delete next[field];
        return next;
      }
      if (mode === "restore") return { ...current, [field]: { mode } };
      const seedMetadata =
        piBindingMetadataValue(model.pi_binding_override, field) !== undefined
          ? model.pi_binding_override
          : model.pi_binding_effective;
      return {
        ...current,
        [field]: {
          mode,
          raw: piOverrideValueToRaw(seedMetadata, field),
        },
      };
    });
  }

  function setValue(field: PiOverrideField, raw: string) {
    setDraft((current) => ({ ...current, [field]: { mode: "value", raw } }));
  }

  async function handleSave() {
    if (!hasChanges || hasErrors) return;
    setSaveError(null);
    try {
      await overrideMutation.mutateAsync({
        modelConfigId: model.model_config_id,
        fields: result.fields,
      });
      onClose();
    } catch (cause) {
      setSaveError(apiErrorDetail(cause, copy));
    }
  }

  async function handleRestoreAll() {
    setClearAllError(null);
    try {
      await clearOverrideMutation.mutateAsync({
        modelConfigId: model.model_config_id,
      });
      setClearAllOpen(false);
      onClose();
    } catch (cause) {
      setClearAllError(apiErrorDetail(cause, copy));
    }
  }

  return (
    <>
      <Dialog
        open
        onOpenChange={(open) => {
          if (!open && !busy) onClose();
        }}
      >
        <DialogContent className="sm:max-w-[720px]">
          <DialogHeader>
            <DialogTitle>{copy.overrideDialogTitle}</DialogTitle>
            <DialogDescription>
              {copy.overrideDialogDescription}
            </DialogDescription>
          </DialogHeader>
          <DialogBody className="max-h-[65vh] overflow-y-auto">
            <FieldGroup className="gap-3">
              {PI_OVERRIDE_FIELD_ORDER.map((field) => {
                const label = copy[FIELD_LABEL_KEYS[field]];
                const entry = draft[field];
                const mode = entry?.mode ?? "unchanged";
                const raw = entry?.mode === "value" ? entry.raw : "";
                const validationError = result.errors[field];
                const errorId = `pi-override-error-${field}`;
                return (
                  <OperatorInsetPanel key={field} title={label}>
                    <div className="grid gap-1 text-xs text-muted-foreground sm:grid-cols-3">
                      <span>
                        {copy.overrideSourceValueLabel}:{" "}
                        {valueOrAbsent(
                          formatPiBindingMetadataValue(
                            model.pi_binding_source,
                            field,
                          ),
                          copy,
                        )}
                      </span>
                      <span>
                        {copy.overrideCurrentValueLabel}:{" "}
                        {valueOrAbsent(
                          formatPiBindingMetadataValue(
                            model.pi_binding_override,
                            field,
                          ),
                          copy,
                        )}
                      </span>
                      <span>
                        {copy.overrideEffectiveValueLabel}:{" "}
                        {valueOrAbsent(
                          formatPiBindingMetadataValue(
                            model.pi_binding_effective,
                            field,
                          ),
                          copy,
                        )}
                      </span>
                    </div>
                    <Field data-invalid={Boolean(validationError) || undefined}>
                      <div className="grid gap-2 sm:grid-cols-[11rem_minmax(0,1fr)]">
                        <div className="flex flex-col gap-1">
                          <FieldLabel htmlFor={`pi-override-mode-${field}`}>
                            {copy.overrideModeLabel}
                          </FieldLabel>
                          <Select
                            value={mode}
                            disabled={sourceState.sourceActionsBlocked}
                            onValueChange={(value) =>
                              setMode(
                                field,
                                value as "unchanged" | "value" | "restore",
                              )
                            }
                          >
                            <SelectTrigger
                              id={`pi-override-mode-${field}`}
                              aria-label={`${label} ${copy.overrideModeLabel}`}
                            >
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectGroup>
                                <SelectItem value="unchanged">
                                  {copy.overrideModeUnchanged}
                                </SelectItem>
                                <SelectItem value="value">
                                  {copy.overrideModeValue}
                                </SelectItem>
                                <SelectItem value="restore">
                                  {copy.overrideModeRestore}
                                </SelectItem>
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </div>
                        <PiOverrideValueEditor
                          copy={copy}
                          disabled={sourceState.sourceActionsBlocked}
                          errorId={validationError ? errorId : undefined}
                          field={field}
                          invalid={Boolean(validationError)}
                          label={label}
                          mode={mode}
                          raw={raw}
                          onChange={(value) => setValue(field, value)}
                        />
                      </div>
                      {validationError ? (
                        <FieldError id={errorId}>
                          {copy[ERROR_LABEL_KEYS[validationError]]}
                        </FieldError>
                      ) : null}
                    </Field>
                  </OperatorInsetPanel>
                );
              })}
            </FieldGroup>
            {saveError ? (
              <OperatorCallout intent="danger" description={saveError} />
            ) : sourceState.sourceQuery.isError ? (
              <OperatorCallout
                intent="danger"
                description={copy.sourceActionsBlocked}
              />
            ) : null}
          </DialogBody>
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={busy}>
              {copy.cancel}
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setClearAllError(null);
                setClearAllOpen(true);
              }}
              disabled={
                busy ||
                !model.pi_binding_override ||
                sourceState.sourceActionsBlocked
              }
            >
              {copy.overrideRestoreAll}
            </Button>
            <Button
              onClick={() => void handleSave()}
              disabled={
                busy ||
                !hasChanges ||
                hasErrors ||
                sourceState.sourceActionsBlocked
              }
            >
              {overrideMutation.isPending ? (
                <Spinner data-icon="inline-start" />
              ) : null}
              {copy.overrideSave}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <OperatorDestructiveDialog
        open={clearAllOpen}
        onOpenChange={(open) => {
          if (!clearOverrideMutation.isPending) setClearAllOpen(open);
        }}
        title={copy.overrideClearAllTitle}
        description={copy.overrideClearAllDescription}
        cancelLabel={copy.cancel}
        confirmLabel={copy.overrideClearAllConfirm}
        confirmingLabel={copy.overrideClearing}
        confirming={clearOverrideMutation.isPending}
        confirmDisabled={sourceState.sourceActionsBlocked}
        cancelDisabled={clearOverrideMutation.isPending}
        confirmTestId="pi-clear-overrides-confirm"
        onCancel={() => setClearAllOpen(false)}
        onConfirm={handleRestoreAll}
      >
        <OperatorCallout
          intent="warning"
          description={copy.overrideClearAllImpact}
        />
        {clearAllError ? (
          <OperatorCallout intent="danger" description={clearAllError} />
        ) : sourceState.sourceQuery.isError ? (
          <OperatorCallout
            intent="danger"
            description={copy.sourceActionsBlocked}
          />
        ) : null}
      </OperatorDestructiveDialog>
    </>
  );
}

function PiOverrideValueEditor({
  copy,
  disabled,
  errorId,
  field,
  invalid,
  label,
  mode,
  onChange,
  raw,
}: {
  copy: Copy;
  disabled: boolean;
  errorId: string | undefined;
  field: PiOverrideField;
  invalid: boolean;
  label: string;
  mode: "unchanged" | "value" | "restore";
  onChange: (value: string) => void;
  raw: string;
}) {
  if (mode !== "value") {
    return (
      <p className="self-end pb-2 text-xs text-muted-foreground">
        {mode === "restore"
          ? copy.overrideWillRestore
          : copy.overrideWillNotChange}
      </p>
    );
  }
  if (field === "reasoning") {
    return (
      <Select value={raw} disabled={disabled} onValueChange={onChange}>
        <SelectTrigger
          className="self-end"
          aria-label={label}
          aria-invalid={invalid || undefined}
          aria-describedby={errorId}
        >
          <SelectValue placeholder={copy.overrideBooleanPlaceholder} />
        </SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="true">{copy.overrideBooleanTrue}</SelectItem>
            <SelectItem value="false">{copy.overrideBooleanFalse}</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
    );
  }
  if (field === "thinking_level_map" || field === "compat") {
    return (
      <Textarea
        aria-label={label}
        aria-invalid={invalid || undefined}
        aria-describedby={errorId}
        className="min-h-24 font-mono text-xs"
        spellCheck={false}
        disabled={disabled}
        value={raw}
        onChange={(event) => onChange(event.target.value)}
      />
    );
  }
  return (
    <Input
      aria-label={label}
      aria-invalid={invalid || undefined}
      aria-describedby={errorId}
      className="self-end"
      inputMode={
        field === "context_window" || field === "max_tokens"
          ? "numeric"
          : undefined
      }
      disabled={disabled}
      placeholder={field === "input" ? copy.overrideInputPlaceholder : undefined}
      value={raw}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}
