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
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { ModelCatalogResponse } from "@/lib/types";
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
  const result = useMemo(() => buildCatalogOverridePatch(draft), [draft]);
  const hasErrors = Object.keys(result.errors).length > 0;

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

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
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
                    <SelectItem value="unchanged">
                      {copy.overrideModeUnchanged}
                    </SelectItem>
                    <SelectItem value="value">
                      {copy.overrideModeValue}
                    </SelectItem>
                    <SelectItem value="restore">
                      {copy.overrideModeRestore}
                    </SelectItem>
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
                          <SelectItem value="true">
                            {copy.overrideBooleanTrue}
                          </SelectItem>
                          <SelectItem value="false">
                            {copy.overrideBooleanFalse}
                          </SelectItem>
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
                          <SelectItem value="alpha">alpha</SelectItem>
                          <SelectItem value="beta">beta</SelectItem>
                          <SelectItem value="deprecated">deprecated</SelectItem>
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
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          {catalog?.bound && (
            <Button
              type="button"
              variant="destructive"
              disabled={busy}
              onClick={() =>
                runAction(
                  () => modelsApi.catalog.clearOverride(modelConfigId),
                  onClose,
                )
              }
            >
              {copy.clearAllOverridesAction}
            </Button>
          )}
          <Button
            type="button"
            disabled={
              busy ||
              (!clearAll &&
                (hasErrors || Object.keys(result.patch).length === 0))
            }
            onClick={() =>
              clearAll
                ? runAction(
                    () => modelsApi.catalog.clearOverride(modelConfigId),
                    onClose,
                  )
                : runAction(
                    () =>
                      modelsApi.catalog.putOverride(
                        modelConfigId,
                        result.patch,
                      ),
                    onClose,
                  )
            }
          >
            {copy.saveOverrideAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
