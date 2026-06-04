import type { ReactNode } from "react";
import { ChevronRight, FileJson, Server } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type {
  BootstrapConfigApplyMode,
  BootstrapConfigFieldCapability,
  BootstrapConfigResponse,
  BootstrapConfigSecretKey,
  BootstrapConfigValues,
} from "@/lib/types";
import {
  SERVER_FIELD_PATHS,
  formatDateTime,
  getCapabilityLabel,
  getCapabilityVariant,
  numberValue,
  textValue,
  type FieldErrors,
  type SettingsStartupCopy,
} from "./startupFieldMetadata";

export type FieldEffectRenderer = (field: string) => ReactNode;
export type SectionEffectRenderer = (fields: string[]) => ReactNode;

export function LoadingSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-20" />
      <div className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-64" />
        <Skeleton className="h-64" />
        <Skeleton className="h-64" />
        <Skeleton className="h-64" />
      </div>
    </div>
  );
}

export function FieldEffectBadge({ capability, copy }: { capability?: BootstrapConfigFieldCapability; copy: SettingsStartupCopy }) {
  if (!capability) {
    return null;
  }
  return <Badge variant={getCapabilityVariant(capability.mode)}>{getCapabilityLabel(copy, capability.mode)}</Badge>;
}

export function SectionEffectBadge({ capabilities, copy, fields }: { capabilities: Record<string, BootstrapConfigFieldCapability>; copy: SettingsStartupCopy; fields: string[] }) {
  const modes = new Set(fields.map((field) => capabilities[field]?.mode).filter(Boolean));
  if (modes.size === 0) {
    return null;
  }
  if (modes.size > 1) {
    return <Badge variant="secondary">{copy.mixedEffects}</Badge>;
  }
  const [mode] = [...modes] as BootstrapConfigApplyMode[];
  return <FieldEffectBadge capability={{ mode }} copy={copy} />;
}

export function FieldLabelWithEffect({ effect, htmlFor, label }: { effect?: ReactNode; htmlFor?: string; label: string }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <FieldLabel htmlFor={htmlFor}>{label}</FieldLabel>
      {effect}
    </div>
  );
}

export function FieldLegendWithEffect({ effect, label }: { effect?: ReactNode; label: string }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <FieldLegend>{label}</FieldLegend>
      {effect}
    </div>
  );
}

interface StartupDisclosureProps {
  children: ReactNode;
  closedLabel: string;
  description?: string;
  open: boolean;
  openLabel: string;
  testId: string;
  onOpenChange: (open: boolean) => void;
}

export function StartupDisclosure({
  children,
  closedLabel,
  description,
  open,
  openLabel,
  testId,
  onOpenChange,
}: StartupDisclosureProps) {
  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <div className="rounded-lg border bg-muted/20 px-3 py-2.5">
        <CollapsibleTrigger
          data-testid={testId}
          className="flex w-full items-start justify-between gap-3 rounded-md text-left text-sm font-medium transition-colors hover:bg-muted/50"
        >
          <span className="flex min-w-0 flex-col gap-1">
            <span>{open ? openLabel : closedLabel}</span>
            {description ? <span className="text-xs font-normal text-muted-foreground">{description}</span> : null}
          </span>
          <ChevronRight className={cn("mt-0.5 h-4 w-4 shrink-0 transition-transform", open && "rotate-90")} />
        </CollapsibleTrigger>
        <CollapsibleContent data-testid={`${testId}-content`} className="pt-4">
          {children}
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}

interface StartupFieldProps {
  id: string;
  label: string;
  value: string;
  description?: string;
  effect?: ReactNode;
  error?: string;
  type?: string;
  placeholder?: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}

export function StartupInputField({
  id,
  label,
  value,
  description,
  effect,
  error,
  type = "text",
  placeholder,
  disabled = false,
  onChange,
}: StartupFieldProps) {
  const invalid = Boolean(error);
  return (
    <Field data-invalid={invalid || undefined} data-disabled={disabled || undefined}>
      <FieldLabelWithEffect htmlFor={id} label={label} effect={effect} />
      <Input
        id={id}
        type={type}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        aria-invalid={invalid || undefined}
        onChange={(event) => onChange(event.target.value)}
      />
      {description ? <FieldDescription>{description}</FieldDescription> : null}
      <FieldError>{error}</FieldError>
    </Field>
  );
}

interface SecretReplacementFieldProps {
  id: string;
  label: string;
  secretKey: BootstrapConfigSecretKey;
  masked: string;
  configured: boolean;
  editable: boolean;
  value: string;
  copy: SettingsStartupCopy;
  effect?: ReactNode;
  error?: string;
  onChange: (secretKey: BootstrapConfigSecretKey, value: string) => void;
  onClear: (secretKey: BootstrapConfigSecretKey) => void;
}

export function SecretReplacementField({
  id,
  label,
  secretKey,
  masked,
  configured,
  editable,
  value,
  copy,
  effect,
  error,
  onChange,
  onClear,
}: SecretReplacementFieldProps) {
  const replacing = value.trim().length > 0;
  const invalid = Boolean(error);
  return (
    <Field data-invalid={invalid || undefined} data-disabled={!editable || undefined}>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <FieldContent>
          <FieldLabelWithEffect htmlFor={id} label={label} effect={effect} />
          <FieldDescription>{copy.currentSecretMetadata(configured ? masked || copy.set : copy.notConfigured)}</FieldDescription>
        </FieldContent>
        <Badge variant={editable ? (replacing ? "destructive" : "secondary") : "outline"} className="w-fit">
          {editable ? (replacing ? copy.replaceOnSave : copy.preserve) : copy.preserveOnly}
        </Badge>
      </div>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Input
          id={id}
          type="password"
          value={value}
          disabled={!editable}
          aria-invalid={invalid || undefined}
          placeholder={editable ? copy.leaveBlankToPreserveCurrentSecret : copy.replacementDisabled}
          onChange={(event) => onChange(secretKey, event.target.value)}
        />
        <Button type="button" variant="outline" disabled={!value} onClick={() => onClear(secretKey)}>
          {copy.clear}
        </Button>
      </div>
      <FieldError>{error}</FieldError>
    </Field>
  );
}

function MetadataRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="max-w-[65%] break-all text-right font-medium">{value}</span>
    </div>
  );
}

interface StartupFileStatusCardProps {
  bootstrapConfig: BootstrapConfigResponse;
  copy: SettingsStartupCopy;
  currentApplySummary: {
    badge: string;
    variant: "destructive" | "secondary" | "outline";
  };
}

export function StartupFileStatusCard({ bootstrapConfig, copy, currentApplySummary }: StartupFileStatusCardProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-sm">
          <FileJson />
          {copy.fileStatusTitle}
        </CardTitle>
        <CardDescription>{copy.fileStatusDescription}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 md:grid-cols-2">
        <MetadataRow label={copy.configPath} value={bootstrapConfig.config_path} />
        <MetadataRow label={copy.schemaVersion} value={String(bootstrapConfig.schema_version)} />
        <MetadataRow label={copy.fileRevision} value={String(bootstrapConfig.file_revision)} />
        <MetadataRow label={copy.loadedRevision} value={String(bootstrapConfig.loaded_revision)} />
        <MetadataRow label={copy.updated} value={formatDateTime(bootstrapConfig.updated_at, copy.notRecorded)} />
        <div className="flex items-center justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2 text-sm">
          <span className="text-muted-foreground">{copy.state}</span>
          <span className="flex items-center gap-2">
            <Badge variant={bootstrapConfig.writable ? "secondary" : "destructive"}>{bootstrapConfig.writable ? copy.writable : copy.readOnly}</Badge>
            <Badge variant={bootstrapConfig.apply_result ? currentApplySummary.variant : "outline"}>{bootstrapConfig.apply_result ? currentApplySummary.badge : copy.loaded}</Badge>
          </span>
        </div>
      </CardContent>
    </Card>
  );
}

interface StartupServerSectionProps {
  copy: SettingsStartupCopy;
  controlsDisabled: boolean;
  corsOriginsText: string;
  fieldErrors: FieldErrors;
  fieldEffect: FieldEffectRenderer;
  sectionEffect: SectionEffectRenderer;
  setCorsOriginsText: (value: string) => void;
  setServerField: (field: "host" | "port", rawValue: string) => void;
  values: BootstrapConfigValues;
}

export function StartupServerSection({
  copy,
  controlsDisabled,
  corsOriginsText,
  fieldErrors,
  fieldEffect,
  sectionEffect,
  setCorsOriginsText,
  setServerField,
  values,
}: StartupServerSectionProps) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2 text-sm">
          <Server />
          {copy.serverAndBrowserAccessTitle}
          {sectionEffect(SERVER_FIELD_PATHS)}
        </CardTitle>
        <CardDescription>{copy.serverAndBrowserAccessDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldSet disabled={controlsDisabled}>
          <FieldLegend>{copy.server}</FieldLegend>
          <FieldGroup>
            <StartupInputField
              id="startup-server-host"
              label={copy.serverHost}
              effect={fieldEffect("server.host")}
              value={textValue(values.server.host)}
              error={fieldErrors["server.host"]}
              disabled={controlsDisabled}
              onChange={(value) => setServerField("host", value)}
            />
            <StartupInputField
              id="startup-server-port"
              label={copy.serverPort}
              effect={fieldEffect("server.port")}
              type="number"
              value={numberValue(values.server.port)}
              error={fieldErrors["server.port"]}
              disabled={controlsDisabled}
              onChange={(value) => setServerField("port", value)}
            />
            <StartupInputField
              id="startup-cors-origins"
              label={copy.corsAllowedOrigins}
              effect={fieldEffect("http.cors_allowed_origins")}
              value={corsOriginsText}
              error={fieldErrors["http.cors_allowed_origins"]}
              description={copy.corsOriginsDescription}
              disabled={controlsDisabled}
              onChange={setCorsOriginsText}
            />
          </FieldGroup>
        </FieldSet>
      </CardContent>
    </Card>
  );
}
