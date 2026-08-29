export function PiDroppedFieldsEvidence({
  fields,
  label,
}: {
  fields: string[] | undefined;
  label: string;
}) {
  if (!fields || fields.length === 0) return null;
  return (
    <p className="max-w-72 text-xs text-muted-foreground">
      {label}: <span className="font-mono">{fields.join(", ")}</span>
    </p>
  );
}
