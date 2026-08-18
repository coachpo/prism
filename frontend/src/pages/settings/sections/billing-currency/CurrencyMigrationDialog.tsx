import { useCallback, useState } from "react";
import { ArrowRight, CheckCircle2 } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { isValidCurrencyCode } from "@/lib/costing";
import type {
  CostingSettingsUpdate,
  CurrencyMigrationDraftHeader,
  CurrencyMigrationDraftChunkItem,
  CurrencyMigrationPreview,
  PricingMigrationInventoryTemplate,
} from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";
import { OperatorCallout } from "@/shared/design-system";
import { formatNumber } from "@/i18n/format";
import { toast } from "sonner";

interface CurrencyMigrationDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentCosting: CostingSettingsUpdate;
  onMigrated: () => Promise<void>;
}

const STEP_PREVIEW = "preview" as const;
const STEP_REPAIR = "repair" as const;
const STEP_COMMIT = "commit" as const;
type Step = typeof STEP_PREVIEW | typeof STEP_REPAIR | typeof STEP_COMMIT;

type TieredTemplateBlock = {
  current_currency_code: string;
  templates: Array<{
    template_id: number;
    name: string;
    input_tokens_above: number;
    input_price: string;
    output_price: string;
    cached_input_price: string | null;
    cache_creation_price: string | null;
    reasoning_price: string | null;
  }>;
};

type PreparedMigration = {
  rows: CurrencyMigrationDraftChunkItem[];
  names: Record<number, string>;
  inventoryId: string | null;
  inventoryHash: string | null;
  inventoryGeneration: number | null;
  operationKind: "currency_cutover" | "repair_same_currency";
};

function tieredTemplateBlockFromError(error: unknown): TieredTemplateBlock | null {
  if (!(error instanceof ApiError) || error.status !== 409 || error.code !== "currency_migration_blocked_by_tiered_templates") return null;
  if (!error.details || typeof error.details !== "object") return null;
  const details = error.details as { current_currency_code?: unknown; templates?: unknown };
  if (typeof details.current_currency_code !== "string" || !Array.isArray(details.templates)) return null;
  const templates = details.templates.filter((item): item is TieredTemplateBlock["templates"][number] => {
    if (!item || typeof item !== "object") return false;
    const row = item as Record<string, unknown>;
    const optionalString = (value: unknown): value is string | null => value === null || typeof value === "string";
    return typeof row.template_id === "number" && typeof row.name === "string" && typeof row.input_tokens_above === "number" && typeof row.input_price === "string" && typeof row.output_price === "string" && optionalString(row.cached_input_price) && optionalString(row.cache_creation_price) && optionalString(row.reasoning_price);
  });
  return { current_currency_code: details.current_currency_code, templates };
}

async function loadAllPricingTemplatePages(): Promise<PreparedMigration["rows"]> {
  const rows: CurrencyMigrationDraftChunkItem[] = [];
  let cursor: string | undefined;
  const seen = new Set<string>();
  while (true) {
    const page = await api.pricingTemplates.listPage({ limit: 100, cursor });
    for (const item of page.items) {
      rows.push({
        template_id: Number(item.id),
        expected_version: item.current_revision.version,
        expected_updated_at: item.updated_at,
        input_price: item.current_revision.input_price,
        output_price: item.current_revision.output_price,
        cached_input_price: item.current_revision.cached_input_price,
        cache_creation_price: item.current_revision.cache_creation_price,
        reasoning_price: item.current_revision.reasoning_price,
      });
    }
    if (!page.next_cursor) {
      break;
    }
    if (seen.has(page.next_cursor)) {
      throw new Error("Pricing template page cursor repeated; reload Settings before retrying.");
    }
    seen.add(page.next_cursor);
    cursor = page.next_cursor;
  }
  return rows;
}

function inventoryRowToDraftItem(item: PricingMigrationInventoryTemplate): CurrencyMigrationDraftChunkItem {
  return {
    template_id: item.template_id,
    expected_version: item.base_version,
    expected_updated_at: item.updated_at,
    input_price: item.current_input_price ?? "",
    output_price: item.current_output_price ?? "",
    cached_input_price: item.current_cached_input_price,
    cache_creation_price: item.current_cache_creation_price,
    reasoning_price: item.current_reasoning_price,
  };
}

async function loadAllInventoryTemplatePages(
  inventoryId: string,
): Promise<{ rows: CurrencyMigrationDraftChunkItem[]; names: Record<number, string> }> {
  const rows: CurrencyMigrationDraftChunkItem[] = [];
  const names: Record<number, string> = {};
  let cursor: string | undefined;
  const seen = new Set<string>();
  while (true) {
    const page = await api.settings.costing.currencyMigrationInventoryTemplates(inventoryId, { limit: 100, cursor });
    for (const item of page.items) {
      rows.push(inventoryRowToDraftItem(item));
      names[item.template_id] = item.name;
    }
    if (!page.next_cursor) {
      break;
    }
    if (seen.has(page.next_cursor)) {
      throw new Error("Pricing migration inventory cursor repeated; reload Settings before retrying.");
    }
    seen.add(page.next_cursor);
    cursor = page.next_cursor;
  }
  return { rows, names };
}

export function CurrencyMigrationDialog({
  open,
  onOpenChange,
  currentCosting,
  onMigrated,
}: CurrencyMigrationDialogProps) {
  const copy = getStaticMessages().settingsCurrencyMigration;
  const [code, setCode] = useState("");
  const [symbol, setSymbol] = useState("");
  const [step, setStep] = useState<Step>(STEP_PREVIEW);
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<CurrencyMigrationPreview | null>(null);
  const [draft, setDraft] = useState<CurrencyMigrationDraftHeader | null>(null);
  const [prepared, setPrepared] = useState<PreparedMigration | null>(null);
  const [tieredTemplateBlock, setTieredTemplateBlock] = useState<TieredTemplateBlock | null>(null);

  const codeError = code.trim() ? (isValidCurrencyCode(code.trim()) ? null : copy.invalidCode) : null;

  const submitMigration = useCallback(async (migration: PreparedMigration) => {
    const expectedUpdatedAt = currentCosting.expected_updated_at?.trim();
    const expectedEpochText = String(currentCosting.reporting_currency_epoch ?? "").trim();
    const expectedEpoch = expectedEpochText ? Number(expectedEpochText) : null;
    if (!expectedUpdatedAt || (expectedEpoch !== null && (!Number.isSafeInteger(expectedEpoch) || expectedEpoch < 1))) {
      throw new Error(copy.previewFailed);
    }
    const draftId = crypto.randomUUID();
    const migrationOperationId = crypto.randomUUID();
    const created = await api.settings.costing.currencyMigrationDraftCreate({
      draft_id: draftId,
      migration_operation_id: migrationOperationId,
      operation_kind: migration.operationKind,
      target_currency_code: code.trim().toUpperCase(),
      target_currency_symbol: symbol.trim(),
      expected_inventory_id: migration.inventoryId,
      expected_inventory_hash: migration.inventoryHash,
      expected_inventory_generation: migration.inventoryGeneration,
      expected_reporting_currency_epoch: expectedEpoch,
      expected_settings_updated_at: expectedUpdatedAt,
    });
    for (let offset = 0, ordinal = 1; offset < migration.rows.length; offset += 100, ordinal += 1) {
      await api.settings.costing.currencyMigrationDraftChunk(draftId, ordinal, migration.rows.slice(offset, offset + 100));
    }
    const sealed = await api.settings.costing.currencyMigrationDraftSeal(draftId);
    const response = await api.settings.costing.currencyMigrationPreview({
      operation_kind: migration.operationKind,
      migration_operation_id: created.migration_operation_id,
      draft_id: sealed.draft_id,
      draft_hash: sealed.draft_hash ?? "",
    });
    setPrepared(migration);
    setDraft(sealed);
    setPreview(response);
    setStep(STEP_COMMIT);
  }, [code, copy.previewFailed, currentCosting.expected_updated_at, currentCosting.reporting_currency_epoch, symbol]);

  const handlePreview = useCallback(async () => {
    if (codeError || !code.trim() || !symbol.trim()) {
      return;
    }
    setLoading(true);
    try {
      const inventory = currentCosting.pricing_migration_inventory;
      const currentCode = currentCosting.report_currency_code.trim().toUpperCase();
      const hasEpoch = Boolean(String(currentCosting.reporting_currency_epoch ?? "").trim());
      const useInventory = Boolean(inventory && (!hasEpoch || (inventory.template_issue_count > 0 && code.trim().toUpperCase() === currentCode)));
      let rows: CurrencyMigrationDraftChunkItem[];
      let names: Record<number, string> = {};
      let inventoryID: string | null = null;
      let inventoryHash: string | null = null;
      let inventoryGeneration: number | null = null;
      if (useInventory && inventory) {
        const loaded = await loadAllInventoryTemplatePages(inventory.inventory_id);
        rows = loaded.rows;
        names = loaded.names;
        inventoryID = inventory.inventory_id;
        inventoryHash = inventory.inventory_hash;
        inventoryGeneration = inventory.generation;
      } else {
        rows = await loadAllPricingTemplatePages();
      }
      const operationKind: PreparedMigration["operationKind"] =
        useInventory && hasEpoch && currentCode && code.trim().toUpperCase() === currentCode ? "repair_same_currency" : "currency_cutover";
      const migration: PreparedMigration = { rows, names, inventoryId: inventoryID, inventoryHash, inventoryGeneration, operationKind };
      setPrepared(migration);
      if (rows.some((row) => !row.input_price.trim() || !row.output_price.trim())) {
        setStep(STEP_REPAIR);
        return;
      }
      await submitMigration(migration);
    } catch (error) {
      const blocked = tieredTemplateBlockFromError(error);
      if (blocked) setTieredTemplateBlock(blocked);
      else toast.error(error instanceof Error ? error.message : copy.previewFailed);
    } finally {
      setLoading(false);
    }
  }, [code, codeError, copy.previewFailed, currentCosting.pricing_migration_inventory, currentCosting.report_currency_code, currentCosting.reporting_currency_epoch, submitMigration, symbol]);

  const handleRepairContinue = useCallback(async () => {
    if (!prepared) {
      return;
    }
    if (prepared.rows.some((row) => !row.input_price.trim() || !row.output_price.trim())) {
      toast.error(copy.repairMissingRequired);
      return;
    }
    setLoading(true);
    try {
      await submitMigration(prepared);
    } catch (error) {
      const blocked = tieredTemplateBlockFromError(error);
      if (blocked) setTieredTemplateBlock(blocked);
      else toast.error(error instanceof Error ? error.message : copy.previewFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.previewFailed, copy.repairMissingRequired, prepared, submitMigration]);

  const updatePreparedPrice = useCallback((
    templateId: number,
    field: keyof Pick<CurrencyMigrationDraftChunkItem, "input_price" | "output_price" | "cached_input_price" | "cache_creation_price" | "reasoning_price">,
    value: string,
  ) => {
    setPrepared((current) => current ? {
      ...current,
      rows: current.rows.map((row) => row.template_id === templateId ? { ...row, [field]: value } : row),
    } : current);
  }, []);

  const handleCommit = useCallback(async () => {
    if (!preview || !draft || preview.next_epoch === null) {
      return;
    }
    setLoading(true);
    try {
      await api.settings.costing.currencyMigrationCommit({
        operation_kind: preview.operation_kind,
        migration_operation_id: preview.migration_operation_id,
        draft_id: preview.draft_id,
        draft_hash: preview.draft_hash,
        preview_hash: preview.preview_hash,
      });
      await onMigrated();
      toast.success(copy.commitSucceeded(preview.target_currency_code, preview.next_epoch));
      setPreview(null);
      setPrepared(null);
      setTieredTemplateBlock(null);
      setStep(STEP_PREVIEW);
      setCode("");
      setSymbol("");
      onOpenChange(false);
    } catch (error) {
      const blocked = tieredTemplateBlockFromError(error);
      if (blocked) setTieredTemplateBlock(blocked);
      else toast.error(error instanceof Error ? error.message : copy.commitFailed);
    } finally {
      setLoading(false);
    }
  }, [draft, onMigrated, onOpenChange, preview, copy]);

  const handleOpenChange = useCallback(
    (next: boolean) => {
      if (!next) {
        setPreview(null);
        setDraft(null);
        setPrepared(null);
        setTieredTemplateBlock(null);
        setStep(STEP_PREVIEW);
        setCode("");
        setSymbol("");
      }
      onOpenChange(next);
    },
    [onOpenChange],
  );

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent aria-describedby="currency-migration-description">
        <AlertDialogHeader>
          <AlertDialogTitle>{copy.title}</AlertDialogTitle>
          <AlertDialogDescription id="currency-migration-description">
            {copy.description}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {tieredTemplateBlock ? (
          <OperatorCallout
            intent="danger"
            title={copy.tieredTemplatesTitle}
            description={
              <div className="flex flex-col gap-2">
                <p>{copy.tieredTemplatesDescription(tieredTemplateBlock.current_currency_code)}</p>
                <ul className="list-disc space-y-1 pl-5 text-xs">
                  {tieredTemplateBlock.templates.map((template) => (
                    <li key={template.template_id}>
                      <span className="font-medium">{copy.tieredTemplateRow(template.name, template.input_tokens_above)}</span>
                      <span className="ml-2 text-muted-foreground">{copy.tieredTemplatePrices}{[template.input_price, template.output_price, template.cached_input_price ?? getStaticMessages().requestLogs.notConfigured, template.cache_creation_price ?? getStaticMessages().requestLogs.notConfigured, template.reasoning_price ?? getStaticMessages().requestLogs.notConfigured].join(" / ")}</span>
                    </li>
                  ))}
                </ul>
              </div>
            }
          />
        ) : null}

        {step === STEP_PREVIEW ? (
          <div className="flex flex-col gap-3">
            <FieldGroup className="gap-3">
              <div className="flex flex-wrap items-end gap-3">
                <Field className="w-28">
                  <FieldLabel htmlFor="migration-currency-code">{copy.code}</FieldLabel>
                  <Input
                    id="migration-currency-code"
                    name="target_currency_code"
                    autoComplete="off"
                    maxLength={3}
                    value={code}
                    aria-invalid={codeError ? true : undefined}
                    onChange={(event) => setCode(event.target.value.toUpperCase())}
                    placeholder={currentCosting.report_currency_code || "USD"}
                  />
                </Field>
                <Field className="w-24">
                  <FieldLabel htmlFor="migration-currency-symbol">{copy.symbol}</FieldLabel>
                  <Input
                    id="migration-currency-symbol"
                    name="target_currency_symbol"
                    autoComplete="off"
                    maxLength={5}
                    value={symbol}
                    onChange={(event) => setSymbol(event.target.value)}
                    placeholder="€"
                  />
                </Field>
              </div>
              {codeError ? <p className="text-sm font-medium text-destructive">{codeError}</p> : null}
            </FieldGroup>
            <OperatorCallout
              intent="warning"
              description={copy.warning(
                currentCosting.report_currency_code || "—",
                currentCosting.report_currency_symbol || "-",
              )}
            />
          </div>
        ) : step === STEP_REPAIR && prepared ? (
          <div className="flex max-h-[min(60vh,34rem)] flex-col gap-3 overflow-y-auto">
            <OperatorCallout
              intent="warning"
              description={
                <>
                  <p>{copy.repairDescription}</p>
                  <details className="pt-2 text-xs">
                    <summary className="cursor-pointer font-medium">{getStaticMessages().common.moreDetails}</summary>
                    <p className="pt-2">{copy.repairDescriptionDetails}</p>
                  </details>
                </>
              }
            />
            {prepared.rows.filter((row) => !row.input_price.trim() || !row.output_price.trim()).map((row) => (
              <div key={row.template_id} className="rounded-md border border-border p-3">
                <p className="mb-2 text-sm font-medium">{prepared.names[row.template_id] || `Template ${row.template_id}`}</p>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {([
                    ["input_price", copy.repairFieldInput],
                    ["output_price", copy.repairFieldOutput],
                    ["cached_input_price", copy.repairFieldCached],
                    ["cache_creation_price", copy.repairFieldCreation],
                    ["reasoning_price", copy.repairFieldReasoning],
                  ] as const).map(([field, label]) => (
                    <Field key={field}>
                      <FieldLabel htmlFor={`repair-${row.template_id}-${field}`}>{label}</FieldLabel>
                      <Input
                        id={`repair-${row.template_id}-${field}`}
                        inputMode="decimal"
                        value={row[field] ?? ""}
                        onChange={(event) => updatePreparedPrice(row.template_id, field, event.target.value)}
                      />
                    </Field>
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : preview ? (
          <div className="flex flex-col gap-3">
            <OperatorCallout
              intent="success"
              description={copy.previewSummary(
                preview.current_currency_code,
                preview.target_currency_code,
                preview.template_count,
                preview.revision_change_count,
              )}
            />
            <div className="max-h-56 overflow-y-auto rounded-md border border-border">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-background text-left text-xs font-medium text-muted-foreground">
                  <tr>
                    <th className="px-3 py-2">{copy.tableTemplate}</th>
                    <th className="px-3 py-2">{copy.tableVersion}</th>
                    <th className="px-3 py-2">{copy.tableReferences}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {preview.template_page.items.map((template) => (
                    <tr key={template.template_id}>
                      <td className="px-3 py-2 font-medium">{template.name}</td>
                      <td className="px-3 py-2 font-mono text-xs">
                        {template.current_version}
                        <ArrowRight className="mx-1 inline h-3 w-3" aria-hidden="true" />
                        {template.next_version}
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">
                        {formatNumber(template.reference_count)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
              <CheckCircle2 className="h-3.5 w-3.5" aria-hidden="true" />
              {copy.frozenRevisions(preview.next_epoch ?? "-")}
            </p>
          </div>
        ) : null}

        <AlertDialogFooter>
          <AlertDialogCancel disabled={loading}>{copy.cancel}</AlertDialogCancel>
          {step === STEP_PREVIEW ? (
            <Button
              type="button"
              onClick={() => void handlePreview()}
              disabled={loading || Boolean(codeError) || !code.trim() || !symbol.trim()}
            >
              {loading ? copy.previewing : copy.previewButton}
            </Button>
          ) : step === STEP_REPAIR ? (
            <Button type="button" onClick={() => void handleRepairContinue()} disabled={loading || !prepared}>
              {loading ? copy.previewing : copy.repairContinue}
            </Button>
          ) : (
            <Button
              type="button"
              variant="destructive"
              onClick={() => void handleCommit()}
              disabled={loading}
            >
              {loading ? copy.committing : copy.commitButton}
            </Button>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
