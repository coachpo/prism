import { Activity } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
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
  FieldLabelWithEffect,
  SecretReplacementField,
  StartupInputField,
  type FieldEffectRenderer,
  type SectionEffectRenderer,
} from "./StartupServerSection";

interface StartupTelemetrySectionProps {
  bootstrapConfig: BootstrapConfigResponse;
  clearSecretInput: (secretKey: BootstrapConfigSecretKey) => void;
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  fieldErrors: FieldErrors;
  fieldEffect: FieldEffectRenderer;
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
  fieldEffect,
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
      <CardContent>
        <FieldSet disabled={controlsDisabled}>
          <FieldLegend>{copy.telemetry}</FieldLegend>
          <FieldGroup>
            <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
              <FieldContent>
                <FieldLabelWithEffect htmlFor="startup-telemetry-enabled" label={copy.telemetryEnabled} effect={fieldEffect("telemetry.enabled")} />
                <FieldDescription>{copy.telemetryEnabledDescription}</FieldDescription>
              </FieldContent>
              <Switch id="startup-telemetry-enabled" checked={telemetryEnabled} disabled={controlsDisabled} onCheckedChange={setTelemetryEnabled} />
            </Field>
          </FieldGroup>
        </FieldSet>
        <Separator className="my-6" />
        <FieldSet disabled={telemetryControlsDisabled}>
          <FieldLegend>{copy.telemetryProtocolAndExporterTitle}</FieldLegend>
          <FieldDescription>{copy.telemetryExporter}</FieldDescription>
          <FieldGroup>
            <div className="grid gap-4 md:grid-cols-2">
              <StartupInputField
                id="startup-telemetry-endpoint"
                label={copy.telemetryExporterEndpoint}
                effect={fieldEffect("telemetry.exporter.endpoint")}
                value={textValue(exporter?.endpoint ?? null)}
                placeholder={copy.telemetryExporterEndpointPlaceholder}
                error={fieldErrors["telemetry.exporter.endpoint"]}
                disabled={telemetryControlsDisabled}
                onChange={(value) => setStringField("telemetry.exporter.endpoint", value)}
              />
              <Field data-invalid={Boolean(fieldErrors["telemetry.exporter.protocol"]) || undefined} data-disabled={telemetryControlsDisabled || undefined}>
                <FieldLabelWithEffect htmlFor="startup-telemetry-protocol" label={copy.telemetryExporterProtocol} effect={fieldEffect("telemetry.exporter.protocol")} />
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
                <FieldLabelWithEffect htmlFor="startup-telemetry-compression" label={copy.telemetryExporterCompression} effect={fieldEffect("telemetry.exporter.compression")} />
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
                effect={fieldEffect("telemetry.exporter.timeout")}
                value={textValue(exporter?.timeout ?? null)}
                placeholder={copy.telemetryExporterTimeoutPlaceholder}
                error={fieldErrors["telemetry.exporter.timeout"]}
                disabled={telemetryControlsDisabled}
                onChange={(value) => setStringField("telemetry.exporter.timeout", value)}
              />
              <Field data-invalid={Boolean(fieldErrors["telemetry.exporter.auth.mode"]) || undefined} data-disabled={telemetryControlsDisabled || undefined}>
                <FieldLabelWithEffect htmlFor="startup-telemetry-auth-mode" label={copy.telemetryExporterAuthMode} effect={fieldEffect("telemetry.exporter.auth.mode")} />
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
            </div>
            <SecretReplacementField
              id="startup-telemetry-authorization-header"
              label={copy.telemetryAuthorizationHeader}
              effect={fieldEffect("telemetry.exporter.auth.authorizationHeader")}
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
        <Separator className="my-6" />
        <FieldSet disabled={telemetryControlsDisabled}>
          <FieldLegend>{copy.telemetryTls}</FieldLegend>
          <FieldGroup>
            <Field orientation="horizontal" data-disabled={telemetryControlsDisabled || undefined}>
              <FieldContent>
                <FieldLabelWithEffect htmlFor="startup-telemetry-tls-insecure-skip-verify" label={copy.telemetryTlsInsecureSkipVerify} effect={fieldEffect("telemetry.exporter.tls.insecure_skip_verify")} />
                <FieldDescription>{copy.telemetryTlsInsecureSkipVerifyDescription}</FieldDescription>
              </FieldContent>
              <Switch id="startup-telemetry-tls-insecure-skip-verify" checked={Boolean(exporter?.tls?.insecure_skip_verify)} disabled={telemetryControlsDisabled} onCheckedChange={(checked) => setBooleanField("telemetry.exporter.tls.insecure_skip_verify", checked)} />
            </Field>
            <StartupInputField
              id="startup-telemetry-tls-ca-file"
              label={copy.telemetryTlsCaFile}
              effect={fieldEffect("telemetry.exporter.tls.ca_file")}
              value={textValue(exporter?.tls?.ca_file ?? null)}
              placeholder={copy.telemetryTlsCaFilePlaceholder}
              description={copy.telemetryTlsCaFileDescription}
              error={fieldErrors["telemetry.exporter.tls.ca_file"]}
              disabled={telemetryControlsDisabled}
              onChange={(value) => setStringField("telemetry.exporter.tls.ca_file", value)}
            />
          </FieldGroup>
        </FieldSet>
        <Separator className="my-6" />
        <FieldSet disabled={telemetryControlsDisabled}>
          <FieldLegend>{copy.telemetrySignals}</FieldLegend>
          <FieldDescription>{copy.telemetrySignalsDescription}</FieldDescription>
          <FieldGroup>
            <Field orientation="horizontal" data-disabled={telemetryControlsDisabled || undefined}>
              <FieldContent>
                <FieldLabelWithEffect htmlFor="startup-telemetry-metrics-enabled" label={copy.telemetryMetricsEnabled} effect={fieldEffect("telemetry.metrics.enabled")} />
              </FieldContent>
              <Switch id="startup-telemetry-metrics-enabled" checked={Boolean(telemetryValues.metrics?.enabled)} disabled={telemetryControlsDisabled} onCheckedChange={(checked) => setBooleanField("telemetry.metrics.enabled", checked)} />
            </Field>
            <Field orientation="horizontal" data-disabled={telemetryControlsDisabled || undefined}>
              <FieldContent>
                <FieldLabelWithEffect htmlFor="startup-telemetry-traces-enabled" label={copy.telemetryTracesEnabled} effect={fieldEffect("telemetry.traces.enabled")} />
              </FieldContent>
              <Switch id="startup-telemetry-traces-enabled" checked={Boolean(telemetryValues.traces?.enabled)} disabled={telemetryControlsDisabled} onCheckedChange={(checked) => setBooleanField("telemetry.traces.enabled", checked)} />
            </Field>
            <StartupInputField
              id="startup-telemetry-traces-sampling-ratio"
              label={copy.telemetryTracesSamplingRatio}
              effect={fieldEffect("telemetry.traces.sampling_ratio")}
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
      </CardContent>
    </Card>
  );
}
