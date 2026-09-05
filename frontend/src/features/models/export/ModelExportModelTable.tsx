import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { OperatorTableShell } from "@/shared/design-system";
import type { ExportSourceModelRow } from "@/lib/types";
import { PiBindingCell } from "./PiBindingCell";
import type { PiBindingController } from "@/features/models/catalog/pi/usePiBindingController";
import type { ModelExportSourceState } from "./useModelExportSource";

const WARNING_LABEL_KEYS: Record<string, string> = {
    price_no_template: "warnNoTemplate",
    price_currency_not_usd: "warnNotUsd",
    price_unit_not_per_1m: "warnNotPerMillion",
    pricing_component_missing: "warnIncomplete",
    price_reasoning_mismatch: "warnReasoningMismatch",
    price_target_conflict: "warnTargetConflict",
    price_peak_valley_unrepresentable: "warnPeakValley",
    price_tier_unrepresentable: "warnTierUnrepresentable",
    metadata_incomplete: "warnMetadataIncomplete",
    pi_source_fields_dropped: "warnPiSourceFieldsDropped",
    unsupported_input_modality: "warnUnsupportedInputModality",
    mixed_base_urls: "warnMixedBaseUrls",
};

export function ModelExportModelTable({
    controller,
    onToggle,
    selectedIds,
    sourceState,
    summary,
    visibleModels,
}: {
    controller: PiBindingController;
    onToggle: (id: number, checked: boolean) => void;
    selectedIds: ReadonlySet<number>;
    sourceState: ModelExportSourceState;
    summary: string;
    visibleModels: ExportSourceModelRow[];
}) {
    const { messages } = useLocale();
    const copy = messages.modelExportPage;
    return (
        <OperatorTableShell summary={summary}>
            <Table>
                <TableHeader>
                    <TableRow>
                        <TableHead>{copy.columnSelect}</TableHead>
                        <TableHead>{copy.columnModel}</TableHead>
                        <TableHead>{copy.columnFamily}</TableHead>
                        <TableHead>{copy.columnPiBinding}</TableHead>
                        <TableHead>{copy.columnPrice}</TableHead>
                        <TableHead>{copy.columnRisks}</TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {visibleModels.map((model) => (
                        <ModelExportModelRow
                            key={model.model_config_id}
                            controller={controller}
                            copy={copy}
                            model={model}
                            onToggle={(checked) =>
                                onToggle(model.model_config_id, checked)
                            }
                            selected={selectedIds.has(model.model_config_id)}
                            sourceState={sourceState}
                        />
                    ))}
                    {visibleModels.length === 0 && (
                        <TableRow>
                            <TableCell
                                colSpan={6}
                                className="py-8 text-center text-muted-foreground"
                            >
                                {copy.emptyTable}
                            </TableCell>
                        </TableRow>
                    )}
                </TableBody>
            </Table>
        </OperatorTableShell>
    );
}

function ModelExportModelRow({
    controller,
    copy,
    model,
    onToggle,
    selected,
    sourceState,
}: {
    controller: PiBindingController;
    copy: Record<string, string>;
    model: ExportSourceModelRow;
    onToggle: (checked: boolean) => void;
    selected: boolean;
    sourceState: ModelExportSourceState;
}) {
    const warningCodes = Array.from(
        new Set([
            ...(model.price_risk.warning_codes ?? []),
            ...(model.warnings ?? []),
        ]),
    );
    return (
        <TableRow
            data-testid={`export-row-${model.model_config_id}`}
            className="group"
        >
            <TableCell>
                <Checkbox
                    checked={selected}
                    disabled={!model.selectable}
                    onCheckedChange={(c) => onToggle(c === true)}
                    aria-label={model.model_id}
                />
            </TableCell>
            <TableCell className="font-mono">
                {model.model_id}
                {!model.selectable && (
                    <Badge variant="outline" className="ml-2">
                        {copy.unselectablePrefix}{" "}
                        {model.unselectable_reason ?? ""}
                    </Badge>
                )}
            </TableCell>
            <TableCell>{model.api_family}</TableCell>
            {/* 这一格装的是整句说明（无法绑定 / 绑定不可渲染），
                必须放开 TableCell 默认的 nowrap，否则一行撑穿整张表。 */}
            <TableCell className="whitespace-normal">
                <PiBindingCell
                    controller={controller}
                    model={model}
                    sourceState={sourceState}
                />
            </TableCell>
            <TableCell>
                {model.price_risk.exportable
                    ? copy.priceExportable
                    : copy.priceOmitted}
            </TableCell>
            <TableCell>
                <div className="flex flex-wrap gap-1">
                    {warningCodes.map((code) => (
                        <Badge key={code} variant="outline" title={code}>
                            {copy[WARNING_LABEL_KEYS[code] ?? ""] ??
                                copy.warnGeneric}
                        </Badge>
                    ))}
                </div>
            </TableCell>
        </TableRow>
    );
}
