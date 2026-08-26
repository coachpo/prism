import { useLocale } from "@/i18n/useLocale";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { OperatorSectionCard } from "@/shared/design-system";
import type { ExportPlatform, ExportSourceModelRow } from "./exportTypes";

export function ModelExportDestinationPanel({
  defaultModelConfigId,
  gatewayOrigin,
  gatewayOriginInvalid,
  onDefaultModelChange,
  onGatewayOriginChange,
  onPlatformChange,
  onProviderIdChange,
  platform,
  providerId,
  providerIdInvalid,
  selectedModels,
}: {
  defaultModelConfigId?: number;
  gatewayOrigin: string;
  gatewayOriginInvalid: boolean;
  onDefaultModelChange: (value: number | undefined) => void;
  onGatewayOriginChange: (value: string) => void;
  onPlatformChange: (platform: ExportPlatform) => void;
  onProviderIdChange: (value: string) => void;
  platform: ExportPlatform;
  providerId: string;
  providerIdInvalid: boolean;
  selectedModels: ExportSourceModelRow[];
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;

  return (
    <OperatorSectionCard
      title={copy.commonSettingsTitle}
      description={copy.commonSettingsDescription}
    >
      <FieldGroup className="gap-4 md:grid md:grid-cols-2 xl:grid-cols-4">
        <Field>
          <FieldLabel htmlFor="export-platform">{copy.platformLabel}</FieldLabel>
          <Select
            value={platform}
            onValueChange={(value) => onPlatformChange(value as ExportPlatform)}
          >
            <SelectTrigger id="export-platform" aria-label={copy.platformLabel}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="pi">{copy.platformPi}</SelectItem>
                <SelectItem value="opencode">{copy.platformOpencode}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        <Field data-invalid={gatewayOriginInvalid}>
          <FieldLabel htmlFor="export-gateway-origin">
            {copy.gatewayOriginLabel}
          </FieldLabel>
          <Input
            id="export-gateway-origin"
            value={gatewayOrigin}
            aria-invalid={gatewayOriginInvalid}
            spellCheck={false}
            onChange={(event) => onGatewayOriginChange(event.target.value)}
          />
          <FieldDescription>
            {gatewayOriginInvalid
              ? copy.gatewayOriginInvalid
              : copy.gatewayOriginHint}
          </FieldDescription>
        </Field>
        <Field data-invalid={providerIdInvalid}>
          <FieldLabel htmlFor="export-provider-id">{copy.providerIdLabel}</FieldLabel>
          <Input
            id="export-provider-id"
            value={providerId}
            aria-invalid={providerIdInvalid}
            spellCheck={false}
            onChange={(event) => onProviderIdChange(event.target.value)}
          />
          <FieldDescription>
            {providerIdInvalid ? copy.providerIdInvalid : copy.providerIdHint}
          </FieldDescription>
        </Field>
        {platform === "opencode" && (
          <Field>
            <FieldLabel htmlFor="export-default-model">
              {copy.defaultModelLabel}
            </FieldLabel>
            <Select
              value={
                defaultModelConfigId === undefined
                  ? "none"
                  : String(defaultModelConfigId)
              }
              onValueChange={(value) =>
                onDefaultModelChange(
                  value === "none" ? undefined : Number(value),
                )
              }
            >
              <SelectTrigger
                id="export-default-model"
                aria-label={copy.defaultModelLabel}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="none">{copy.defaultModelNone}</SelectItem>
                  {selectedModels.map((model) => (
                    <SelectItem
                      key={model.model_config_id}
                      value={String(model.model_config_id)}
                    >
                      {model.model_id}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FieldDescription>{copy.defaultModelHint}</FieldDescription>
          </Field>
        )}
      </FieldGroup>
    </OperatorSectionCard>
  );
}
