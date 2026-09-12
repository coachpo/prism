export function PiDroppedFieldsEvidence({
  emptyLabel,
  fields,
  label,
}: {
  emptyLabel?: string;
  fields: string[] | undefined;
  label: string;
}) {
  const { messages } = useLocale();
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
      {label}: <span>{messages.externalCatalog.omittedSettingsCount(fields.length)}</span>
    </p>
  );
}
import { useLocale } from "@/i18n/useLocale";
