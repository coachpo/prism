import { useMemo, useState } from "react";

import { Undo2 } from "lucide-react";

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
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { ModelCatalogResponse } from "@/lib/types";
import {
  CATALOG_OVERRIDE_TEXT_FIELDS,
  catalogFieldLabel,
  renderCatalogFieldValue,
} from "./catalogMetadataPresentation";

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
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [clearAll, setClearAll] = useState(false);

  const patch = useMemo(() => {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(draft)) {
      if (value === "") {
        result[key] = null;
      } else if (
        key === "limit_context" ||
        key === "limit_input" ||
        key === "limit_output"
      ) {
        const parsed = Number(value);
        if (!Number.isInteger(parsed) || parsed < 0) continue;
        result[key] = parsed;
      } else {
        result[key] = value;
      }
    }
    return result;
  }, [draft]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{copy.overrideDialogTitle}</DialogTitle>
          <DialogDescription>{copy.overrideDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex max-h-[60vh] flex-col gap-[var(--density-inline-gap)] overflow-y-auto">
          {CATALOG_OVERRIDE_TEXT_FIELDS.map((key) => (
            <div key={key} className="flex items-end gap-2">
              <div className="flex grow flex-col gap-1">
                <Label htmlFor={"override-" + key}>
                  {catalogFieldLabel(copy, key)}
                </Label>
                <Input
                  id={"override-" + key}
                  value={draft[key] ?? ""}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      [key]: event.target.value,
                    }))
                  }
                  placeholder={
                    renderCatalogFieldValue(catalog?.effective ?? null, key) ??
                    copy.overridePlaceholderSource(
                      renderCatalogFieldValue(catalog?.source ?? null, key),
                    )
                  }
                />
              </div>
              {renderCatalogFieldValue(catalog?.source ?? null, key) !== null && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  title={copy.restoreFieldTitle}
                  disabled={busy}
                  onClick={() =>
                    runAction(() =>
                      modelsApi.catalog.putOverride(modelConfigId, {
                        [key]: null,
                      }),
                    )
                  }
                >
                  <Undo2 />
                </Button>
              )}
            </div>
          ))}
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
            disabled={busy || (Object.keys(patch).length === 0 && !clearAll)}
            onClick={() =>
              clearAll
                ? runAction(
                    () => modelsApi.catalog.clearOverride(modelConfigId),
                    onClose,
                  )
                : runAction(
                    () => modelsApi.catalog.putOverride(modelConfigId, patch),
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
