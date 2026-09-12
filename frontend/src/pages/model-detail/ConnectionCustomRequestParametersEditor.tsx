import { useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout } from "@/shared/design-system";
import { parseCustomRequestParametersDraft, type CustomRequestParametersParseError } from "./customRequestParameters";
import { RequestSettingFields } from "./RequestSettingFields";
import { serializeSettings, settingNode, type SettingNode } from "./structuredRequestSettings";

interface Props {
  draft: string;
  onDraftChange: (draft: string) => void;
  error: CustomRequestParametersParseError | null;
}

export function ConnectionCustomRequestParametersEditor({ draft, onDraftChange, error }: Props) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const parsed = parseCustomRequestParametersDraft(draft);
  const [editing, setEditing] = useState(() => ({ source: draft, node: settingNode(parsed.value ?? {}), readable: !parsed.error }));
  // Prefill/reopen replaces the tree. Incomplete local fields keep their exact
  // draft so validation blocks saving instead of silently using an old value.
  if (editing.source !== draft) {
    setEditing({ source: draft, node: settingNode(parsed.value ?? {}), readable: !parsed.error });
  }
  const update = (node: SettingNode) => {
    const source = serializeSettings(node);
    setEditing({ source, node, readable: true });
    onDraftChange(source);
  };
  const displayError = error ?? parsed.error;
  const topLevelCount = editing.node.kind === "group" ? editing.node.entries.length : 0;

  return <section className="flex flex-col gap-3" data-testid="connection-dialog-custom-request-parameters-card">
    <div className="flex flex-col gap-1">
      <h3 className="text-sm font-semibold">{copy.customRequestParameters}</h3>
      <p className="text-sm text-muted-foreground">{copy.customRequestParametersDescription}</p>
      {editing.readable ? <p className="text-xs text-muted-foreground">{topLevelCount ? copy.customRequestParametersSummary(topLevelCount) : copy.customRequestParametersNotConfigured}</p> : null}
    </div>
    {displayError ? <OperatorCallout intent="danger" role="alert" description={settingErrorMessage(copy, displayError)} /> : null}
    {editing.readable ? <RequestSettingFields node={editing.node} label={copy.customRequestParameters} onChange={update} /> : <p className="text-xs text-muted-foreground">{copy.settingReadFailed}</p>}
    <p className="text-xs text-muted-foreground">{copy.customRequestParametersNotSecretHint}</p>
  </section>;
}

function settingErrorMessage(copy: ReturnType<typeof useLocale>["messages"]["modelDetail"], error: CustomRequestParametersParseError): string {
  switch (error.reason) {
    case "blank_key": return copy.settingNameRequired;
    case "duplicate_key": return copy.settingNameDuplicate;
    case "protected_field": return copy.settingNameUnavailable;
    case "number_out_of_range": return copy.settingNumberInvalid;
    case "too_large": return copy.settingTooLarge;
    case "too_deep": return copy.settingTooDeep;
    case "too_many_members": return copy.settingTooMany;
    default: return copy.customRequestParametersErrorInvalid;
  }
}
