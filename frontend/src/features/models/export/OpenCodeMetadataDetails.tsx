import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import type { OpenCodeExportModelRow, OpenCodeExportMetadata } from "@/lib/types";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const FIELDS: Array<keyof OpenCodeExportMetadata> = [
  "name",
  "family",
  "release_date",
  "attachment",
  "reasoning",
  "tool_call",
  "temperature",
  "modalities_input",
  "modalities_output",
  "limit_context",
  "limit_input",
  "limit_output",
];

/** The safe catalog projection, including omitted fields and their evidence. */
export function OpenCodeMetadataDetails({
  model,
  onRepair,
}: {
  model: OpenCodeExportModelRow;
  onRepair?: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.opencodeExport;
  const labels: Record<string, string> = copy.fields;
  const provenance: Record<string, string> = copy.provenance;
  const reasons: Record<string, string> = copy.issues;
  const value = (
    field: keyof OpenCodeExportMetadata,
    metadata: OpenCodeExportMetadata | Record<string, unknown>,
  ) => {
    const item = metadata[field];
    if (item === undefined || item === null) return copy.absent;
    if (typeof item === "boolean") return item ? copy.trueValue : copy.falseValue;
    if (Array.isArray(item)) {
      return (
        item
          .map((modality) =>
            typeof modality === "string"
              ? (copy.modalities as Record<string, string>)[modality] ??
                copy.unknownValue
              : copy.unknownValue,
          )
          .join("、") || copy.emptyList
      );
    }
    if (typeof item === "object") return copy.invalidField;
    return String(item);
  };

  return (
    <details>
      <summary className="cursor-pointer rounded py-1 text-primary focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring">
        {copy.metadataDetails}
      </summary>
      <div className="flex flex-col gap-2 py-2">
        <p className="text-xs text-muted-foreground">{copy.metadataHint}</p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.fieldLabel}</TableHead>
              <TableHead>{copy.sourceValue}</TableHead>
              <TableHead>{copy.overrideValue}</TableHead>
              <TableHead>{copy.finalValue}</TableHead>
              <TableHead>{copy.originLabel}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {FIELDS.map((field) => (
              <TableRow key={field}>
                <TableCell>{labels[field] ?? copy.unknownField}</TableCell>
                {[
                  model.source_metadata,
                  model.override_metadata,
                  model.merged_metadata,
                ].map((metadata, index) => (
                  <TableCell
                    key={index}
                    className={
                      field.startsWith("limit_") || field === "release_date"
                        ? "font-mono text-right"
                        : "whitespace-normal"
                    }
                  >
                    {value(field, metadata)}
                  </TableCell>
                ))}
                <TableCell className="whitespace-normal">
                  {provenance[model.metadata_provenance[field]] ?? copy.absent}
                  {model.missing_metadata.includes(field) ? (
                    <p className="text-xs text-muted-foreground">
                      {copy.missingField}
                    </p>
                  ) : null}
                  {(model.metadata_issues ?? [])
                    .filter((issue) => issue.field === field)
                    .map((issue, index) => (
                      <p key={index} className="text-xs text-muted-foreground">
                        {provenance[issue.source] ?? copy.unknownSource}
                        {"："}
                        {reasons[issue.reason] ?? copy.invalidField}
                      </p>
                    ))}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {onRepair ? <Button variant="link" onClick={onRepair}>{copy.completeMetadata}</Button> : <a className="text-primary underline underline-offset-4" href={`/route/models/${model.model_config_id}`}>{copy.completeMetadata}</a>}
        <p className="text-xs text-muted-foreground">
          {copy.automaticReasoningHint}
        </p>
      </div>
    </details>
  );
}
