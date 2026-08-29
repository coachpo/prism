import { useLocale } from "@/i18n/useLocale";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { OperatorSectionCard } from "@/shared/design-system";

export function ModelExportDestinationPanel({
  gatewayOrigin,
  gatewayOriginInvalid,
  onGatewayOriginChange,
  onProviderIdChange,
  providerId,
  providerIdInvalid,
}: {
  gatewayOrigin: string;
  gatewayOriginInvalid: boolean;
  onGatewayOriginChange: (value: string) => void;
  onProviderIdChange: (value: string) => void;
  providerId: string;
  providerIdInvalid: boolean;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  return (
    <OperatorSectionCard
      title={copy.commonSettingsTitle}
      description={copy.commonSettingsDescription}
    >
      <FieldGroup className="gap-4 md:grid md:grid-cols-2">
        <Field data-invalid={gatewayOriginInvalid}>
          <FieldLabel htmlFor="export-gateway-origin">
            {copy.gatewayOriginLabel}
          </FieldLabel>
          <Input
            id="export-gateway-origin"
            value={gatewayOrigin}
            aria-invalid={gatewayOriginInvalid}
            spellCheck={false}
            onChange={(e) => onGatewayOriginChange(e.target.value)}
          />
          <FieldDescription>
            {gatewayOriginInvalid
              ? copy.gatewayOriginInvalid
              : copy.gatewayOriginHint}
          </FieldDescription>
        </Field>
        <Field data-invalid={providerIdInvalid}>
          <FieldLabel htmlFor="export-provider-id">
            {copy.providerIdLabel}
          </FieldLabel>
          <Input
            id="export-provider-id"
            value={providerId}
            aria-invalid={providerIdInvalid}
            spellCheck={false}
            onChange={(e) => onProviderIdChange(e.target.value)}
          />
          <FieldDescription>
            {providerIdInvalid ? copy.providerIdInvalid : copy.providerIdHint}
          </FieldDescription>
        </Field>
      </FieldGroup>
    </OperatorSectionCard>
  );
}
