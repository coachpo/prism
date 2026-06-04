import { useState } from "react";
import { Activity } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLegend, FieldSet } from "@/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import type { BootstrapConfigResponse, BootstrapConfigSecretKey, BootstrapConfigValues } from "@/lib/types";
import {
  TELEMETRY_FIELD_PATHS,
  normalizeTelemetryValues,
  numberValue,
  textValue,
  type FieldErrors,
  type SecretInputState,
  type SettingsStartupCopy,
} from "./startupFieldMetadata";
import {
  SecretReplacementField,
  StartupDisclosure,
  StartupInputField,
  type SectionEffectRenderer,
} from "./StartupServerSection";

interface StartupTelemetrySectionProps {
  bootstrapConfig: BootstrapConfigResponse;
  clearSecretInput: (secretKey: BootstrapConfigSecretKey) => void;
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  fieldErrors: FieldErrors;
  handleSecretInputChange: (secretKey: BootstrapConfigSecretKey, value: string) => void;
  sectionEffect: SectionEffectRenderer;
  secretInputs: SecretInputState;
  setBooleanField: (path: string, checked: boolean) => void;
  setFloatField: (path: string, rawValue: string) => void;
  setStringField: (path: string, rawValue: string) => void;
  setTelemetryAuthMode: (value: string) => void;
  setTelemetryEnabled: (checked: boolean) => void;
  values: BootstrapConfigValues;
}

export function StartupTelemetrySection({
  bootstrapConfig,
  clearSecretInput,
  controlsDisabled,
  copy,
  fieldErrors,
  handleSecretInputChange,
  sectionEffect,
  secretInputs,
  setBooleanField,
  setFloatField,
  setStringField,
  setTelemetryAuthMode,
  setTelemetryEnabled,
  values,
}: StartupTelemetrySectionProps) {
  const telemetryValues = normalizeTelemetryValues(values.telemetry);
  const telemetryEnabled = Boolean(telemetryValues.enabled);
  const exporter = telemetryValues.exporter;
  const authMode = exporter?.auth?.mode ?? "";
  const telemetryControlsDisabled = controlsDisabled || !telemetryEnabled;
  const tracesEnabled = telemetryEnabled && Boolean(telemetryValues.traces?.enabled);
  const advancedTelemetryActive = telemetryEnabled && (
    authMode === "authorization_header"
    || Boolean(bootstrapConfig.secrets["telemetry.exporter.auth.authorizationHeader"].configured)
    || Boolean(exporter?.tls?.insecure_skip_verify)
    || Boolean(exporter?.tls?.ca_file)
    || Boolean(telemetryValues.metrics?.enabled)
    || Boolean(telemetryValues.traces?.enabled)
    || telemetryValues.traces?.sampling_ratio !== null
    || Boolean(fieldErrors["telemetry.exporter.auth.mode"])
    || Boolean(fieldErrors["telemetry.exporter.auth.authorizationHeader"])
    || Boolean(fieldErrors["telemetry.exporter.tls.ca_file"])
    || Boolean(fieldErrors["telemetry.traces.sampling_ratio"])
  );
  const [advancedOpenOverride, setAdvancedOpenOverride] = useState<boolean | null>(null);
  const advancedOpen = advancedOpenOverride ?? advancedTelemetryActive;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2 text-sm">
          <Activity />
          {copy.telemetrySectionTitle}
          {sectionEffect(TELEMETRY_FIELD_PATHS)}
        </CardTitle>
        <CardDescription>{copy.telemetrySectionDescription}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <FieldSet disabled={controlsDisabled}>
          <FieldLegend>{copy.telemetry}</FieldLegend>
          <FieldGroup>
            <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
              <FieldContent>
                <label htmlFor="startup-telemetry-enabled" className="text-sm font-medium leading-none">{copy.telemetryEnabled}</label>
                <FieldDescription>{copy.telemetryEnabledDescription}</FieldDescription>
              </FieldContent>
              <Switch id="startup-telemetry-enabled" checked={telemetryEnabled} disabled={controlsDisabled} onCheckedChange={setTelemetryEnabled} />
            </Field>
          </FieldGroup>
        </FieldSet>
        <Separator />
        <FieldSet disabled={telemetryControlsDisabled}>
          <FieldLegend>{copy.telemetryProtocolAndExporterTitle}</FieldLegend>
          <FieldDescription>{copy.telemetryExporter}</FieldDescription>
          <FieldGroup>
            <div className="grid gap-4 md:grid-cols-2">
              <StartupInputField
                id="startup-telemetry-endpoint"
                label={copy.telemetryExporterEndpoint}
                value={textValue(exporter?.endpoint ?? null)}
                placeholder={copy.telemetryExporterEndpointPlaceholder}
                error={fieldErrors["telemetry.exporter.endpoint"]}
                disabled={telemetryControlsDisabled}
                onChange={(value) => setStringField("telemetry.exporter.endpoint", value)}
              />
              <Field data-invalid={Boolean(fieldErrors["telemetry.exporter.protocol"]) || undefined} data-disabled={telemetryControlsDisabled || undefined}>
                <label htmlFor="startup-telemetry-protocol" className="text-sm font-medium leading-none">{copy.telemetryExporterProtocol}</label>
                <Select value={exporter?.protocol ?? ""} disabled={telemetryControlsDisabled} onValueChange={(value) => setStringField("telemetry.exporter.protocol", value)}>
                  <SelectTrigger id="startup-telemetry-protocol" aria-invalid={Boolean(fieldErrors["telemetry.exporter.protocol"]) || undefined}>
                    <SelectValue placeholder={copy.telemetryExporterProtocolPlaceholder} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="grpc">{copy.telemetryExporterProtocolGrpc}</SelectItem>
                      <SelectItem value="http/protobuf">{copy.telemetryExporterProtocolHttpProtobuf}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError>{fieldErrors["telemetry.exporter.protocol"]}</FieldError>
              </Field>
              <Field data-invalid={Boolean(fieldErrors["telemetry.exporter.compression"]) || undefined} data-disabled={telemetryControlsDisabled || undefined}>
                <label htmlFor="startup-telemetry-compression" className="text-sm font-medium leading-none">{copy.telemetryExporterCompression}</label>
                <Select value={exporter?.compression ?? ""} disabled={telemetryControlsDisabled} onValueChange={(value) => setStringField("telemetry.exporter.compression", value)}>
                  <SelectTrigger id="startup-telemetry-compression" aria-invalid={Boolean(fieldErrors["telemetry.exporter.compression"]) || undefined}>
                    <SelectValue placeholder={copy.telemetryExporterCompressionPlaceholder} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="none">{copy.telemetryCompressionNone}</SelectItem>
                      <SelectItem value="gzip">{copy.telemetryCompressionGzip}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldError>{fieldErrors["telemetry.exporter.compression"]}</FieldError>
              </Field>
              <StartupInputField
                id="startup-telemetry-timeout"
                label={copy.telemetryExporterTimeout}
                value={textValue(exporter?.timeout ?? null)}
                placeholder={copy.telemetryExporterTimeoutPlaceholder}
                error={fieldErrors["telemetry.exporter.timeout"]}
                disabled={telemetryControlsDisabled}
                onChange={(value) => setStringField("telemetry.exporter.timeout", value)}
              />
            </div>
          </FieldGroup>
        </FieldSet>
        <StartupDisclosure
          testId="startup-telemetry-advanced-toggle"
          open={advancedOpen}
          onOpenChange={setAdvancedOpenOverride}
          closedLabel={copy.showAdvancedTelemetry}
          openLabel={copy.hideAdvancedTelemetry}
          description={copy.advancedTelemetryDescription}
        >
          <div className="space-y-6">
            <FieldSet disabled={telemetryControlsDisabled}>
              <FieldLegend>{copy.telemetryExporterAuthMode}</FieldLegend>
              <FieldGroup>
                <Field data-invalid={Boolean(fieldErrors["telemetry.exporter.auth.mode"]) || undefined} data-disabled={telemetryControlsDisabled || undefined}>
                  <label htmlFor="startup-telemetry-auth-mode" className="text-sm font-medium leading-none">{copy.telemetryExporterAuthMode}</label>
                  <Select value={authMode} disabled={telemetryControlsDisabled} onValueChange={setTelemetryAuthMode}>
                    <SelectTrigger id="startup-telemetry-auth-mode" aria-invalid={Boolean(fieldErrors["telemetry.exporter.auth.mode"]) || undefined}>
                      <SelectValue placeholder={copy.telemetryExporterAuthModePlaceholder} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="none">{copy.telemetryExporterAuthModeNone}</SelectItem>
                        <SelectItem value="authorization_header">{copy.telemetryExporterAuthModeAuthorizationHeader}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldError>{fieldErrors["telemetry.exporter.auth.mode"]}</FieldError>
                </Field>
                <SecretReplacementField
                  id="startup-telemetry-authorization-header"
                  label={copy.telemetryAuthorizationHeader}
                  secretKey="telemetry.exporter.auth.authorizationHeader"
                  masked={bootstrapConfig.secrets["telemetry.exporter.auth.authorizationHeader"].masked}
                  configured={bootstrapConfig.secrets["telemetry.exporter.auth.authorizationHeader"].configured}
                  editable={telemetryEnabled && authMode === "authorization_header" && bootstrapConfig.secrets["telemetry.exporter.auth.authorizationHeader"].editable && !controlsDisabled}
                  value={secretInputs["telemetry.exporter.auth.authorizationHeader"]}
                  copy={copy}
                  error={fieldErrors["telemetry.exporter.auth.authorizationHeader"]}
                  onChange={handleSecretInputChange}
                  onClear={clearSecretInput}
                />
                <FieldDescription>{copy.telemetryAuthorizationHeaderDescription}</FieldDescription>
              </FieldGroup>
            </FieldSet>
            <Separator />
            <FieldSet disabled={telemetryControlsDisabled}>
              <FieldLegend>{copy.telemetryTls}</FieldLegend>
              <FieldGroup>
                <Field orientation="horizontal" data-disabled={telemetryControlsDisabled || undefined}>
                  <FieldContent>
                    <label htmlFor="startup-telemetry-tls-insecure-skip-verify" className="text-sm font-medium leading-none">{copy.telemetryTlsInsecureSkipVerify}</label>
                    <FieldDescription>{copy.telemetryTlsInsecureSkipVerifyDescription}</FieldDescription>
                  </FieldContent>
                  <Switch id="startup-telemetry-tls-insecure-skip-verify" checked={Boolean(exporter?.tls?.insecure_skip_verify)} disabled={telemetryControlsDisabled} onCheckedChange={(checked) => setBooleanField("telemetry.exporter.tls.insecure_skip_verify", checked)} />
                </Field>
                <StartupInputField
                  id="startup-telemetry-tls-ca-file"
                  label={copy.telemetryTlsCaFile}
                  value={textValue(exporter?.tls?.ca_file ?? null)}
                  placeholder={copy.telemetryTlsCaFilePlaceholder}
                  description={copy.telemetryTlsCaFileDescription}
                  error={fieldErrors["telemetry.exporter.tls.ca_file"]}
                  disabled={telemetryControlsDisabled}
                  onChange={(value) => setStringField("telemetry.exporter.tls.ca_file", value)}
                />
              </FieldGroup>
            </FieldSet>
            <Separator />
            <FieldSet disabled={telemetryControlsDisabled}>
              <FieldLegend>{copy.telemetrySignals}</FieldLegend>
              <FieldDescription>{copy.telemetrySignalsDescription}</FieldDescription>
              <FieldGroup>
                <Field orientation="horizontal" data-disabled={telemetryControlsDisabled || undefined}>
                  <FieldContent>
                    <span className="text-sm font-medium leading-none">{copy.telemetryMetricsEnabled}</span>
                  </FieldContent>
                  <Switch id="startup-telemetry-metrics-enabled" aria-label={copy.telemetryMetricsEnabled} checked={Boolean(telemetryValues.metrics?.enabled)} disabled={telemetryControlsDisabled} onCheckedChange={(checked) => setBooleanField("telemetry.metrics.enabled", checked)} />
                </Field>
                <Field orientation="horizontal" data-disabled={telemetryControlsDisabled || undefined}>
                  <FieldContent>
                    <span className="text-sm font-medium leading-none">{copy.telemetryTracesEnabled}</span>
                  </FieldContent>
                  <Switch id="startup-telemetry-traces-enabled" aria-label={copy.telemetryTracesEnabled} checked={Boolean(telemetryValues.traces?.enabled)} disabled={telemetryControlsDisabled} onCheckedChange={(checked) => setBooleanField("telemetry.traces.enabled", checked)} />
                </Field>
                <StartupInputField
                  id="startup-telemetry-traces-sampling-ratio"
                  label={copy.telemetryTracesSamplingRatio}
                  type="number"
                  value={numberValue(telemetryValues.traces?.sampling_ratio ?? null)}
                  placeholder="1"
                  description={copy.telemetryTracesSamplingRatioDescription}
                  error={fieldErrors["telemetry.traces.sampling_ratio"]}
                  disabled={telemetryControlsDisabled || !tracesEnabled}
                  onChange={(value) => setFloatField("telemetry.traces.sampling_ratio", value)}
                />
              </FieldGroup>
            </FieldSet>
          </div>
        </StartupDisclosure>
      </CardContent>
    </Card>
  );
}
