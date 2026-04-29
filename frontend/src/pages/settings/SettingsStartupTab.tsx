import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Database,
  FileJson,
  KeyRound,
  Loader2,
  Network,
  RefreshCw,
  Save,
  Server,
  ShieldAlert,
} from "lucide-react";
import { toast } from "sonner";
import type { Messages } from "@/i18n/messages/en";
import { useLocale } from "@/i18n/useLocale";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ApiError, api } from "@/lib/api";
import type {
  BootstrapConfigConfirmationToken,
  BootstrapConfigResponse,
  BootstrapConfigSecretKey,
  BootstrapConfigSecretUpdates,
  BootstrapConfigUpdateRequest,
  BootstrapConfigValues,
} from "@/lib/types";

const SECRET_KEYS: BootstrapConfigSecretKey[] = [
  "database.url",
  "runtime.secretEncryptionKey",
  "auth.jwtSigningKey",
  "stateTransfer.bundleEncryptionKey",
  "mail.smtp.password",
];

type ValidationStatus = "success" | "warning" | "error";
type SecretInputState = Record<BootstrapConfigSecretKey, string>;
type FieldErrors = Record<string, string>;
type SettingsStartupCopy = Messages["settingsStartup"];

interface ValidationRow {
  field: string;
  message: string;
  status: ValidationStatus;
}

interface DangerousConfirmation {
  token: BootstrapConfigConfirmationToken;
  label: string;
  active: boolean;
}

const emptySecretInputs = (): SecretInputState => ({
  "database.url": "",
  "runtime.secretEncryptionKey": "",
  "auth.jwtSigningKey": "",
  "stateTransfer.bundleEncryptionKey": "",
  "mail.smtp.password": "",
});

const cloneValues = (values: BootstrapConfigValues): BootstrapConfigValues => structuredClone(values);

function textValue(value: string | null): string {
  return value ?? "";
}

function numberValue(value: number | null): string {
  return value === null ? "" : String(value);
}

function parseNullableInteger(rawValue: string): number | null {
  const trimmed = rawValue.trim();
  if (!trimmed) {
    return null;
  }
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isNaN(parsed) ? null : parsed;
}

function parseOrigins(rawValue: string): string[] {
  return rawValue.split(",").map((origin) => origin.trim()).filter(Boolean);
}

function formatOrigins(origins: string[] | null): string {
  return (origins ?? []).join(", ");
}

function formatDateTime(value: string | null | undefined, fallback: string): string {
  if (!value) {
    return fallback;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function isAbsoluteUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return Boolean(url.protocol && url.host);
  } catch {
    return false;
  }
}

function isPositiveInteger(value: number | null): boolean {
  return Number.isInteger(value) && value !== null && value > 0;
}

function isNonNegativeInteger(value: number | null): boolean {
  return Number.isInteger(value) && value !== null && value >= 0;
}
function buildPreserveSecretUpdates(): BootstrapConfigSecretUpdates {
  return {
    "database.url": { action: "preserve" },
    "runtime.secretEncryptionKey": { action: "preserve" },
    "auth.jwtSigningKey": { action: "preserve" },
    "stateTransfer.bundleEncryptionKey": { action: "preserve" },
    "mail.smtp.password": { action: "preserve" },
  };
}

function getApiErrorDetail(error: unknown): unknown {
  if (error instanceof ApiError) {
    return error.detail;
  }
  return null;
}

function extractBackendRows(error: unknown, copy: SettingsStartupCopy): ValidationRow[] {
  const detail = getApiErrorDetail(error);
  if (!detail || typeof detail !== "object") {
    return [];
  }
  const bodyDetail = (detail as { detail?: unknown }).detail;
  if (typeof bodyDetail === "string") {
    return [{ field: "backend", message: bodyDetail, status: "error" }];
  }
  if (bodyDetail && typeof bodyDetail === "object") {
    const record = bodyDetail as Record<string, unknown>;
    const message = typeof record.message === "string" ? record.message : copy.backendValidationFailed;
    const field = typeof record.field === "string" ? record.field : "backend";
    const confirmations = Array.isArray(record.required_confirmations)
      ? copy.requiredConfirmations(record.required_confirmations.join(", "))
      : "";
    return [{ field, message: `${message}${confirmations}`, status: "error" }];
  }
  return [];
}

function getErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

function getValidationStatusLabel(status: ValidationStatus, copy: SettingsStartupCopy): string {
  if (status === "error") {
    return copy.validationStatusError;
  }
  if (status === "warning") {
    return copy.validationStatusWarning;
  }
  return copy.validationStatusSuccess;
}

function LoadingSkeleton() {
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

interface StartupFieldProps {
  id: string;
  label: string;
  value: string;
  description?: string;
  error?: string;
  type?: string;
  placeholder?: string;
  disabled?: boolean;
  onChange: (value: string) => void;
}

function StartupInputField({
  id,
  label,
  value,
  description,
  error,
  type = "text",
  placeholder,
  disabled = false,
  onChange,
}: StartupFieldProps) {
  const invalid = Boolean(error);
  return (
    <Field data-invalid={invalid || undefined} data-disabled={disabled || undefined}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
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
interface MetadataRowProps {
  label: string;
  value: string;
}

function MetadataRow({ label, value }: MetadataRowProps) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="max-w-[65%] break-all text-right font-medium">{value}</span>
    </div>
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
  error?: string;
  onChange: (secretKey: BootstrapConfigSecretKey, value: string) => void;
  onClear: (secretKey: BootstrapConfigSecretKey) => void;
}
function SecretReplacementField({
  id,
  label,
  secretKey,
  masked,
  configured,
  editable,
  value,
  copy,
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
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          <FieldDescription>
            {copy.currentSecretMetadata(configured ? masked || copy.set : copy.notConfigured)} {editable ? copy.enterNewValueWhenReplacing : copy.preserveOnlyInThisVersion}
          </FieldDescription>
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

export function SettingsStartupTab() {
  const { messages } = useLocale();
  const copy = messages.settingsStartup;
  const [bootstrapConfig, setBootstrapConfig] = useState<BootstrapConfigResponse | null>(null);
  const [values, setValues] = useState<BootstrapConfigValues | null>(null);
  const [corsOriginsText, setCorsOriginsText] = useState("");
  const [secretInputs, setSecretInputs] = useState<SecretInputState>(() => emptySecretInputs());
  const [confirmedTokens, setConfirmedTokens] = useState<BootstrapConfigConfirmationToken[]>([]);
  const [validationRows, setValidationRows] = useState<ValidationRow[]>([]);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [loading, setLoading] = useState(true);
  const [validating, setValidating] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [restartRequired, setRestartRequired] = useState(false);
  const [dangerDialogOpen, setDangerDialogOpen] = useState(false);

  const hydrateConfig = useCallback((response: BootstrapConfigResponse) => {
    setBootstrapConfig(response);
    setValues(cloneValues(response.values));
    setCorsOriginsText(formatOrigins(response.values.http.cors_allowed_origins));
    setConfirmedTokens([]);
    setValidationRows([]);
    setFieldErrors({});
    setRestartRequired(response.restart_required);
  }, []);

  const loadConfig = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      const response = await api.config.bootstrap.get();
      hydrateConfig(response);
    } catch (error) {
      setLoadError(getErrorMessage(error, copy.failedToLoad));
    } finally {
      setLoading(false);
    }
  }, [copy.failedToLoad, hydrateConfig]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const updateValues = useCallback((updater: (current: BootstrapConfigValues) => BootstrapConfigValues) => {
    setValues((current) => (current ? updater(current) : current));
    setValidationRows([]);
  }, []);

  const setServerField = useCallback((field: "host" | "port", rawValue: string) => {
    updateValues((current) => ({
      ...current,
      server: {
        ...current.server,
        [field]: field === "port" ? parseNullableInteger(rawValue) : rawValue.trim() || null,
      },
    }));
  }, [updateValues]);

  const setNumberField = useCallback((path: string, rawValue: string) => {
    const parsed = parseNullableInteger(rawValue);
    updateValues((current) => {
      const next = cloneValues(current);
      const segments = path.split(".");
      let target: Record<string, unknown> = next as unknown as Record<string, unknown>;
      for (let index = 0; index < segments.length - 1; index += 1) {
        target = target[segments[index]] as Record<string, unknown>;
      }
      target[segments[segments.length - 1]] = parsed;
      return next;
    });
  }, [updateValues]);

  const setStringField = useCallback((path: string, rawValue: string) => {
    updateValues((current) => {
      const next = cloneValues(current);
      const segments = path.split(".");
      let target: Record<string, unknown> = next as unknown as Record<string, unknown>;
      for (let index = 0; index < segments.length - 1; index += 1) {
        target = target[segments[index]] as Record<string, unknown>;
      }
      target[segments[segments.length - 1]] = rawValue.trim() || null;
      return next;
    });
  }, [updateValues]);

  const setBooleanField = useCallback((path: string, checked: boolean) => {
    updateValues((current) => {
      const next = cloneValues(current);
      const segments = path.split(".");
      let target: Record<string, unknown> = next as unknown as Record<string, unknown>;
      for (let index = 0; index < segments.length - 1; index += 1) {
        target = target[segments[index]] as Record<string, unknown>;
      }
      target[segments[segments.length - 1]] = checked;
      return next;
    });
  }, [updateValues]);

  const handleSecretInputChange = useCallback((secretKey: BootstrapConfigSecretKey, value: string) => {
    if (secretKey === "runtime.secretEncryptionKey") {
      return;
    }
    setSecretInputs((current) => ({ ...current, [secretKey]: value }));
    setValidationRows([]);
  }, []);

  const clearSecretInput = useCallback((secretKey: BootstrapConfigSecretKey) => {
    setSecretInputs((current) => ({ ...current, [secretKey]: "" }));
  }, []);

  const secretUpdates = useMemo<BootstrapConfigSecretUpdates>(() => {
    const updates = buildPreserveSecretUpdates();
    for (const secretKey of SECRET_KEYS) {
      const replacement = secretInputs[secretKey].trim();
      if (secretKey !== "runtime.secretEncryptionKey" && replacement) {
        updates[secretKey] = { action: "replace", value: replacement };
      }
    }
    return updates;
  }, [secretInputs]);

  const dangerousConfirmations = useMemo<DangerousConfirmation[]>(() => {
    if (!bootstrapConfig || !values) {
      return [];
    }
    return [
      {
        token: "server-host-change",
        label: copy.hostChangeLabel,
        active: bootstrapConfig.values.server.host !== values.server.host,
      },
      {
        token: "server-port-change",
        label: copy.portChangeLabel,
        active: bootstrapConfig.values.server.port !== values.server.port,
      },
      {
        token: "database-url-change",
        label: copy.databaseUrlChangeLabel,
        active: secretUpdates["database.url"].action === "replace",
      },
      {
        token: "auth-jwt-signing-key-change",
        label: copy.jwtSigningKeyChangeLabel,
        active: secretUpdates["auth.jwtSigningKey"].action === "replace",
      },
      {
        token: "state-transfer-bundle-encryption-key-change",
        label: copy.bundleEncryptionKeyChangeLabel,
        active: secretUpdates["stateTransfer.bundleEncryptionKey"].action === "replace",
      },
    ];
  }, [bootstrapConfig, copy, secretUpdates, values]);

  const activeDangerousConfirmations = useMemo(
    () => dangerousConfirmations.filter((confirmation) => confirmation.active),
    [dangerousConfirmations],
  );

  const missingDangerousConfirmations = useMemo(
    () => activeDangerousConfirmations.filter((confirmation) => !confirmedTokens.includes(confirmation.token)),
    [activeDangerousConfirmations, confirmedTokens],
  );

  const frontendValidation = useCallback((): { errors: FieldErrors; rows: ValidationRow[] } => {
    if (!values) {
      return { errors: {}, rows: [] };
    }
    const errors: FieldErrors = {};
    const rows: ValidationRow[] = [];
    const addError = (field: string, message: string) => {
      errors[field] = message;
      rows.push({ field, message, status: "error" });
    };
    if (!values.server.host?.trim()) {
      addError("server.host", copy.serverHostRequired);
    }
    if (!values.server.port || values.server.port < 1 || values.server.port > 65535) {
      addError("server.port", copy.serverPortRange);
    }
    const origins = parseOrigins(corsOriginsText);
    const uniqueOrigins = new Set(origins);
    if (origins.length === 0) {
      addError("http.cors_allowed_origins", copy.corsOriginsRequired);
    } else if (uniqueOrigins.size !== origins.length) {
      addError("http.cors_allowed_origins", copy.corsOriginsUnique);
    } else if (origins.some((origin) => !isAbsoluteUrl(origin))) {
      addError("http.cors_allowed_origins", copy.corsOriginsAbsolute);
    }
    return { errors, rows };
  }, [copy, corsOriginsText, values]);

  const validateNumericRelationships = useCallback((errors: FieldErrors, rows: ValidationRow[]) => {
    if (!values) {
      return;
    }
    const checkPositive = (field: string, value: number | null) => {
      if (!isPositiveInteger(value)) {
        errors[field] = copy.usePositiveInteger;
        rows.push({ field, message: copy.usePositiveInteger, status: "error" });
      }
    };
    const checkNonNegative = (field: string, value: number | null) => {
      if (!isNonNegativeInteger(value)) {
        errors[field] = copy.useZeroOrPositiveInteger;
        rows.push({ field, message: copy.useZeroOrPositiveInteger, status: "error" });
      }
    };
    checkPositive("database.runtime_pool.max_conns", values.database.runtime_pool.max_conns);
    checkNonNegative("database.runtime_pool.min_idle_conns", values.database.runtime_pool.min_idle_conns);
    checkPositive("database.management_pool.max_conns", values.database.management_pool.max_conns);
    checkNonNegative("database.management_pool.min_idle_conns", values.database.management_pool.min_idle_conns);
    checkPositive("database.management_admission.m2_max_concurrent", values.database.management_admission.m2_max_concurrent);
    checkPositive("database.management_admission.m3_max_concurrent", values.database.management_admission.m3_max_concurrent);
    checkPositive("runtime.transport.max_idle_conns", values.runtime.transport.max_idle_conns);
    checkPositive("runtime.transport.max_idle_conns_per_host", values.runtime.transport.max_idle_conns_per_host);
    checkNonNegative("runtime.transport.max_conns_per_host", values.runtime.transport.max_conns_per_host);
    checkPositive("auth.access_token_ttl_seconds", values.auth.access_token_ttl_seconds);
    checkPositive("auth.refresh_token_ttl_seconds", values.auth.refresh_token_ttl_seconds);
    checkPositive("auth.reset_code_ttl_seconds", values.auth.reset_code_ttl_seconds);
    if ((values.database.management_admission.m3_max_concurrent ?? 0) > (values.database.management_admission.m2_max_concurrent ?? 0)) {
      errors["database.management_admission.m3_max_concurrent"] = copy.m3ConcurrencyLimit;
      rows.push({ field: "database.management_admission", message: copy.m3ConcurrencyLimit, status: "error" });
    }
    if ((values.database.runtime_pool.min_idle_conns ?? 0) > (values.database.runtime_pool.max_conns ?? 0)) {
      errors["database.runtime_pool.min_idle_conns"] = copy.minIdleMustNotExceedMax;
      rows.push({ field: "database.runtime_pool", message: copy.minIdleMustNotExceedMax, status: "error" });
    }
    if ((values.database.management_pool.min_idle_conns ?? 0) > (values.database.management_pool.max_conns ?? 0)) {
      errors["database.management_pool.min_idle_conns"] = copy.minIdleMustNotExceedMax;
      rows.push({ field: "database.management_pool", message: copy.minIdleMustNotExceedMax, status: "error" });
    }
    const checkRequiredString = (field: string, value: string | null) => {
      if (!value?.trim()) {
        errors[field] = copy.useRequiredValue;
        rows.push({ field, message: copy.useRequiredValue, status: "error" });
      }
    };
    checkRequiredString("runtime.transport.idle_conn_timeout", values.runtime.transport.idle_conn_timeout);
    checkRequiredString("runtime.transport.request_timeout", values.runtime.transport.request_timeout);
    checkRequiredString("runtime.transport.response_header_timeout", values.runtime.transport.response_header_timeout);
    checkRequiredString("runtime.transport.tls_handshake_timeout", values.runtime.transport.tls_handshake_timeout);
    checkRequiredString("runtime.transport.expect_continue_timeout", values.runtime.transport.expect_continue_timeout);
    checkRequiredString("auth.access_cookie_name", values.auth.access_cookie_name);
    checkRequiredString("auth.refresh_cookie_name", values.auth.refresh_cookie_name);
  }, [copy, values]);

  const buildRequest = useCallback((confirmations: BootstrapConfigConfirmationToken[]): BootstrapConfigUpdateRequest | null => {
    if (!bootstrapConfig || !values) {
      return null;
    }
    const nextValues = cloneValues(values);
    nextValues.http.cors_allowed_origins = parseOrigins(corsOriginsText);
    return {
      expected_revision: bootstrapConfig.file_revision,
      expected_etag: bootstrapConfig.document_etag,
      values: nextValues,
      secret_updates: secretUpdates,
      confirmations,
    };
  }, [bootstrapConfig, corsOriginsText, secretUpdates, values]);

  const runFrontendValidation = useCallback((requireConfirmations: boolean) => {
    const { errors, rows } = frontendValidation();
    validateNumericRelationships(errors, rows);
    if (requireConfirmations && missingDangerousConfirmations.length > 0) {
      rows.push({
        field: "confirmations",
        message: copy.completeDangerousChecklist,
        status: "error",
      });
    }
    setFieldErrors(errors);
    setValidationRows(rows.length ? rows : [{ field: "frontend", message: copy.clientChecksPassed, status: "success" }]);
    return rows.length === 0;
  }, [copy.clientChecksPassed, copy.completeDangerousChecklist, frontendValidation, missingDangerousConfirmations.length, validateNumericRelationships]);

  const handleValidate = useCallback(async () => {
    if (!runFrontendValidation(false)) {
      toast.error(copy.fixClientErrorsBeforeBackendValidation);
      return;
    }
    const request = buildRequest(confirmedTokens);
    if (!request) {
      return;
    }
    setValidating(true);
    try {
      await api.config.bootstrap.validate(request);
      setValidationRows([{ field: "backend", message: copy.backendValidationPassed, status: "success" }]);
      toast.success(copy.bootstrapConfigValidated);
    } catch (error) {
      const rows = extractBackendRows(error, copy);
      setValidationRows(rows.length ? rows : [{ field: "backend", message: getErrorMessage(error, copy.validationUnavailable), status: "error" }]);
      toast.error(copy.failedToValidate);
    } finally {
      setValidating(false);
    }
  }, [buildRequest, confirmedTokens, copy, runFrontendValidation]);

  const performSave = useCallback(async () => {
    const request = buildRequest(confirmedTokens);
    if (!request) {
      return;
    }
    setSaving(true);
    try {
      await api.config.bootstrap.validate(request);
      const response = await api.config.bootstrap.update(request);
      hydrateConfig(response);
      setSecretInputs(emptySecretInputs());
      setRestartRequired(response.restart_required);
      setValidationRows([{ field: "save", message: response.restart_required ? copy.saveRestartRequiredMessage : copy.noEffectiveChangesWritten, status: "success" }]);
      toast.success(response.restart_required ? copy.savedRestartRequiredToast : copy.alreadyUpToDateToast);
    } catch (error) {
      setSecretInputs(emptySecretInputs());
      const rows = extractBackendRows(error, copy);
      setValidationRows(rows.length ? rows : [{ field: "save", message: getErrorMessage(error, copy.failedToSave), status: "error" }]);
      toast.error(copy.failedToSave);
    } finally {
      setSaving(false);
      setDangerDialogOpen(false);
    }
  }, [buildRequest, confirmedTokens, copy, hydrateConfig]);

  const handleSave = useCallback(() => {
    if (!runFrontendValidation(true)) {
      toast.error(copy.completeValidationBeforeSaving);
      return;
    }
    if (activeDangerousConfirmations.length > 0) {
      setDangerDialogOpen(true);
      return;
    }
    void performSave();
  }, [activeDangerousConfirmations.length, copy.completeValidationBeforeSaving, performSave, runFrontendValidation]);

  const dirtySummary = useMemo(() => {
    if (!bootstrapConfig || !values) {
      return [] as string[];
    }
    const items: string[] = [];
    if (JSON.stringify(bootstrapConfig.values) !== JSON.stringify({ ...values, http: { ...values.http, cors_allowed_origins: parseOrigins(corsOriginsText) } })) {
      items.push(copy.safeValuesChanged);
    }
    const replacements = SECRET_KEYS.filter((secretKey) => secretUpdates[secretKey].action === "replace");
    if (replacements.length > 0) {
      items.push(copy.secretReplacementCount(replacements.length));
    }
    return items;
  }, [bootstrapConfig, copy, corsOriginsText, secretUpdates, values]);

  const toggleConfirmation = useCallback((token: BootstrapConfigConfirmationToken, checked: boolean) => {
    setConfirmedTokens((current) => {
      if (checked) {
        return current.includes(token) ? current : [...current, token];
      }
      return current.filter((item) => item !== token);
    });
  }, []);

  if (loading) {
    return <LoadingSkeleton />;
  }

  if (loadError || !bootstrapConfig || !values) {
    return (
      <Alert variant="destructive">
        <AlertCircle />
        <AlertTitle>{copy.loadFailedTitle}</AlertTitle>
        <AlertDescription className="gap-3">
          <p>{loadError ?? copy.loadFailedDescription}</p>
          <Button type="button" variant="outline" size="sm" onClick={() => void loadConfig()}>
            <RefreshCw data-icon="inline-start" />
            {copy.retry}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  const controlsDisabled = saving || validating || !bootstrapConfig.writable;

  return (
    <div className="flex flex-col gap-6">
      <Alert>
        <ShieldAlert />
        <AlertTitle>{copy.startupBootstrapConfigTitle}</AlertTitle>
        <AlertDescription>
          {copy.startupBootstrapConfigDescription}
        </AlertDescription>
      </Alert>

      {restartRequired ? (
        <Alert>
          <RefreshCw />
          <AlertTitle>{copy.restartRequired}</AlertTitle>
          <AlertDescription>
            {copy.restartRequiredDescription}
          </AlertDescription>
        </Alert>
      ) : null}

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
              <Badge variant={restartRequired ? "destructive" : "outline"}>{restartRequired ? copy.restartRequired : copy.loaded}</Badge>
            </span>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm"><Server />{copy.serverAndBrowserAccessTitle}</CardTitle>
            <CardDescription>{copy.serverAndBrowserAccessDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.server}</FieldLegend>
              <FieldGroup>
                <StartupInputField id="startup-server-host" label={copy.serverHost} value={textValue(values.server.host)} error={fieldErrors["server.host"]} disabled={controlsDisabled} onChange={(value) => setServerField("host", value)} />
                <StartupInputField id="startup-server-port" label={copy.serverPort} type="number" value={numberValue(values.server.port)} error={fieldErrors["server.port"]} disabled={controlsDisabled} onChange={(value) => setServerField("port", value)} />
                <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                  <FieldContent><FieldLabel htmlFor="startup-docs-enabled">{copy.docsEnabled}</FieldLabel><FieldDescription>{copy.docsEnabledDescription}</FieldDescription></FieldContent>
                  <Switch id="startup-docs-enabled" checked={Boolean(values.server.docs_enabled)} disabled={controlsDisabled} onCheckedChange={(checked) => setBooleanField("server.docs_enabled", checked)} />
                </Field>
                <StartupInputField id="startup-cors-origins" label={copy.corsAllowedOrigins} value={corsOriginsText} error={fieldErrors["http.cors_allowed_origins"]} description={copy.corsOriginsDescription} disabled={controlsDisabled} onChange={setCorsOriginsText} />
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm"><Database />{copy.databaseAndCapacityTitle}</CardTitle>
            <CardDescription>{copy.databaseAndCapacityDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.database}</FieldLegend>
              <FieldGroup>
                <SecretReplacementField id="startup-database-url" label={copy.databaseUrl} secretKey="database.url" masked={bootstrapConfig.secrets["database.url"].masked} configured={bootstrapConfig.secrets["database.url"].configured} editable={bootstrapConfig.secrets["database.url"].editable && !controlsDisabled} value={secretInputs["database.url"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
                <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-runtime-max-conns" label={copy.runtimeMaxConns} type="number" value={numberValue(values.database.runtime_pool.max_conns)} error={fieldErrors["database.runtime_pool.max_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.runtime_pool.max_conns", value)} />
                  <StartupInputField id="startup-runtime-min-idle" label={copy.runtimeMinIdle} type="number" value={numberValue(values.database.runtime_pool.min_idle_conns)} error={fieldErrors["database.runtime_pool.min_idle_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.runtime_pool.min_idle_conns", value)} />
                  <StartupInputField id="startup-management-max-conns" label={copy.managementMaxConns} type="number" value={numberValue(values.database.management_pool.max_conns)} error={fieldErrors["database.management_pool.max_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.management_pool.max_conns", value)} />
                  <StartupInputField id="startup-management-min-idle" label={copy.managementMinIdle} type="number" value={numberValue(values.database.management_pool.min_idle_conns)} error={fieldErrors["database.management_pool.min_idle_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.management_pool.min_idle_conns", value)} />
                  <StartupInputField id="startup-m2-concurrent" label={copy.m2MaxConcurrent} type="number" value={numberValue(values.database.management_admission.m2_max_concurrent)} error={fieldErrors["database.management_admission.m2_max_concurrent"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.management_admission.m2_max_concurrent", value)} />
                  <StartupInputField id="startup-m3-concurrent" label={copy.m3MaxConcurrent} type="number" value={numberValue(values.database.management_admission.m3_max_concurrent)} error={fieldErrors["database.management_admission.m3_max_concurrent"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.management_admission.m3_max_concurrent", value)} />
                </div>
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm"><Network />{copy.transportTitle}</CardTitle>
            <CardDescription>{copy.transportDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.transport}</FieldLegend>
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="startup-buffering-mode">{copy.bufferingMode}</FieldLabel>
                  <Select value={values.runtime.buffering_mode ?? "buffered"} disabled={controlsDisabled} onValueChange={(value) => setStringField("runtime.buffering_mode", value)}>
                    <SelectTrigger id="startup-buffering-mode"><SelectValue placeholder={copy.selectMode} /></SelectTrigger>
                    <SelectContent><SelectGroup><SelectItem value="buffered">{copy.buffered}</SelectItem><SelectItem value="streaming">{copy.streaming}</SelectItem></SelectGroup></SelectContent>
                  </Select>
                </Field>
                <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-max-idle-conns" label={copy.maxIdleConns} type="number" value={numberValue(values.runtime.transport.max_idle_conns)} error={fieldErrors["runtime.transport.max_idle_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("runtime.transport.max_idle_conns", value)} />
                  <StartupInputField id="startup-max-idle-per-host" label={copy.maxIdlePerHost} type="number" value={numberValue(values.runtime.transport.max_idle_conns_per_host)} error={fieldErrors["runtime.transport.max_idle_conns_per_host"]} disabled={controlsDisabled} onChange={(value) => setNumberField("runtime.transport.max_idle_conns_per_host", value)} />
                  <StartupInputField id="startup-max-conns-per-host" label={copy.maxConnsPerHost} type="number" value={numberValue(values.runtime.transport.max_conns_per_host)} error={fieldErrors["runtime.transport.max_conns_per_host"]} disabled={controlsDisabled} onChange={(value) => setNumberField("runtime.transport.max_conns_per_host", value)} />
                  <StartupInputField id="startup-idle-timeout" label={copy.idleConnTimeout} value={textValue(values.runtime.transport.idle_conn_timeout)} error={fieldErrors["runtime.transport.idle_conn_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.idle_conn_timeout", value)} />
                  <StartupInputField id="startup-request-timeout" label={copy.requestTimeout} value={textValue(values.runtime.transport.request_timeout)} error={fieldErrors["runtime.transport.request_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.request_timeout", value)} />
                  <StartupInputField id="startup-response-header-timeout" label={copy.responseHeaderTimeout} value={textValue(values.runtime.transport.response_header_timeout)} error={fieldErrors["runtime.transport.response_header_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.response_header_timeout", value)} />
                  <StartupInputField id="startup-tls-timeout" label={copy.tlsHandshakeTimeout} value={textValue(values.runtime.transport.tls_handshake_timeout)} error={fieldErrors["runtime.transport.tls_handshake_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.tls_handshake_timeout", value)} />
                  <StartupInputField id="startup-expect-timeout" label={copy.expectContinueTimeout} value={textValue(values.runtime.transport.expect_continue_timeout)} error={fieldErrors["runtime.transport.expect_continue_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.expect_continue_timeout", value)} />
                </div>
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-sm"><KeyRound />{copy.authAndCookiesTitle}</CardTitle>
            <CardDescription>{copy.authAndCookiesDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.auth}</FieldLegend>
              <FieldGroup>
                <SecretReplacementField id="startup-jwt-key" label={copy.jwtSigningKey} secretKey="auth.jwtSigningKey" masked={bootstrapConfig.secrets["auth.jwtSigningKey"].masked} configured={bootstrapConfig.secrets["auth.jwtSigningKey"].configured} editable={bootstrapConfig.secrets["auth.jwtSigningKey"].editable && !controlsDisabled} value={secretInputs["auth.jwtSigningKey"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
                <SecretReplacementField id="startup-smtp-password" label="SMTP password" secretKey="mail.smtp.password" masked={bootstrapConfig.secrets["mail.smtp.password"].masked} configured={bootstrapConfig.secrets["mail.smtp.password"].configured} editable={bootstrapConfig.secrets["mail.smtp.password"].editable && !controlsDisabled} value={secretInputs["mail.smtp.password"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
                <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-access-ttl" label={copy.accessTokenTtlSeconds} type="number" value={numberValue(values.auth.access_token_ttl_seconds)} error={fieldErrors["auth.access_token_ttl_seconds"]} disabled={controlsDisabled} onChange={(value) => setNumberField("auth.access_token_ttl_seconds", value)} />
                  <StartupInputField id="startup-refresh-ttl" label={copy.refreshTokenTtlSeconds} type="number" value={numberValue(values.auth.refresh_token_ttl_seconds)} error={fieldErrors["auth.refresh_token_ttl_seconds"]} disabled={controlsDisabled} onChange={(value) => setNumberField("auth.refresh_token_ttl_seconds", value)} />
                  <StartupInputField id="startup-reset-ttl" label={copy.resetCodeTtlSeconds} type="number" value={numberValue(values.auth.reset_code_ttl_seconds)} error={fieldErrors["auth.reset_code_ttl_seconds"]} disabled={controlsDisabled} onChange={(value) => setNumberField("auth.reset_code_ttl_seconds", value)} />
                  <StartupInputField id="startup-access-cookie" label={copy.accessCookieName} value={textValue(values.auth.access_cookie_name)} error={fieldErrors["auth.access_cookie_name"]} disabled={controlsDisabled} onChange={(value) => setStringField("auth.access_cookie_name", value)} />
                  <StartupInputField id="startup-refresh-cookie" label={copy.refreshCookieName} value={textValue(values.auth.refresh_cookie_name)} error={fieldErrors["auth.refresh_cookie_name"]} disabled={controlsDisabled} onChange={(value) => setStringField("auth.refresh_cookie_name", value)} />
                </div>
                <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                  <FieldContent><FieldLabel htmlFor="startup-cookie-secure">{copy.secureCookies}</FieldLabel><FieldDescription>{copy.secureCookiesDescription}</FieldDescription></FieldContent>
                  <Switch id="startup-cookie-secure" checked={Boolean(values.auth.cookie_secure)} disabled={controlsDisabled} onCheckedChange={(checked) => setBooleanField("auth.cookie_secure", checked)} />
                </Field>
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm"><ShieldAlert />{copy.stateTransferTitle}</CardTitle>
          <CardDescription>{copy.stateTransferDescription}</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldSet disabled={controlsDisabled}>
            <FieldLegend>{copy.secrets}</FieldLegend>
            <FieldGroup>
              <SecretReplacementField id="startup-bundle-key" label={copy.bundleEncryptionKey} secretKey="stateTransfer.bundleEncryptionKey" masked={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].masked} configured={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].configured} editable={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].editable && !controlsDisabled} value={secretInputs["stateTransfer.bundleEncryptionKey"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
              <SecretReplacementField id="startup-runtime-secret-key" label={copy.runtimeSecretEncryptionKey} secretKey="runtime.secretEncryptionKey" masked={bootstrapConfig.secrets["runtime.secretEncryptionKey"].masked} configured={bootstrapConfig.secrets["runtime.secretEncryptionKey"].configured} editable={false} value="" copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
            </FieldGroup>
          </FieldSet>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-sm"><CheckCircle2 />{copy.reviewAndSaveTitle}</CardTitle>
          <CardDescription>{copy.reviewAndSaveDescription}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2">
            {dirtySummary.length ? dirtySummary.map((item) => <Badge key={item} variant="secondary">{item}</Badge>) : <Badge variant="outline">{copy.noLocalChangesDetected}</Badge>}
            {activeDangerousConfirmations.length ? <Badge variant="destructive">{copy.dangerousChangesStaged}</Badge> : null}
          </div>
          <FieldSet>
            <FieldLegend>{copy.dangerousChecklistTitle}</FieldLegend>
            <FieldDescription>{copy.dangerousChecklistDescription}</FieldDescription>
            <FieldGroup data-slot="checkbox-group">
              {dangerousConfirmations.map((confirmation) => (
                <Field key={confirmation.token} orientation="horizontal" data-disabled={!confirmation.active || controlsDisabled || undefined}>
                  <Checkbox id={confirmation.token} checked={confirmedTokens.includes(confirmation.token)} disabled={!confirmation.active || controlsDisabled} onCheckedChange={(checked) => toggleConfirmation(confirmation.token, checked === true)} />
                  <FieldContent><FieldLabel htmlFor={confirmation.token}>{confirmation.label}</FieldLabel><FieldDescription>{confirmation.active ? copy.confirmationRequiredBeforeSave : copy.noChangeCurrentlyStaged}</FieldDescription></FieldContent>
                </Field>
              ))}
            </FieldGroup>
          </FieldSet>
          <Separator />
          <Table>
            <TableHeader>
              <TableRow><TableHead>{copy.status}</TableHead><TableHead>{copy.field}</TableHead><TableHead>{copy.message}</TableHead></TableRow>
            </TableHeader>
            <TableBody>
              {validationRows.map((row, index) => (
                <TableRow key={`${row.field}-${index}`}>
                  <TableCell><Badge variant={row.status === "error" ? "destructive" : row.status === "warning" ? "secondary" : "outline"}>{getValidationStatusLabel(row.status, copy)}</Badge></TableCell>
                  <TableCell>{row.field}</TableCell>
                  <TableCell className="whitespace-normal">{row.message}</TableCell>
                </TableRow>
              ))}
              {!validationRows.length ? <TableRow><TableCell colSpan={3} className="text-muted-foreground">{copy.noValidationRunYet}</TableCell></TableRow> : null}
            </TableBody>
          </Table>
        </CardContent>
        <CardFooter className="justify-end gap-2">
          <Button type="button" variant="outline" disabled={controlsDisabled} onClick={() => void handleValidate()}>
            {validating ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <CheckCircle2 data-icon="inline-start" />}
            {copy.validate}
          </Button>
          <Button type="button" disabled={controlsDisabled || saving || !bootstrapConfig.writable} onClick={handleSave}>
            {saving ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />}
            {copy.saveStartupConfig}
          </Button>
        </CardFooter>
      </Card>

      <AlertDialog open={dangerDialogOpen} onOpenChange={(open) => {
        setDangerDialogOpen(open);
        if (!open) {
          setSecretInputs(emptySecretInputs());
        }
      }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.dangerDialogTitle}</AlertDialogTitle>
            <AlertDialogDescription>
              {copy.dangerDialogDescription}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="rounded-md border bg-muted/20 p-3 text-sm">
            <ul className="flex list-disc flex-col gap-1 pl-5">
              {activeDangerousConfirmations.map((confirmation) => <li key={confirmation.token}>{confirmation.label}</li>)}
            </ul>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={saving}>{copy.saveDangerousChangesCancel}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={saving} onClick={(event) => {
              event.preventDefault();
              void performSave();
            }}>
              {copy.saveAndRequireRestart}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
