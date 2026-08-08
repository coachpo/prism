import { useId } from "react";
import { Braces, Eraser, WandSparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { useLocale } from "@/i18n/useLocale";
import {
  parseCustomRequestParametersDraft,
  type CustomRequestParametersParseError,
} from "./customRequestParameters";

interface ConnectionCustomRequestParametersEditorProps {
  draft: string;
  onDraftChange: (draft: string) => void;
  error: CustomRequestParametersParseError | null;
}

/**
 * Full-width JSON editor for the Connection custom request parameters. The
 * draft stays as raw text so operators can repair JSON that is not yet valid;
 * validation mirrors the backend shared validator and blocks save until the
 * draft is empty or a fully valid object.
 */
export function ConnectionCustomRequestParametersEditor({
  draft,
  onDraftChange,
  error,
}: ConnectionCustomRequestParametersEditorProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const textareaId = useId();
  const errorId = useId();

  const parsed = parseCustomRequestParametersDraft(draft);
  const displayError = error ?? parsed.error;
  const isValid = displayError === null;
  const topLevelCount = parsed.value ? Object.keys(parsed.value).length : 0;

  const handleFormat = () => {
    if (!isValid || parsed.value === null) {
      return;
    }
    onDraftChange(JSON.stringify(parsed.value, null, 2));
  };

  return (
    <section
      className="flex flex-col gap-2.5"
      data-testid="connection-dialog-custom-request-parameters-card"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h3 className="text-sm font-semibold tracking-tight text-foreground">
            {copy.customRequestParameters}
          </h3>
          <p className="text-sm text-muted-foreground">{copy.customRequestParametersDescription}</p>
          <p className="text-xs text-muted-foreground">
            {topLevelCount > 0
              ? copy.customRequestParametersSummary(topLevelCount)
              : copy.customRequestParametersNotConfigured}
          </p>
        </div>
        <div className="flex gap-1.5">
          <Button type="button" variant="outline" size="sm" onClick={handleFormat}>
            <WandSparkles data-icon="inline-start" />
            {copy.customRequestParametersFormat}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label={copy.customRequestParametersClear}
            onClick={() => onDraftChange("")}
          >
            <Eraser data-icon="inline-start" />
            {copy.customRequestParametersClear}
          </Button>
        </div>
      </div>

      <Textarea
        id={textareaId}
        name="custom_request_parameters"
        className="min-h-40 w-full font-mono text-xs"
        spellCheck={false}
        autoComplete="off"
        aria-label={copy.customRequestParameters}
        aria-invalid={!isValid}
        aria-describedby={!isValid ? errorId : undefined}
        placeholder={copy.customRequestParametersPlaceholder}
        value={draft}
        onChange={(event) => onDraftChange(event.target.value)}
      />

      {!isValid && displayError ? (
        <p id={errorId} className="text-sm font-medium text-destructive" role="alert">
          {customRequestParametersErrorMessage(copy, displayError)}
        </p>
      ) : null}

      <div className="flex flex-col gap-1">
        <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
          <Braces data-icon="inline-start" className="mt-0.5 shrink-0" />
          {copy.customRequestParametersProtectedHint}
        </p>
        <p className="text-xs text-muted-foreground">{copy.customRequestParametersNotSecretHint}</p>
      </div>
    </section>
  );
}

function customRequestParametersErrorMessage(
  copy: ReturnType<typeof useLocale>["messages"]["modelDetail"],
  error: CustomRequestParametersParseError,
): string {
  const path = error.path.length > 0 ? error.path : "custom_request_parameters";
  switch (error.reason) {
    case "not_object":
      return copy.customRequestParametersErrorNotObject(path);
    case "duplicate_key":
      return copy.customRequestParametersErrorDuplicateKey(path);
    case "blank_key":
      return copy.customRequestParametersErrorBlankKey(path);
    case "protected_field":
      return copy.customRequestParametersErrorProtectedField(path);
    case "too_large":
      return copy.customRequestParametersErrorTooLarge(path, error.limit ?? 65536);
    case "too_deep":
      return copy.customRequestParametersErrorTooDeep(path, error.limit ?? 16);
    case "too_many_members":
      return copy.customRequestParametersErrorTooManyMembers(path, error.limit ?? 256);
    case "number_out_of_range":
      return copy.customRequestParametersErrorNumberOutOfRange(path);
    default:
      return copy.customRequestParametersErrorInvalid;
  }
}
