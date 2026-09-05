import { Button } from "@/components/ui/button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { getOpenAIAcceptedFormatOptionLabel } from "@/features/models/openaiCapabilityOptions";
import { useLocale } from "@/i18n/useLocale";
import type { Endpoint, OpenAIAcceptedFormat } from "@/lib/types";
import { OperatorInsetPanel } from "@/shared/design-system";

interface InitialTerminalTargetFieldsProps {
  apiFamily: "openai" | "anthropic" | "gemini";
  endpointId: number | null;
  endpoints: Endpoint[];
  inlineApiKey: string;
  inlineBaseUrl: string;
  inlineEndpoint: boolean;
  inlineName: string;
  modelId: string;
  resolvedAcceptedFormat: OpenAIAcceptedFormat | null;
  setEndpointId: (value: number | null) => void;
  setInlineApiKey: (value: string) => void;
  setInlineBaseUrl: (value: string) => void;
  setInlineEndpoint: (value: boolean) => void;
  setInlineName: (value: string) => void;
  setTargetName: (value: string) => void;
  targetName: string;
  upstreamModelId: string;
  upstreamModelIdError: string | null;
  onUpstreamModelIdChange: (value: string) => void;
}

/** The first Terminal Target portion of the atomic model-create form. */
export function InitialTerminalTargetFields({
  apiFamily,
  endpointId,
  endpoints,
  inlineApiKey,
  inlineBaseUrl,
  inlineEndpoint,
  inlineName,
  modelId,
  resolvedAcceptedFormat,
  setEndpointId,
  setInlineApiKey,
  setInlineBaseUrl,
  setInlineEndpoint,
  setInlineName,
  setTargetName,
  targetName,
  upstreamModelId,
  upstreamModelIdError,
  onUpstreamModelIdChange,
}: InitialTerminalTargetFieldsProps) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;

  return (
    <OperatorInsetPanel data-testid="initial-terminal-target-section">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium">{copy.initialTargetTitle}</h3>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setInlineEndpoint(!inlineEndpoint)}
          type="button"
        >
          {inlineEndpoint
            ? copy.initialTargetUseExisting
            : copy.initialTargetCreateInline}
        </Button>
      </div>
      {apiFamily === "openai" && resolvedAcceptedFormat ? (
        <p className="mt-2 text-xs text-muted-foreground">
          {copy.ownerDerivedCapability}:{" "}
          {getOpenAIAcceptedFormatOptionLabel(resolvedAcceptedFormat, copy)}（
          {copy.ownerDerivedReadOnly}）
        </p>
      ) : null}
      {inlineEndpoint ? (
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <Field className="gap-1.5">
            <FieldLabel htmlFor="create-target-inline-name">
              {copy.endpointNameLabel}
            </FieldLabel>
            <Input
              id="create-target-inline-name"
              value={inlineName}
              onChange={(event) => setInlineName(event.target.value)}
            />
          </Field>
          <Field className="gap-1.5">
            <FieldLabel htmlFor="create-target-inline-url">
              {copy.endpointBaseUrlLabel}
            </FieldLabel>
            <Input
              id="create-target-inline-url"
              value={inlineBaseUrl}
              onChange={(event) => setInlineBaseUrl(event.target.value)}
            />
          </Field>
          <Field className="gap-1.5 sm:col-span-2">
            <FieldLabel htmlFor="create-target-inline-key">
              {copy.endpointApiKeyLabel}
            </FieldLabel>
            <Input
              id="create-target-inline-key"
              type="password"
              value={inlineApiKey}
              onChange={(event) => setInlineApiKey(event.target.value)}
            />
          </Field>
        </div>
      ) : (
        <Field className="mt-3 gap-1.5">
          <FieldLabel htmlFor="create-target-endpoint">
            {copy.endpointLabel}
          </FieldLabel>
          <Select
            value={endpointId === null ? "" : String(endpointId)}
            onValueChange={(value) =>
              setEndpointId(value === "" ? null : Number(value))
            }
          >
            <SelectTrigger id="create-target-endpoint" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {endpoints.map((endpoint) => (
                  <SelectItem key={endpoint.id} value={String(endpoint.id)}>
                    {endpoint.name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
      )}
      <Field className="mt-3 gap-1.5">
        <FieldLabel htmlFor="create-target-name">
          {copy.targetNameLabel}
        </FieldLabel>
        <Input
          id="create-target-name"
          value={targetName}
          onChange={(event) => setTargetName(event.target.value)}
        />
      </Field>
      <Field
        data-invalid={upstreamModelIdError != null}
        className="mt-3 gap-1.5"
      >
        <FieldLabel htmlFor="create-target-upstream-model-id">
          {copy.initialTargetUpstreamModelIdLabel}
        </FieldLabel>
        <Input
          id="create-target-upstream-model-id"
          className="font-mono"
          autoComplete="off"
          placeholder={copy.initialTargetUpstreamModelIdPlaceholder}
          value={upstreamModelId}
          aria-invalid={upstreamModelIdError != null}
          onChange={(event) => onUpstreamModelIdChange(event.target.value)}
        />
        {upstreamModelIdError ? (
          <FieldError>{upstreamModelIdError}</FieldError>
        ) : (
          <FieldDescription>
            {modelId.trim()
              ? copy.initialTargetUpstreamModelIdHint(modelId.trim())
              : copy.initialTargetUpstreamModelIdHintEmpty}
          </FieldDescription>
        )}
      </Field>
    </OperatorInsetPanel>
  );
}
