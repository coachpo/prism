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
import type { ExportSourceModelRow } from "./exportTypes";
import type { EnhancementDraft } from "./useModelExportUploadReview";

const WARNING_LABEL_KEYS: Record<string, string> = {
  price_no_template: "warnNoTemplate",
  price_currency_not_usd: "warnNotUsd",
  price_unit_not_per_1m: "warnNotPerMillion",
  price_incomplete_components: "warnIncomplete",
  pricing_component_missing: "warnIncomplete",
  price_reasoning_mismatch: "warnReasoningMismatch",
  price_target_conflict: "warnTargetConflict",
  price_peak_valley_unrepresentable: "warnPeakValley",
  price_tier_unrepresentable: "warnTierUnrepresentable",
  enrichment_unavailable: "warnEnrichmentUnavailable",
  metadata_incomplete: "warnMetadataIncomplete",
  pi_compat_may_require_manual_override: "warnPiCompatManual",
  unsupported_input_modality: "warnUnsupportedInputModality",
  variants_unrepresentable: "warnVariants",
  thinking_level_map_unrepresentable: "warnThinkingMap",
  mixed_base_urls: "warnMixedBaseUrls",
  mixed_credentials: "warnMixedCredentials",
};

export function ModelExportModelTable({
  enhancements,
  onToggle,
  selectedIds,
  summary,
  visibleModels,
}: {
  enhancements: Record<number, EnhancementDraft>;
  onToggle: (id: number, checked: boolean) => void;
  selectedIds: ReadonlySet<number>;
  summary: string;
  visibleModels: ExportSourceModelRow[];
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage as unknown as Record<string, string>;
  const pageCopy = messages.modelExportPage;

  return (
    <OperatorTableShell summary={summary}>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{pageCopy.columnSelect}</TableHead>
            <TableHead>{pageCopy.columnModel}</TableHead>
            <TableHead>{pageCopy.columnFamily}</TableHead>
            <TableHead>{pageCopy.columnTargets}</TableHead>
            <TableHead>{pageCopy.columnMetadata}</TableHead>
            <TableHead>{pageCopy.columnPrice}</TableHead>
            <TableHead>{pageCopy.columnRisks}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {visibleModels.map((model) => (
            <ModelExportModelRow
              key={model.model_config_id}
              model={model}
              selected={selectedIds.has(model.model_config_id)}
              enhanced={Boolean(enhancements[model.model_config_id])}
              onToggle={(checked) => onToggle(model.model_config_id, checked)}
              copy={copy}
            />
          ))}
          {visibleModels.length === 0 && (
            <TableRow>
              <TableCell
                colSpan={7}
                className="py-8 text-center text-muted-foreground"
              >
                {pageCopy.emptyTable}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </OperatorTableShell>
  );
}

function ModelExportModelRow({
  copy,
  enhanced,
  model,
  onToggle,
  selected,
}: {
  copy: Record<string, string>;
  enhanced: boolean;
  model: ExportSourceModelRow;
  onToggle: (checked: boolean) => void;
  selected: boolean;
}) {
  const warningCodes = Array.from(
    new Set([
      ...(model.price_risk.warning_codes ?? []),
      ...(model.warnings ?? []),
    ]),
  );

  return (
    <tr
      data-testid={`export-row-${model.model_config_id}`}
      className="border-b last:border-b-0"
    >
      <td className="py-2 pr-2">
        <Checkbox
          checked={selected}
          disabled={!model.selectable}
          onCheckedChange={(checked) => onToggle(checked === true)}
          aria-label={model.model_id}
        />
      </td>
      <td className="py-2 pr-2 font-mono">
        {model.model_id}
        {!model.selectable && (
          <Badge variant="outline" className="ml-2">
            {copy.unselectablePrefix}
            {model.unselectable_reason ? ` ${model.unselectable_reason}` : ""}
          </Badge>
        )}
        {enhanced && (
          <Badge variant="secondary" className="ml-2">
            {copy.enhancedBadge}
          </Badge>
        )}
      </td>
      <td className="py-2 pr-2">{model.api_family}</td>
      <td className="py-2 pr-2 tabular-nums">{model.targets.length}</td>
      <td className="py-2 pr-2">
        {model.enrichment.available
          ? copy.metadataFull
          : copy.metadataStoredOnly}
      </td>
      <td className="py-2 pr-2">
        {model.price_risk.exportable ? copy.priceExportable : copy.priceOmitted}
      </td>
      <td className="py-2">
        <div className="flex flex-wrap gap-1">
          {warningCodes.map((code) => (
            <Badge key={code} variant="outline" title={code}>
              {copy[WARNING_LABEL_KEYS[code] ?? "warnGeneric"]}
            </Badge>
          ))}
        </div>
      </td>
    </tr>
  );
}
