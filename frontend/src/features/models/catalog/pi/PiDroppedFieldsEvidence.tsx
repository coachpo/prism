export function PiDroppedFieldsEvidence({
  emptyLabel,
  fields,
  label,
}: {
  emptyLabel?: string;
  fields: string[] | undefined;
  label: string;
}) {
  if (!fields || fields.length === 0) {
    if (!emptyLabel) return null;
    return (
      <p className="max-w-72 text-xs text-muted-foreground">
        {label}: <span>{emptyLabel}</span>
      </p>
    );
  }
  return (
    <p className="max-w-72 text-xs text-muted-foreground">
      {label}: <span className="font-mono">{fields.join(", ")}</span>
    </p>
  );
}
