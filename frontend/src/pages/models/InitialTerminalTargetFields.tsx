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
import { useLocale } from "@/i18n/useLocale";
import type { Endpoint } from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system";

interface InitialTerminalTargetFieldsProps {
  endpointId: number | null;
  endpoints: Endpoint[];
  inlineApiKey: string;
  inlineBaseUrl: string;
  inlineEndpoint: boolean;
  inlineName: string;
  modelId: string;
  setEndpointId: (value: number | null) => void;
  setInlineApiKey: (value: string) => void;
  setInlineBaseUrl: (value: string) => void;
  setInlineEndpoint: (value: boolean) => void;
  setInlineName: (value: string) => void;
  endpointsLoading: boolean;
  endpointsError: boolean;
  onRetryEndpoints: () => void;
  invalidField: string | null;
  fieldError: string | null;
  upstreamModelId: string;
  upstreamModelIdError: string | null;
  onUpstreamModelIdChange: (value: string) => void;
}

/** The first Terminal Target portion of the atomic model-create form. */
export function InitialTerminalTargetFields({
  endpointId,
  endpoints,
  inlineApiKey,
  inlineBaseUrl,
  inlineEndpoint,
  inlineName,
  modelId,
  setEndpointId,
  setInlineApiKey,
  setInlineBaseUrl,
  setInlineEndpoint,
  setInlineName,
  endpointsLoading,
  endpointsError,
  onRetryEndpoints,
  invalidField,
  fieldError,
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
      {inlineEndpoint ? (
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          <Field className="gap-1.5">
            <FieldLabel htmlFor="create-target-inline-name">
              {copy.endpointNameLabel}
            </FieldLabel>
            <Input
              id="create-target-inline-name"
              value={inlineName}
              aria-required="true"
              aria-invalid={invalidField === "create-target-inline-name"}
              onChange={(event) => setInlineName(event.target.value)}
            />
            {invalidField === "create-target-inline-name" ? <FieldError>{fieldError}</FieldError> : null}
          </Field>
          <Field className="gap-1.5">
            <FieldLabel htmlFor="create-target-inline-url">
              {copy.endpointBaseUrlLabel}
            </FieldLabel>
            <Input
              id="create-target-inline-url"
              value={inlineBaseUrl}
              placeholder="https://api.example.com"
              className="font-mono"
              aria-required="true"
              aria-invalid={invalidField === "create-target-inline-url"}
              aria-describedby="create-target-inline-url-help"
              onChange={(event) => setInlineBaseUrl(event.target.value)}
            />
            <FieldDescription id="create-target-inline-url-help">{messages.endpointsUi.baseUrlHelp}</FieldDescription>
            {invalidField === "create-target-inline-url" ? <FieldError>{fieldError}</FieldError> : null}
            {/\/v1\/?$/.test(inlineBaseUrl.trim()) ? <OperatorCallout intent="warning" description={messages.endpointsUi.baseUrlVersionWarning} action={<Button type="button" variant="outline" onClick={() => setInlineBaseUrl(inlineBaseUrl.trim().replace(/\/v1\/?$/, ""))}>{messages.endpointsUi.useServiceRoot}</Button>} /> : null}
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
            disabled={endpointsLoading || endpointsError}
            onValueChange={(value) =>
              setEndpointId(value === "" ? null : Number(value))
            }
          >
            <SelectTrigger id="create-target-endpoint" className="w-full" aria-invalid={invalidField === "create-target-endpoint"}>
              <SelectValue placeholder={endpointsLoading ? copy.servicesLoading : copy.selectService} />
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
          {invalidField === "create-target-endpoint" ? <FieldError>{fieldError}</FieldError> : null}
          {endpointsError ? <OperatorCallout intent="danger" description={copy.servicesLoadFailed} action={<Button type="button" variant="outline" onClick={onRetryEndpoints}>{messages.common.retry}</Button>} /> : null}
        </Field>
      )}
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
          aria-describedby="create-target-upstream-model-help"
          onChange={(event) => onUpstreamModelIdChange(event.target.value)}
        />
        {upstreamModelIdError ? (
          <FieldError id="create-target-upstream-model-help">{upstreamModelIdError}</FieldError>
        ) : (
          <FieldDescription id="create-target-upstream-model-help">
            {modelId.trim()
              ? copy.initialTargetUpstreamModelIdHint(modelId.trim())
              : copy.initialTargetUpstreamModelIdHintEmpty}
          </FieldDescription>
        )}
      </Field>
    </OperatorInsetPanel>
  );
}
