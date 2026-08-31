import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { ModelCatalogResponse } from "@/lib/types";
import { OperatorCallout } from "@/shared/design-system";
import {
  CATALOG_FIELD_KINDS,
  CATALOG_FIELD_ORDER,
  catalogFieldLabel,
  renderCatalogFieldValue,
  type CatalogFieldKey,
} from "./catalogMetadataPresentation";
import {
  buildCatalogOverridePatch,
  catalogOverrideValueToRaw,
  type CatalogOverrideDraft,
} from "./catalogOverrideDraft";

type CatalogActionRunner = (
  action: () => Promise<unknown>,
  done?: () => void,
  onError?: (message: string) => void,
) => Promise<void>;

export function CatalogOverrideDialog({
  catalog,
  modelConfigId,
  busy,
  onClose,
  runAction,
}: {
  modelConfigId: number;
  catalog: ModelCatalogResponse | null;
  busy: boolean;
  onClose: () => void;
  runAction: CatalogActionRunner;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [draft, setDraft] = useState<CatalogOverrideDraft>({});
  const [clearAll, setClearAll] = useState(false);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const result = useMemo(() => buildCatalogOverridePatch(draft), [draft]);
  const hasErrors = Object.keys(result.errors).length > 0;
  // Freeze the binding the dialog was opened against. A failed mutation
  // triggers an authoritative parent re-read; keeping this snapshot prevents
  // the still-open draft from silently retargeting itself to a newly rebound
  // offering on the operator's next click.
  const [bindingSnapshot] = useState(() => ({
    providerId: catalog?.provider_id?.trim() ?? "",
    catalogModelId: catalog?.catalog_model_id?.trim() ?? "",
    bindingUpdatedAt: catalog?.updated_at ?? "",
  }));
  const { providerId, catalogModelId, bindingUpdatedAt } = bindingSnapshot;
  const writeSnapshotComplete = Boolean(
    catalog?.bound && providerId && catalogModelId,
  );
  const clearSnapshotComplete = Boolean(
    writeSnapshotComplete && bindingUpdatedAt,
  );

  const setMode = (
    key: CatalogFieldKey,
    mode: "unchanged" | "value" | "restore",
  ) => {
    setDraft((current) => {
      if (mode === "unchanged") {
        const next = { ...current };
        delete next[key];
        return next;
      }
      if (mode === "restore") return { ...current, [key]: { mode } };
      return {
        ...current,
        [key]: {
          mode,
          raw: catalogOverrideValueToRaw(
            catalog?.override ?? catalog?.effective ?? null,
            key,
          ),
        },
      };
    });
  };

  const setValue = (key: CatalogFieldKey, raw: string) => {
    setDraft((current) => ({ ...current, [key]: { mode: "value", raw } }));
  };

  async function runMutation(action: () => Promise<unknown>) {
    setMutationError(null);
    await runAction(action, onClose, setMutationError);
  }

  function putOverride() {
    if (!writeSnapshotComplete) {
      return Promise.reject(new Error(copy.bindingSnapshotIncomplete));
    }
    return modelsApi.catalog.putOverride(modelConfigId, {
      expected_provider_id: providerId,
      expected_catalog_model_id: catalogModelId,
      override: result.patch,
    });
  }

  function clearOverride() {
    if (!clearSnapshotComplete) {
      return Promise.reject(new Error(copy.bindingSnapshotIncomplete));
    }
    return modelsApi.catalog.clearOverride(modelConfigId, {
      expected_provider_id: providerId,
      expected_catalog_model_id: catalogModelId,
      expected_binding_updated_at: bindingUpdatedAt,
    });
  }

  return (
    <Dialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{copy.overrideDialogTitle}</DialogTitle>
          <DialogDescription>{copy.overrideDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex max-h-[65vh] flex-col gap-[var(--density-inline-gap)] overflow-y-auto">
          {CATALOG_FIELD_ORDER.map((key) => {
            const entry = draft[key];
            const mode = entry?.mode ?? "unchanged";
            const raw = entry?.mode === "value" ? entry.raw : "";
            const kind = CATALOG_FIELD_KINDS[key];
            return (
              <div
                key={key}
                className="grid gap-2 rounded-md border border-border p-3 sm:grid-cols-[minmax(9rem,1fr)_10rem_minmax(12rem,1.5fr)]"
              >
                <div className="flex min-w-0 flex-col gap-0.5">
                  <Label htmlFor={`override-mode-${key}`}>
                    {catalogFieldLabel(copy, key)}
                  </Label>
                  <span className="truncate text-xs text-muted-foreground">
                    {copy.overridePlaceholderSource(
                      renderCatalogFieldValue(catalog?.source ?? null, key),
                    )}
                  </span>
                </div>
                <Select
                  value={mode}
                  onValueChange={(value) =>
                    setMode(
                      key,
                      value as "unchanged" | "value" | "restore",
                    )
                  }
                >
                  <SelectTrigger id={`override-mode-${key}`}>
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
                <div className="flex min-w-0 flex-col gap-1">
                  {mode === "value" ? (
                    kind === "boolean" ? (
                      <Select
                        value={raw}
                        onValueChange={(value) => setValue(key, value)}
                      >
                        <SelectTrigger aria-label={catalogFieldLabel(copy, key)}>
                          <SelectValue placeholder={copy.overrideBooleanPlaceholder} />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="true">
                              {copy.overrideBooleanTrue}
                            </SelectItem>
                            <SelectItem value="false">
                              {copy.overrideBooleanFalse}
                            </SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    ) : kind === "status" ? (
                      <Select
                        value={raw}
                        onValueChange={(value) => setValue(key, value)}
                      >
                        <SelectTrigger aria-label={catalogFieldLabel(copy, key)}>
                          <SelectValue placeholder={copy.overrideStatusPlaceholder} />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            <SelectItem value="alpha">alpha</SelectItem>
                            <SelectItem value="beta">beta</SelectItem>
                            <SelectItem value="deprecated">deprecated</SelectItem>
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input
                        aria-label={catalogFieldLabel(copy, key)}
                        inputMode={kind === "integer" ? "numeric" : undefined}
                        maxLength={
                          kind === "string" || kind === "date" ? 500 : undefined
                        }
                        placeholder={
                          kind === "string_list"
                            ? copy.overrideListPlaceholder
                            : renderCatalogFieldValue(
                                catalog?.effective ?? null,
                                key,
                              ) ?? ""
                        }
                        value={raw}
                        onChange={(event) => setValue(key, event.target.value)}
                      />
                    )
                  ) : (
                    <span className="self-center text-xs text-muted-foreground">
                      {mode === "restore"
                        ? copy.overrideWillRestore
                        : copy.overrideWillNotChange}
                    </span>
                  )}
                  {result.errors[key] ? (
                    <span className="text-xs text-destructive">
                      {result.errors[key]}
                    </span>
                  ) : null}
                </div>
              </div>
            );
          })}
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={clearAll}
              onCheckedChange={(checked) => setClearAll(checked === true)}
            />
            {copy.clearAllOverridesLabel}
          </label>
          <p className="text-xs text-muted-foreground">
            {copy.overrideDisplayNameNote}
          </p>
          {mutationError ? (
            <OperatorCallout intent="danger" description={mutationError} />
          ) : null}
          {catalog?.bound && !writeSnapshotComplete ? (
            <OperatorCallout
              intent="danger"
              description={copy.bindingSnapshotIncomplete}
            />
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
            {messages.settingsDialogs.cancel}
          </Button>
          {catalog?.bound && (
            <Button
              type="button"
              variant="destructive"
              disabled={busy || !clearSnapshotComplete}
              onClick={() =>
                void runMutation(clearOverride)
              }
            >
              {copy.clearAllOverridesAction}
            </Button>
          )}
          <Button
            type="button"
            disabled={
              busy ||
              (clearAll
                ? !clearSnapshotComplete
                : !writeSnapshotComplete) ||
              (!clearAll &&
                (hasErrors || Object.keys(result.patch).length === 0))
            }
            onClick={() =>
              clearAll
                ? void runMutation(clearOverride)
                : void runMutation(putOverride)
            }
          >
            {copy.saveOverrideAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
