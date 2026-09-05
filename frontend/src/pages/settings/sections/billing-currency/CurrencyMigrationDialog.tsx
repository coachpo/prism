import { useCallback, useState } from "react";
import { ArrowRight, CheckCircle2 } from "lucide-react";
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
  CurrencyMigrationCard,
  CurrencyMigrationDraftHeader,
  CurrencyMigrationPreview,
} from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";
import { OperatorCallout } from "@/shared/design-system";
import { formatNumber } from "@/i18n/format";
import { toast } from "sonner";
import {
  currencyMigrationCardSetHasMissingRequiredPrice,
} from "./currencyMigrationCards";
import {
  commitCurrencyMigration,
  prepareCurrencyMigration,
  submitCurrencyMigrationPreview,
  type PreparedMigration,
} from "../../costing/currencyMigrationProtocol";

interface CurrencyMigrationDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentCosting: CostingSettingsUpdate;
  onMigrated: () => Promise<void>;
}

const MIGRATION_FORM_ID = "currency-migration-form";
const STEP_PREVIEW = "preview" as const;
const STEP_REPAIR = "repair" as const;
const STEP_COMMIT = "commit" as const;
type Step = typeof STEP_PREVIEW | typeof STEP_REPAIR | typeof STEP_COMMIT;

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

  const codeError = code.trim()
    ? isValidCurrencyCode(code.trim())
      ? null
      : copy.invalidCode
    : null;

  const submitMigration = useCallback(
    async (migration: PreparedMigration) => {
      const result = await submitCurrencyMigrationPreview(
        migration,
        currentCosting,
        code,
        symbol,
        copy.previewFailed,
      );
      setPrepared(migration);
      setDraft(result.draft);
      setPreview(result.preview);
      setStep(STEP_COMMIT);
    },
    [
      code,
      copy.previewFailed,
      currentCosting,
      symbol,
    ],
  );

  const handlePreview = useCallback(async () => {
    if (codeError || !code.trim() || !symbol.trim()) {
      return;
    }
    setLoading(true);
    try {
      const migration = await prepareCurrencyMigration(currentCosting, code);
      setPrepared(migration);
      if (migration.rows.some(currencyMigrationCardSetHasMissingRequiredPrice)) {
        setStep(STEP_REPAIR);
        return;
      }
      await submitMigration(migration);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.previewFailed);
    } finally {
      setLoading(false);
    }
  }, [
    code,
    codeError,
    copy.previewFailed,
    currentCosting,
    submitMigration,
    symbol,
  ]);

  const handleRepairContinue = useCallback(async () => {
    if (!prepared) {
      return;
    }
    if (prepared.rows.some(currencyMigrationCardSetHasMissingRequiredPrice)) {
      toast.error(copy.repairMissingRequired);
      return;
    }
    setLoading(true);
    try {
      await submitMigration(prepared);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.previewFailed);
    } finally {
      setLoading(false);
    }
  }, [
    copy.previewFailed,
    copy.repairMissingRequired,
    prepared,
    submitMigration,
  ]);

  const updatePreparedPrice = useCallback(
    (
      templateId: number,
      cardRole: CurrencyMigrationCard["card_role"],
      field: keyof Omit<CurrencyMigrationCard, "card_role">,
      value: string,
    ) => {
      setPrepared((current) =>
        current
          ? {
              ...current,
              rows: current.rows.map((row) =>
                row.template_id === templateId
                  ? {
                      ...row,
                      cards: row.cards.map((card) =>
                        card.card_role === cardRole
                          ? { ...card, [field]: value }
                          : card,
                      ),
                    }
                  : row,
              ),
            }
          : current,
      );
    },
    [],
  );

  const handleCommit = useCallback(async () => {
    if (!preview || !draft || preview.next_epoch === null) {
      return;
    }
    setLoading(true);
    try {
      await commitCurrencyMigration(preview);
      await onMigrated();
      toast.success(
        copy.commitSucceeded(preview.target_currency_code, preview.next_epoch),
      );
      setPreview(null);
      setPrepared(null);
      setStep(STEP_PREVIEW);
      setCode("");
      setSymbol("");
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.commitFailed);
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

        {step === STEP_PREVIEW ? (
          // 真表单：两个代码框里敲回车就走预检，两个必填项也标出来。
          <form
            id={MIGRATION_FORM_ID}
            className="flex flex-col gap-3"
            onSubmit={(event) => {
              event.preventDefault();
              void handlePreview();
            }}
          >
            <FieldGroup className="gap-3">
              <div className="flex flex-wrap items-end gap-3">
                <Field className="w-28">
                  <FieldLabel htmlFor="migration-currency-code" required>
                    {copy.code}
                  </FieldLabel>
                  <Input
                    id="migration-currency-code"
                    name="target_currency_code"
                    autoComplete="off"
                    maxLength={3}
                    required
                    aria-required="true"
                    value={code}
                    aria-invalid={codeError ? true : undefined}
                    onChange={(event) =>
                      setCode(event.target.value.toUpperCase())
                    }
                    placeholder={currentCosting.report_currency_code || "USD"}
                  />
                </Field>
                <Field className="w-24">
                  <FieldLabel htmlFor="migration-currency-symbol" required>
                    {copy.symbol}
                  </FieldLabel>
                  <Input
                    id="migration-currency-symbol"
                    name="target_currency_symbol"
                    autoComplete="off"
                    maxLength={5}
                    required
                    aria-required="true"
                    value={symbol}
                    onChange={(event) => setSymbol(event.target.value)}
                    placeholder="€"
                  />
                </Field>
              </div>
              {codeError ? (
                <p className="text-sm font-medium text-destructive">
                  {codeError}
                </p>
              ) : null}
            </FieldGroup>
            <OperatorCallout
              intent="warning"
              description={copy.warning(
                currentCosting.report_currency_code || "—",
                currentCosting.report_currency_symbol || "-",
              )}
            />
          </form>
        ) : step === STEP_REPAIR && prepared ? (
          <div className="flex max-h-[min(60vh,34rem)] flex-col gap-3 overflow-y-auto">
            <OperatorCallout
              intent="warning"
              description={
                <>
                  <p>{copy.repairDescription}</p>
                  <details className="pt-2 text-xs">
                    <summary className="cursor-pointer font-medium">
                      {getStaticMessages().common.moreDetails}
                    </summary>
                    <p className="pt-2">{copy.repairDescriptionDetails}</p>
                  </details>
                </>
              }
            />
            {prepared.rows.filter(currencyMigrationCardSetHasMissingRequiredPrice).map((row) => (
              <div
                key={row.template_id}
                className="rounded-md border border-border p-3"
              >
                <p className="mb-2 text-sm font-medium">
                  {prepared.names[row.template_id] ||
                    `Template ${row.template_id}`}
                </p>
                <div className="flex flex-col gap-3">
                  {row.cards.map((card) => (
                    <div
                      key={card.card_role}
                      className="grid grid-cols-1 gap-2 sm:grid-cols-2"
                    >
                      <p className="text-xs font-medium text-muted-foreground sm:col-span-2">
                        {card.card_role}
                      </p>
                      {(
                        [
                          ["input_price", copy.repairFieldInput],
                          ["output_price", copy.repairFieldOutput],
                          ["cached_input_price", copy.repairFieldCached],
                          ["cache_creation_price", copy.repairFieldCreation],
                          ["reasoning_price", copy.repairFieldReasoning],
                        ] as const
                      ).map(([field, label]) => (
                        <Field key={`${card.card_role}-${field}`}>
                          <FieldLabel
                            htmlFor={`repair-${row.template_id}-${card.card_role}-${field}`}
                          >
                            {label}
                          </FieldLabel>
                          <Input
                            id={`repair-${row.template_id}-${card.card_role}-${field}`}
                            inputMode="decimal"
                            value={card[field] ?? ""}
                            onChange={(event) =>
                              updatePreparedPrice(
                                row.template_id,
                                card.card_role,
                                field,
                                event.target.value,
                              )
                            }
                          />
                        </Field>
                      ))}
                    </div>
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
                        <ArrowRight
                          className="mx-1 inline h-3 w-3"
                          aria-hidden="true"
                        />
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
          <AlertDialogCancel disabled={loading}>
            {copy.cancel}
          </AlertDialogCancel>
          {step === STEP_PREVIEW ? (
            // 页脚在 <form> 外，靠 form 属性把提交按钮接回那张表单。
            <Button
              type="submit"
              form={MIGRATION_FORM_ID}
              disabled={
                loading || Boolean(codeError) || !code.trim() || !symbol.trim()
              }
            >
              {loading ? copy.previewing : copy.previewButton}
            </Button>
          ) : step === STEP_REPAIR ? (
            <Button
              type="button"
              onClick={() => void handleRepairContinue()}
              disabled={loading || !prepared}
            >
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
