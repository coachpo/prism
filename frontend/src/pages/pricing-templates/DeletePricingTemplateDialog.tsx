import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type {
  PricingTemplate,
  PricingTemplateConnectionUsageItem,
} from "@/lib/types";
import { isPricingTemplateDeleteBlocked } from "@/features/pricing/pricingDeletion";
import { RateCell } from "@/features/pricing/PricingTemplateRatePanel";
import {
    cardRoleLabel,
    templateRateCards,
} from "@/features/pricing/pricingRateCards";
import {
    OperatorCallout,
    OperatorDestructiveDialog,
    OperatorInsetPanel,
    OperatorValueBadge,
} from "@/shared/design-system";

interface DeletePricingTemplateDialogProps {
    deletePricingTemplateConfirm: PricingTemplate | null;
    displayTemplate?: PricingTemplate | null;
    deletePricingTemplateConflict: PricingTemplateConnectionUsageItem[] | null;
    pricingTemplateUsageError: boolean;
    onClose: () => void;
    onDelete: () => Promise<void>;
    pricingTemplateDeleting: boolean;
    pricingTemplateUsageLoading: boolean;
    pricingTemplateUsageRows: PricingTemplateConnectionUsageItem[];
}

export function DeletePricingTemplateDialog({
    deletePricingTemplateConfirm,
    displayTemplate = deletePricingTemplateConfirm,
    deletePricingTemplateConflict,
    pricingTemplateUsageError,
    onClose,
    onDelete,
    pricingTemplateDeleting,
    pricingTemplateUsageLoading,
    pricingTemplateUsageRows,
}: DeletePricingTemplateDialogProps) {
    const { formatNumber, messages } = useLocale();
    const copy = messages.pricingTemplatesUi;
    const dialogTemplate = deletePricingTemplateConfirm ?? displayTemplate;
    // 删除不可逆，用来确认「删的是不是这一条」的正是这些数字：每张价格卡都
    // 列出来，带货币符号与单位，而不是把峰谷退化成一句「2 张价格卡」。
    const summaryCards = dialogTemplate ? templateRateCards(dialogTemplate) : [];
    const dependencyRows =
        deletePricingTemplateConflict ?? pricingTemplateUsageRows;
    const hasDependencies = dependencyRows.length > 0;
    const deleteDisabled = isPricingTemplateDeleteBlocked({
        deleting: pricingTemplateDeleting,
        usageLoading: pricingTemplateUsageLoading,
        usageError: pricingTemplateUsageError,
        dependencyCount: dependencyRows.length,
    });

    return (
        <OperatorDestructiveDialog
            open={deletePricingTemplateConfirm !== null}
            onOpenChange={(open) => {
                if (!open) {
                    onClose();
                }
            }}
            title={copy.deletePricingTemplate}
            description={copy.deletePricingTemplateDescription(
                dialogTemplate?.name ?? "",
            )}
            cancelLabel={messages.settingsDialogs.cancel}
            confirmLabel={copy.deletePricingTemplate}
            confirmingLabel={messages.settingsDialogs.deleting}
            confirming={pricingTemplateDeleting}
            confirmDisabled={deleteDisabled}
            onCancel={onClose}
            onConfirm={onDelete}
            contentClassName="max-h-[calc(100vh-2rem)] sm:max-w-3xl"
            bodyClassName="min-h-0 flex-1 overflow-hidden"
        >
            <div className="flex h-full flex-col gap-4">
                {dialogTemplate ? (
                    <OperatorInsetPanel>
                        <div className="flex flex-col gap-2">
                            <p className="text-sm font-medium text-foreground">
                                {messages.settingsDialogs.deletionSummary}
                            </p>
                            <div className="flex flex-wrap items-center gap-2">
                                <p className="truncate text-sm font-medium text-foreground">
                                    {dialogTemplate.name}
                                </p>
                                <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                                    v{dialogTemplate.version}
                                </code>
                                <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                                    {dialogTemplate.pricing_currency_code}
                                </code>
                                <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                                    {dialogTemplate.template_kind === "standard"
                                        ? copy.kindStandard
                                        : dialogTemplate.template_kind ===
                                            "tiered"
                                          ? copy.kindTiered
                                          : copy.kindPeakValley}
                                </code>
                            </div>
                            {dialogTemplate.description ? (
                                <p className="text-sm text-muted-foreground">
                                    {dialogTemplate.description}
                                </p>
                            ) : null}
                        </div>

                        {dialogTemplate && summaryCards.length > 0 ? (
                            <div className="flex flex-col gap-3">
                                <p className="text-xs text-muted-foreground">
                                    {copy.rateUnitPerMillion}
                                </p>
                                {summaryCards.map(({ role, card }) => (
                                    <div
                                        key={role}
                                        className="flex flex-col gap-1"
                                    >
                                        {summaryCards.length > 1 ? (
                                            <OperatorValueBadge
                                                label={cardRoleLabel(
                                                    role,
                                                    copy,
                                                )}
                                                className="w-fit text-xs"
                                            />
                                        ) : null}
                                        <div className="grid gap-3 sm:grid-cols-2">
                                            <div className="flex min-w-0 flex-col gap-1">
                                                <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                                                    {copy.input}
                                                </p>
                                                <RateCell
                                                    symbol={
                                                        dialogTemplate.active_currency_symbol
                                                    }
                                                    value={card?.input_price}
                                                />
                                            </div>
                                            <div className="flex min-w-0 flex-col gap-1">
                                                <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
                                                    {copy.output}
                                                </p>
                                                <RateCell
                                                    symbol={
                                                        dialogTemplate.active_currency_symbol
                                                    }
                                                    value={card?.output_price}
                                                />
                                            </div>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        ) : (
                            <OperatorCallout
                                intent="warning"
                                description={copy.unknownKind}
                            />
                        )}

                        <Separator />

                        <p className="text-sm text-muted-foreground">
                            {messages.common.thisActionCannotBeUndone}
                        </p>
                    </OperatorInsetPanel>
                ) : null}

                <div className="min-h-0 flex-1 overflow-y-auto pr-1">
                    {pricingTemplateUsageLoading ? (
                        <div className="flex flex-col gap-3 py-1">
                            <Skeleton className="h-10 rounded-md" />
                            <Skeleton className="h-32 rounded-md" />
                        </div>
                    ) : pricingTemplateUsageError ? (
                        <OperatorCallout
                            intent="danger"
                            description={
                                messages.pricingTemplatesData.loadUsageFailed
                            }
                        />
                    ) : hasDependencies ? (
                        <div className="flex flex-col gap-4 py-1">
                            <OperatorCallout
                                intent="danger"
                                description={copy.deletePricingTemplateInUse(
                                    formatNumber(dependencyRows.length),
                                )}
                            />

                            <div className="operator-table-shell overflow-hidden rounded-lg border border-border">
                                <div className="max-h-[260px] overflow-y-auto">
                                    <Table>
                                        <TableHeader>
                                            <TableRow>
                                                <TableHead>
                                                    {copy.model}
                                                </TableHead>
                                                <TableHead>
                                                    {copy.endpoint}
                                                </TableHead>
                                                <TableHead>
                                                    {copy.terminalTargetColumn}
                                                </TableHead>
                                            </TableRow>
                                        </TableHeader>
                                        <TableBody>
                                            {dependencyRows.map((row) => (
                                                <TableRow
                                                    key={row.connection_id}
                                                >
                                                    <TableCell className="font-medium">
                                                        {row.model_id}
                                                    </TableCell>
                                                    <TableCell>
                                                        {row.endpoint_name}
                                                    </TableCell>
                                                    <TableCell>
                                                        {row.connection_name || (
                                                            <span className="italic text-muted-foreground">
                                                                {copy.unnamed}
                                                            </span>
                                                        )}
                                                    </TableCell>
                                                </TableRow>
                                            ))}
                                        </TableBody>
                                    </Table>
                                </div>
                            </div>
                        </div>
                    ) : null}
                </div>
            </div>
        </OperatorDestructiveDialog>
    );
}
