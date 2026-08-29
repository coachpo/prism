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
import { PiBindingCell } from "./PiBindingCell";
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
  enrichment_unavailable: "warnEnrichmentUnavailable",
  metadata_incomplete: "warnMetadataIncomplete",
  pi_compat_may_require_manual_override: "warnPiCompatManual",
  unsupported_input_modality: "warnUnsupportedInputModality",
  thinking_level_map_unrepresentable: "warnThinkingMap",
  mixed_base_urls: "warnMixedBaseUrls",
  mixed_credentials: "warnMixedCredentials",
  candidate_unselected: "warnCandidateUnselected",
  candidate_api_mismatch: "warnCandidateMismatch",
  candidate_not_in_catalog: "warnNotInCatalog",
};

export function ModelExportModelTable({
  onToggle,
  selectedIds,
  sourceState,
  summary,
  visibleModels,
}: {
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
              copy={copy}
              model={model}
              onToggle={(checked) => onToggle(model.model_config_id, checked)}
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
  copy,
  model,
  onToggle,
  selected,
  sourceState,
}: {
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
    <tr
      data-testid={`export-row-${model.model_config_id}`}
      className="group border-b last:border-b-0"
    >
      <td className="py-2 pr-2">
        <Checkbox
          checked={selected}
          disabled={!model.selectable}
          onCheckedChange={(c) => onToggle(c === true)}
          aria-label={model.model_id}
        />
      </td>
      <td className="py-2 pr-2 font-mono">
        {model.model_id}
        {!model.selectable && (
          <Badge variant="outline" className="ml-2">
            {copy.unselectablePrefix} {model.unselectable_reason ?? ""}
          </Badge>
        )}
      </td>
      <td className="py-2 pr-2">{model.api_family}</td>
      <td className="py-2 pr-2">
        <PiBindingCell model={model} sourceState={sourceState} />
      </td>
      <td className="py-2 pr-2">
        {model.price_risk.exportable ? copy.priceExportable : copy.priceOmitted}
      </td>
      <td className="py-2">
        <div className="flex flex-wrap gap-1">
          {warningCodes.map((code) => (
            <Badge key={code} variant="outline" title={code}>
              {copy[WARNING_LABEL_KEYS[code] ?? ""] ?? copy.warnGeneric}
            </Badge>
          ))}
        </div>
      </td>
    </tr>
  );
}
