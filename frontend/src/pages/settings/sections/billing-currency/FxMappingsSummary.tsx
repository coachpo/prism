import { Badge } from "@/components/ui/badge";
import { useLocale } from "@/i18n/useLocale";

export function FxMappingsSummary() {
  const { messages } = useLocale();
  const copy = messages.settingsBilling;
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h4 className="text-sm font-medium">{copy.fxMappings}</h4>
        <Badge variant="outline">{copy.defaultFx}</Badge>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        {copy.fxOverridesDefault}
      </p>
    </>
  );
}
