import { Network } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  FieldDescription,
  FieldGroup,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Separator } from "@/components/ui/separator";
import type { BootstrapConfigValues } from "@/lib/types";
import {
  RUNTIME_FIELD_PATHS,
  numberValue,
  textValue,
  type FieldErrors,
  type SettingsStartupCopy,
} from "./startupFieldMetadata";
import {
  StartupInputField,
  type FieldEffectRenderer,
  type SectionEffectRenderer,
} from "./StartupServerSection";

interface StartupRuntimeSectionProps {
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  fieldErrors: FieldErrors;
  fieldEffect: FieldEffectRenderer;
  sectionEffect: SectionEffectRenderer;
  setNumberField: (path: string, rawValue: string) => void;
  setStringField: (path: string, rawValue: string) => void;
  values: BootstrapConfigValues;
}

export function StartupRuntimeSection({
  controlsDisabled,
  copy,
  fieldErrors,
  fieldEffect,
  sectionEffect,
  setNumberField,
  setStringField,
  values,
}: StartupRuntimeSectionProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2 text-sm">
          <Network />
          {copy.transportTitle}
          {sectionEffect(RUNTIME_FIELD_PATHS)}
        </CardTitle>
        <CardDescription>{copy.transportDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldSet disabled={controlsDisabled}>
          <FieldLegend>{copy.transport}</FieldLegend>
          <FieldGroup>
            <div className="grid gap-4 md:grid-cols-2">
              <StartupInputField
                id="startup-max-idle-conns"
                label={copy.maxIdleConns}
                effect={fieldEffect("runtime.transport.max_idle_conns")}
                type="number"
                value={numberValue(values.runtime.transport.max_idle_conns)}
                error={fieldErrors["runtime.transport.max_idle_conns"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("runtime.transport.max_idle_conns", value)}
              />
              <StartupInputField
                id="startup-max-idle-per-host"
                label={copy.maxIdlePerHost}
                effect={fieldEffect("runtime.transport.max_idle_conns_per_host")}
                type="number"
                value={numberValue(values.runtime.transport.max_idle_conns_per_host)}
                error={fieldErrors["runtime.transport.max_idle_conns_per_host"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("runtime.transport.max_idle_conns_per_host", value)}
              />
              <StartupInputField
                id="startup-max-conns-per-host"
                label={copy.maxConnsPerHost}
                effect={fieldEffect("runtime.transport.max_conns_per_host")}
                type="number"
                value={numberValue(values.runtime.transport.max_conns_per_host)}
                error={fieldErrors["runtime.transport.max_conns_per_host"]}
                disabled={controlsDisabled}
                onChange={(value) => setNumberField("runtime.transport.max_conns_per_host", value)}
              />
              <StartupInputField
                id="startup-idle-timeout"
                label={copy.idleConnTimeout}
                effect={fieldEffect("runtime.transport.idle_conn_timeout")}
                value={textValue(values.runtime.transport.idle_conn_timeout)}
                error={fieldErrors["runtime.transport.idle_conn_timeout"]}
                disabled={controlsDisabled}
                onChange={(value) => setStringField("runtime.transport.idle_conn_timeout", value)}
              />
              <StartupInputField
                id="startup-request-timeout"
                label={copy.requestTimeout}
                effect={fieldEffect("runtime.transport.request_timeout")}
                value={textValue(values.runtime.transport.request_timeout)}
                error={fieldErrors["runtime.transport.request_timeout"]}
                disabled={controlsDisabled}
                onChange={(value) => setStringField("runtime.transport.request_timeout", value)}
              />
              <StartupInputField
                id="startup-response-header-timeout"
                label={copy.responseHeaderTimeout}
                effect={fieldEffect("runtime.transport.response_header_timeout")}
                value={textValue(values.runtime.transport.response_header_timeout)}
                error={fieldErrors["runtime.transport.response_header_timeout"]}
                disabled={controlsDisabled}
                onChange={(value) => setStringField("runtime.transport.response_header_timeout", value)}
              />
              <StartupInputField
                id="startup-tls-timeout"
                label={copy.tlsHandshakeTimeout}
                effect={fieldEffect("runtime.transport.tls_handshake_timeout")}
                value={textValue(values.runtime.transport.tls_handshake_timeout)}
                error={fieldErrors["runtime.transport.tls_handshake_timeout"]}
                disabled={controlsDisabled}
                onChange={(value) => setStringField("runtime.transport.tls_handshake_timeout", value)}
              />
              <StartupInputField
                id="startup-expect-timeout"
                label={copy.expectContinueTimeout}
                effect={fieldEffect("runtime.transport.expect_continue_timeout")}
                value={textValue(values.runtime.transport.expect_continue_timeout)}
                error={fieldErrors["runtime.transport.expect_continue_timeout"]}
                disabled={controlsDisabled}
                onChange={(value) => setStringField("runtime.transport.expect_continue_timeout", value)}
              />
            </div>
          </FieldGroup>
        </FieldSet>
        <Separator className="my-6" />
        <FieldSet disabled={controlsDisabled}>
          <FieldLegend>{copy.runtimeSideEffects}</FieldLegend>
          <FieldDescription>{copy.runtimeSideEffectsDescription}</FieldDescription>
          <FieldGroup>
            <StartupInputField
              id="startup-side-effects-attempt-timeout"
              label={copy.sideEffectsAttemptTimeout}
              description={copy.sideEffectsAttemptTimeoutDescription}
              effect={fieldEffect("runtime.side_effects.attempt_timeout")}
              value={textValue(values.runtime.side_effects.attempt_timeout)}
              error={fieldErrors["runtime.side_effects.attempt_timeout"]}
              disabled={controlsDisabled}
              onChange={(value) => setStringField("runtime.side_effects.attempt_timeout", value)}
            />
          </FieldGroup>
        </FieldSet>
      </CardContent>
    </Card>
  );
}
