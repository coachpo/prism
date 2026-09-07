import { useLocale } from "@/i18n/useLocale";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { OperatorEmptyState, OperatorTableShell } from "@/shared/design-system";
import type { OpenCodeExportSourceState } from "./useOpenCodeExportSource";
import { OpenCodeMetadataDetails } from "./OpenCodeMetadataDetails";
import { exportWarningLabel } from "./exportWarningLabel";

export function OpenCodeExportModelTable({
  source,
}: {
  source: OpenCodeExportSourceState;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const oc = messages.opencodeExport;
  const reasons: Record<string, string> = oc.unselectableReasons;

  return (
    <OperatorTableShell
      summary={copy.modelSummary
        .replace("{visible}", String(source.visibleModels.length))
        .replace("{selected}", String(source.selectedIds.size))}
    >
      <Table scrollAreaClassName="max-h-[calc(100dvh-24rem)]">
        <TableHeader>
          <TableRow>
            <TableHead className="sticky left-0 z-20 w-12 min-w-12 bg-inset">
              {copy.columnSelect}
            </TableHead>
            <TableHead className="sticky left-12 z-20 bg-inset">
              {copy.columnModel}
            </TableHead>
            <TableHead>{oc.protocolColumn}</TableHead>
            <TableHead>{oc.metadataColumn}</TableHead>
            <TableHead>{copy.columnPrice}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {source.visibleModels.map((model) => (
            <TableRow
              key={model.model_config_id}
              data-testid={`opencode-export-row-${model.model_config_id}`}
            >
              <TableCell className="sticky left-0 z-10 w-12 min-w-12 bg-panel">
                <Checkbox
                  checked={source.selectedIds.has(model.model_config_id)}
                  disabled={!model.selectable || source.sourceActionsBlocked}
                  onCheckedChange={(checked) =>
                    source.toggleModel(model.model_config_id, checked === true)
                  }
                  aria-label={model.model_id}
                  aria-describedby={
                    !model.selectable
                      ? `opencode-blocked-${model.model_config_id}`
                      : undefined
                  }
                />
              </TableCell>
              <TableCell className="sticky left-12 z-10 bg-panel">
                <span className="font-mono">{model.model_id}</span>
                {!model.selectable ? (
                  <p
                    className="whitespace-normal text-xs text-muted-foreground"
                    id={`opencode-blocked-${model.model_config_id}`}
                  >
                    {copy.unselectablePrefix}
                    {reasons[model.unselectable_reason ?? ""] ?? oc.unknownReason}
                  </p>
                ) : null}
              </TableCell>
              <TableCell>
                <p>{formatApiFamily(model.api_family)}</p>
                <p className="font-mono text-xs">
                  {model.npm || oc.absent}{" · "}{model.api_path || oc.absent}
                </p>
              </TableCell>
              <TableCell className="min-w-72 whitespace-normal">
                <OpenCodeMetadataDetails model={model} />
              </TableCell>
              <TableCell className="max-w-80 whitespace-normal">
                <p>
                  {model.price_risk.exportable
                    ? copy.priceExportable
                    : copy.priceOmitted}
                </p>
                {[
                  ...new Set([
                    ...(model.price_risk.warning_codes ?? []),
                    ...(model.warnings ?? []),
                  ]),
                ].map((warning) => (
                  <p key={warning} className="text-xs text-muted-foreground">
                    {exportWarningLabel(copy, warning)}
                  </p>
                ))}
              </TableCell>
            </TableRow>
          ))}
          {source.visibleModels.length === 0 ? (
            <TableRow>
              <TableCell colSpan={5}>
                <OperatorEmptyState
                  title={copy.emptyTable}
                  description={oc.emptyHint}
                />
              </TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>
    </OperatorTableShell>
  );
}
