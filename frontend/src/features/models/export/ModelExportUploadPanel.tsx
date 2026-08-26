import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorSectionCard,
} from "@/shared/design-system";
import { useLocale } from "@/i18n/useLocale";
import type { ExportPlatform } from "./exportTypes";
import type { ModelExportUploadReviewState } from "./useModelExportUploadReview";

export function ModelExportUploadPanel({
  platform,
  review,
}: {
  platform: ExportPlatform;
  review: ModelExportUploadReviewState;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const {
    applyExtraction,
    confirmedHeaders,
    enhancedCount,
    extraction,
    extractionError,
    handleFileUpload,
    setHeaderConfirmed,
  } = review;

  return (
    <OperatorSectionCard
      title={copy.enhancementTitle}
      description={copy.uploadHint}
    >
      <FieldGroup className="gap-3">
        <Field>
          <FieldLabel htmlFor="export-config-upload">
            {copy.uploadLabel}
          </FieldLabel>
          <Input
            key={platform}
            id="export-config-upload"
            type="file"
            accept=".json,.jsonc,application/json,text/plain"
            onChange={(event) => {
              const file = event.currentTarget.files?.[0];
              // The DOM must not retain the uploaded File; only sanitized
              // extraction data enters component state.
              event.currentTarget.value = "";
              void handleFileUpload(file);
            }}
          />
        </Field>
      </FieldGroup>
      {extractionError && (
        <OperatorCallout
          className="mt-3"
          intent="danger"
          description={extractionError}
        />
      )}
      {extraction && (
        <OperatorInsetPanel
          className="mt-3"
          title={copy.extractedSummary
            .replace("{count}", String(extraction.models.length))
            .replace("{kind}", extraction.sourceKind)}
        >
          <div className="flex flex-col gap-3">
            {extraction.headerCandidates.length > 0 ? (
              <FieldGroup className="gap-2">
                <FieldDescription>{copy.headerConfirmTitle}</FieldDescription>
                {extraction.headerCandidates.map((header) => (
                  <Field key={header.id} orientation="horizontal">
                    <Checkbox
                      id={`export-header-${header.id}`}
                      checked={Boolean(confirmedHeaders[header.id])}
                      onCheckedChange={(checked) =>
                        setHeaderConfirmed(header.id, checked === true)
                      }
                    />
                    <FieldLabel
                      htmlFor={`export-header-${header.id}`}
                      className="min-w-0"
                    >
                      <span className="font-mono">{header.name}</span>
                      <span className="truncate font-mono text-muted-foreground">
                        {header.value}
                      </span>
                    </FieldLabel>
                  </Field>
                ))}
              </FieldGroup>
            ) : null}
            {extraction.notes.length > 0 && (
              <ul className="list-disc pl-5 text-xs text-muted-foreground">
                {extraction.notes.slice(0, 6).map((note) => (
                  <li key={note}>{note}</li>
                ))}
              </ul>
            )}
            <div>
              <Button size="sm" variant="outline" onClick={applyExtraction}>
                {copy.applyExtraction}
              </Button>
            </div>
          </div>
        </OperatorInsetPanel>
      )}
      {enhancedCount > 0 && (
        <p className="mt-3 text-xs text-muted-foreground">
          {copy.enhancedCount.replace("{count}", String(enhancedCount))}
        </p>
      )}
    </OperatorSectionCard>
  );
}
