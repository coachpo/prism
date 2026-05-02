import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  AlertCircle,
  CheckCircle2,
  Database,
  FileJson,
  KeyRound,
  Loader2,
  Mail,
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
  BootstrapConfigApplyMode,
  BootstrapConfigApplyResult,
  BootstrapConfigConfirmationToken,
  BootstrapConfigFieldCapability,
  BootstrapConfigFieldChange,
  BootstrapConfigPlannedChanges,
  BootstrapConfigResponse,
  BootstrapConfigSecretKey,
  BootstrapConfigSecretUpdates,
  BootstrapConfigUpdateRequest,
  BootstrapConfigDatabasePoolValues,
  BootstrapConfigDatabasePoolsValues,
  BootstrapConfigMailSMTPValues,
  BootstrapConfigMailValues,
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
type PostgresPoolLane = Exclude<keyof BootstrapConfigDatabasePoolsValues, "total_max_conns">;

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

const DEFAULT_POSTGRES_POOLS: BootstrapConfigDatabasePoolsValues = {
  total_max_conns: 42,
  management: { max_conns: 6, min_idle_conns: 0 },
  runtime_execution: { max_conns: 14, min_idle_conns: 1 },
  runtime_telemetry: { max_conns: 7, min_idle_conns: 0 },
  runtime_feedback: { max_conns: 3, min_idle_conns: 0 },
  realtime: { max_conns: 4, min_idle_conns: 0 },
  cache_refresh: { max_conns: 4, min_idle_conns: 0 },
  background_jobs: { max_conns: 4, min_idle_conns: 0 },
};

const POSTGRES_POOL_LANES: PostgresPoolLane[] = [
  "runtime_execution",
  "runtime_telemetry",
  "runtime_feedback",
  "management",
  "realtime",
  "cache_refresh",
  "background_jobs",
];

const POSTGRES_POOL_LABEL_KEYS = {
  runtime_execution: "postgresLaneRuntimeExecution",
  runtime_telemetry: "postgresLaneRuntimeTelemetry",
  runtime_feedback: "postgresLaneRuntimeFeedback",
  management: "postgresLaneManagement",
  realtime: "postgresLaneRealtime",
  cache_refresh: "postgresLaneCacheRefresh",
  background_jobs: "postgresLaneBackgroundJobs",
} satisfies Record<PostgresPoolLane, keyof SettingsStartupCopy>;


type FieldLabelResolver = (copy: SettingsStartupCopy) => string;

const FIELD_LABELS = {
  "server.host": (copy) => copy.serverHost,
  "server.port": (copy) => copy.serverPort,
  "server.docs_enabled": (copy) => copy.docsEnabled,
  "http.cors_allowed_origins": (copy) => copy.corsAllowedOrigins,
  "database.url": (copy) => copy.databaseUrl,
  "database.pools.total_max_conns": (copy) => copy.postgresTotalMaxConns,
  "database.pools.management.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneManagement),
  "database.pools.management.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneManagement),
  "database.pools.runtime_execution.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneRuntimeExecution),
  "database.pools.runtime_execution.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneRuntimeExecution),
  "database.pools.runtime_telemetry.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneRuntimeTelemetry),
  "database.pools.runtime_telemetry.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneRuntimeTelemetry),
  "database.pools.runtime_feedback.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneRuntimeFeedback),
  "database.pools.runtime_feedback.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneRuntimeFeedback),
  "database.pools.realtime.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneRealtime),
  "database.pools.realtime.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneRealtime),
  "database.pools.cache_refresh.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneCacheRefresh),
  "database.pools.cache_refresh.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneCacheRefresh),
  "database.pools.background_jobs.max_conns": (copy) => copy.postgresLaneMaxConns(copy.postgresLaneBackgroundJobs),
  "database.pools.background_jobs.min_idle_conns": (copy) => copy.postgresLaneMinIdle(copy.postgresLaneBackgroundJobs),
  "database.management_admission.m2_max_concurrent": (copy) => copy.m2MaxConcurrent,
  "database.management_admission.m3_max_concurrent": (copy) => copy.m3MaxConcurrent,
  "runtime.buffering_mode": (copy) => copy.bufferingMode,
  "runtime.transport.max_idle_conns": (copy) => copy.maxIdleConns,
  "runtime.transport.max_idle_conns_per_host": (copy) => copy.maxIdlePerHost,
  "runtime.transport.max_conns_per_host": (copy) => copy.maxConnsPerHost,
  "runtime.transport.idle_conn_timeout": (copy) => copy.idleConnTimeout,
  "runtime.transport.request_timeout": (copy) => copy.requestTimeout,
  "runtime.transport.response_header_timeout": (copy) => copy.responseHeaderTimeout,
  "runtime.transport.tls_handshake_timeout": (copy) => copy.tlsHandshakeTimeout,
  "runtime.transport.expect_continue_timeout": (copy) => copy.expectContinueTimeout,
  "auth.jwtSigningKey": (copy) => copy.jwtSigningKey,
  "auth.access_token_ttl_seconds": (copy) => copy.accessTokenTtlSeconds,
  "auth.refresh_token_ttl_seconds": (copy) => copy.refreshTokenTtlSeconds,
  "auth.reset_code_ttl_seconds": (copy) => copy.resetCodeTtlSeconds,
  "auth.access_cookie_name": (copy) => copy.accessCookieName,
  "auth.refresh_cookie_name": (copy) => copy.refreshCookieName,
  "auth.cookie_secure": (copy) => copy.secureCookies,
  "mail.enabled": (copy) => copy.mailEnabled,
  "mail.from": (copy) => copy.mailFrom,
  "mail.reply_to": (copy) => copy.mailReplyTo,
  "mail.smtp.host": (copy) => copy.smtpHost,
  "mail.smtp.port": (copy) => copy.smtpPort,
  "mail.smtp.mode": (copy) => copy.smtpMode,
  "mail.smtp.ehlo_hostname": (copy) => copy.smtpEhloHostname,
  "mail.smtp.auth": (copy) => copy.smtpAuth,
  "mail.smtp.username": (copy) => copy.smtpUsername,
  "mail.smtp.password_file": (copy) => copy.smtpPasswordFile,
  "mail.smtp.password": (copy) => copy.smtpPassword,
  "mail.smtp.timeout": (copy) => copy.smtpTimeout,
  "mail.smtp.tls_server_name": (copy) => copy.smtpTlsServerName,
  "stateTransfer.bundleEncryptionKey": (copy) => copy.bundleEncryptionKey,
  "runtime.secretEncryptionKey": (copy) => copy.runtimeSecretEncryptionKey,
} satisfies Record<string, FieldLabelResolver>;

const SERVER_FIELD_PATHS = ["server.host", "server.port", "server.docs_enabled", "http.cors_allowed_origins"];
const DATABASE_FIELD_PATHS = [
  "database.url",
  "database.pools.total_max_conns",
  "database.pools.management.max_conns",
  "database.pools.management.min_idle_conns",
  "database.pools.runtime_execution.max_conns",
  "database.pools.runtime_execution.min_idle_conns",
  "database.pools.runtime_telemetry.max_conns",
  "database.pools.runtime_telemetry.min_idle_conns",
  "database.pools.runtime_feedback.max_conns",
  "database.pools.runtime_feedback.min_idle_conns",
  "database.pools.realtime.max_conns",
  "database.pools.realtime.min_idle_conns",
  "database.pools.cache_refresh.max_conns",
  "database.pools.cache_refresh.min_idle_conns",
  "database.pools.background_jobs.max_conns",
  "database.pools.background_jobs.min_idle_conns",
  "database.management_admission.m2_max_concurrent",
  "database.management_admission.m3_max_concurrent",
];
const TRANSPORT_FIELD_PATHS = [
  "runtime.buffering_mode",
  "runtime.transport.max_idle_conns",
  "runtime.transport.max_idle_conns_per_host",
  "runtime.transport.max_conns_per_host",
  "runtime.transport.idle_conn_timeout",
  "runtime.transport.request_timeout",
  "runtime.transport.response_header_timeout",
  "runtime.transport.tls_handshake_timeout",
  "runtime.transport.expect_continue_timeout",
];
const AUTH_FIELD_PATHS = [
  "auth.jwtSigningKey",
  "auth.access_token_ttl_seconds",
  "auth.refresh_token_ttl_seconds",
  "auth.reset_code_ttl_seconds",
  "auth.access_cookie_name",
  "auth.refresh_cookie_name",
  "auth.cookie_secure",
];
const MAIL_FIELD_PATHS = [
  "mail.enabled",
  "mail.from",
  "mail.reply_to",
  "mail.smtp.host",
  "mail.smtp.port",
  "mail.smtp.mode",
  "mail.smtp.ehlo_hostname",
  "mail.smtp.auth",
  "mail.smtp.username",
  "mail.smtp.password_file",
  "mail.smtp.password",
  "mail.smtp.timeout",
  "mail.smtp.tls_server_name",
];
const SECRET_FIELD_PATHS = new Set<string>(SECRET_KEYS);
const STATE_TRANSFER_FIELD_PATHS = ["stateTransfer.bundleEncryptionKey", "runtime.secretEncryptionKey"];

const cloneValues = (values: BootstrapConfigValues): BootstrapConfigValues => structuredClone(values);

function normalizePoolValues(
  pool: BootstrapConfigDatabasePoolValues | null | undefined,
  defaults: BootstrapConfigDatabasePoolValues,
): BootstrapConfigDatabasePoolValues {
  if (!pool) {
    return { ...defaults };
  }
  return {
    max_conns: pool.max_conns === undefined ? defaults.max_conns : pool.max_conns,
    min_idle_conns: pool.min_idle_conns === undefined ? defaults.min_idle_conns : pool.min_idle_conns,
  };
}

function normalizePostgresPools(values: BootstrapConfigValues): BootstrapConfigDatabasePoolsValues {
  const pools = values.database.pools;
  return {
    total_max_conns: pools?.total_max_conns === undefined ? DEFAULT_POSTGRES_POOLS.total_max_conns : pools.total_max_conns,
    management: normalizePoolValues(pools?.management ?? values.database.management_pool, DEFAULT_POSTGRES_POOLS.management),
    runtime_execution: normalizePoolValues(pools?.runtime_execution ?? values.database.runtime_pool, DEFAULT_POSTGRES_POOLS.runtime_execution),
    runtime_telemetry: normalizePoolValues(pools?.runtime_telemetry, DEFAULT_POSTGRES_POOLS.runtime_telemetry),
    runtime_feedback: normalizePoolValues(pools?.runtime_feedback, DEFAULT_POSTGRES_POOLS.runtime_feedback),
    realtime: normalizePoolValues(pools?.realtime, DEFAULT_POSTGRES_POOLS.realtime),
    cache_refresh: normalizePoolValues(pools?.cache_refresh, DEFAULT_POSTGRES_POOLS.cache_refresh),
    background_jobs: normalizePoolValues(pools?.background_jobs, DEFAULT_POSTGRES_POOLS.background_jobs),
  };
}

function defaultSMTPValues(): BootstrapConfigMailSMTPValues {
  return {
    host: null,
    port: null,
    mode: "starttls_required",
    ehlo_hostname: null,
    auth: "none",
    username: null,
    password_file: null,
    timeout: "15s",
    tls_server_name: null,
  };
}

function defaultDisabledMailValues(): BootstrapConfigMailValues {
  return {
    enabled: false,
    from: null,
    reply_to: null,
    smtp: null,
  };
}

function normalizeMailValues(mail: BootstrapConfigMailValues | null | undefined): BootstrapConfigMailValues {
  if (!mail || !mail.enabled) {
    return defaultDisabledMailValues();
  }
  return {
    ...mail,
    enabled: true,
    smtp: mail.smtp ?? defaultSMTPValues(),
  };
}

function normalizeBootstrapValues(values: BootstrapConfigValues): BootstrapConfigValues {
  const nextValues = cloneValues(values);
  nextValues.database = {
    pools: normalizePostgresPools(nextValues),
    management_admission: nextValues.database.management_admission,
  };
  nextValues.mail = normalizeMailValues(nextValues.mail);
  return nextValues;
}

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

function isValidSMTPMode(value: string | null): boolean {
  return value === "starttls_required" || value === "implicit_tls" || value === "plaintext_local_only";
}

function isValidSMTPAuth(value: string | null): boolean {
  return value === "none" || value === "plain";
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

function getPostgresPoolLaneLabel(copy: SettingsStartupCopy, lane: PostgresPoolLane): string {
  return copy[POSTGRES_POOL_LABEL_KEYS[lane]] as string;
}


function formatFieldPath(path: string): string {
  return path.split(".").map((part) => part.replaceAll("_", " ")).join(" / ");
}

function getFieldLabel(copy: SettingsStartupCopy, path: string): string {
  const resolver = (FIELD_LABELS as Record<string, FieldLabelResolver>)[path];
  return resolver?.(copy) ?? formatFieldPath(path);
}

function getCapabilityLabel(copy: SettingsStartupCopy, mode: BootstrapConfigApplyMode): string {
  return mode === "hot_apply" ? copy.appliesImmediately : copy.restartRequired;
}

function getCapabilityVariant(mode: BootstrapConfigApplyMode): "secondary" | "outline" {
  return mode === "hot_apply" ? "secondary" : "outline";
}

function FieldEffectBadge({ capability, copy }: { capability?: BootstrapConfigFieldCapability; copy: SettingsStartupCopy }) {
  if (!capability) {
    return null;
  }
  return <Badge variant={getCapabilityVariant(capability.mode)}>{getCapabilityLabel(copy, capability.mode)}</Badge>;
}

function SectionEffectBadge({ capabilities, copy, fields }: { capabilities: Record<string, BootstrapConfigFieldCapability>; copy: SettingsStartupCopy; fields: string[] }) {
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

function FieldLabelWithEffect({ effect, htmlFor, label }: { effect?: ReactNode; htmlFor?: string; label: string }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <FieldLabel htmlFor={htmlFor}>{label}</FieldLabel>
      {effect}
    </div>
  );
}

function getValueAtPath(values: BootstrapConfigValues, path: string): unknown {
  return path.split(".").reduce<unknown>((current, segment) => {
    if (!current || typeof current !== "object") {
      return undefined;
    }
    return (current as Record<string, unknown>)[segment];
  }, values);
}

function valuesEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function getChangedCapabilityFields(
  bootstrapConfig: BootstrapConfigResponse,
  values: BootstrapConfigValues,
  corsOriginsText: string,
  secretUpdates: BootstrapConfigSecretUpdates,
): string[] {
  const nextValues = normalizeBootstrapValues(values);
  nextValues.http.cors_allowed_origins = parseOrigins(corsOriginsText);
  return Object.keys(bootstrapConfig.apply_capabilities).filter((field) => {
    if (SECRET_FIELD_PATHS.has(field)) {
      return secretUpdates[field as BootstrapConfigSecretKey]?.action === "replace";
    }
    return !valuesEqual(getValueAtPath(bootstrapConfig.values, field), getValueAtPath(nextValues, field));
  });
}

function getDangerousConfirmationLabel(copy: SettingsStartupCopy, token: string, field: string): string {
  if (token === "server-host-change") {
    return copy.hostChangeLabel;
  }
  if (token === "server-port-change") {
    return copy.portChangeLabel;
  }
  if (token === "database-url-change") {
    return copy.databaseUrlChangeLabel;
  }
  if (token === "auth-jwt-signing-key-change") {
    return copy.jwtSigningKeyChangeLabel;
  }
  if (token === "state-transfer-bundle-encryption-key-change") {
    return copy.bundleEncryptionKeyChangeLabel;
  }
  return copy.fieldRequiresConfirmation(getFieldLabel(copy, field));
}

function buildPlannedRows(plannedChanges: BootstrapConfigPlannedChanges | undefined, copy: SettingsStartupCopy): ValidationRow[] {
  const changes = plannedChanges?.changed_fields ?? [];
  if (changes.length === 0) {
    return [{ field: "backend", message: copy.backendValidationPassed, status: "success" }];
  }
  return changes.map((change: BootstrapConfigFieldChange) => ({
    field: getFieldLabel(copy, change.field),
    message: change.mode === "hot_apply" ? copy.plannedHotApplyMessage : copy.plannedRestartRequiredMessage,
    status: change.mode === "hot_apply" ? "success" : "warning",
  }));
}

function summarizeApplyResult(applyResult: BootstrapConfigApplyResult | undefined, copy: SettingsStartupCopy) {
  const failedFields = new Set(applyResult?.failed_hot_apply_fields ?? []);
  const pendingFields = (applyResult?.pending_hot_apply_fields ?? []).filter((field) => !failedFields.has(field));
  const appliedCount = applyResult?.applied_now_fields.length ?? 0;
  const restartCount = applyResult?.restart_required_fields.length ?? 0;
  const pendingCount = pendingFields.length;
  const failedCount = failedFields.size;
  const changedCount = appliedCount + restartCount + pendingCount + failedCount;
  if (failedCount > 0) {
    return {
      badge: copy.hotApplyFailed,
      message: changedCount > failedCount ? copy.saveFailedPartialMessage : copy.saveFailedApplyMessage,
      toast: changedCount > failedCount ? copy.savedPartialApplyToast : copy.failedApplyToast,
      status: "error" as ValidationStatus,
      variant: "destructive" as const,
    };
  }
  if (changedCount === 0) {
    return {
      badge: copy.loaded,
      message: copy.noEffectiveChangesWritten,
      toast: copy.alreadyUpToDateToast,
      status: "success" as ValidationStatus,
      variant: "outline" as const,
    };
  }
  if (restartCount > 0 && (appliedCount > 0 || pendingCount > 0)) {
    return {
      badge: copy.mixedEffects,
      message: copy.saveMixedApplyMessage,
      toast: copy.savedMixedApplyToast,
      status: "warning" as ValidationStatus,
      variant: "secondary" as const,
    };
  }
  if (restartCount > 0) {
    return {
      badge: copy.restartRequired,
      message: copy.saveRestartRequiredMessage,
      toast: copy.savedRestartRequiredToast,
      status: "warning" as ValidationStatus,
      variant: "destructive" as const,
    };
  }
  if (pendingCount > 0) {
    return {
      badge: copy.pendingHotApply,
      message: copy.savePendingHotApplyMessage,
      toast: copy.savedPendingHotApplyToast,
      status: "warning" as ValidationStatus,
      variant: "secondary" as const,
    };
  }
  return {
    badge: copy.appliesImmediately,
    message: copy.saveHotAppliedMessage,
    toast: copy.savedHotAppliedToast,
    status: "success" as ValidationStatus,
    variant: "secondary" as const,
  };
}

function buildApplyResultRows(applyResult: BootstrapConfigApplyResult | undefined, copy: SettingsStartupCopy): ValidationRow[] {
  if (!applyResult) {
    return [{ field: "save", message: copy.noEffectiveChangesWritten, status: "success" }];
  }
  const rows: ValidationRow[] = [];
  for (const field of applyResult.applied_now_fields) {
    rows.push({ field: getFieldLabel(copy, field), message: copy.appliedNowMessage, status: "success" });
  }
  const failedFields = new Set(applyResult.failed_hot_apply_fields);
  for (const field of applyResult.pending_hot_apply_fields) {
    if (!failedFields.has(field)) {
      rows.push({ field: getFieldLabel(copy, field), message: copy.pendingHotApplyMessage, status: "warning" });
    }
  }
  for (const field of applyResult.failed_hot_apply_fields) {
    rows.push({ field: getFieldLabel(copy, field), message: copy.failedHotApplyMessage, status: "error" });
  }
  for (const field of applyResult.restart_required_fields) {
    rows.push({ field: getFieldLabel(copy, field), message: copy.restartRequiredSaveMessage, status: "warning" });
  }
  if (rows.length === 0) {
    return [{ field: "save", message: copy.noEffectiveChangesWritten, status: "success" }];
  }
  if (applyResult.unchanged_fields.length > 0) {
    rows.push({ field: "unchanged", message: copy.unchangedFieldsMessage(applyResult.unchanged_fields.length), status: "success" });
  }
  return rows;
}

function extractBootstrapResponse(error: unknown): BootstrapConfigResponse | null {
  const detail = getApiErrorDetail(error);
  if (!detail || typeof detail !== "object") {
    return null;
  }
  const candidate = detail as Partial<BootstrapConfigResponse>;
  if (candidate.values && candidate.secrets && candidate.apply_capabilities) {
    return candidate as BootstrapConfigResponse;
  }
  return null;
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
  effect?: ReactNode;
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
  effect?: ReactNode;
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
  const [dangerDialogOpen, setDangerDialogOpen] = useState(false);

  const hydrateConfig = useCallback((response: BootstrapConfigResponse) => {
    const normalizedResponse = {
      ...response,
      values: normalizeBootstrapValues(response.values),
    };
    setBootstrapConfig(normalizedResponse);
    setValues(cloneValues(normalizedResponse.values));
    setCorsOriginsText(formatOrigins(normalizedResponse.values.http.cors_allowed_origins));
    setConfirmedTokens([]);
    setValidationRows([]);
    setFieldErrors({});
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

  const setMailEnabled = useCallback((checked: boolean) => {
    if (!checked) {
      setSecretInputs((current) => ({ ...current, "mail.smtp.password": "" }));
    }
    updateValues((current) => {
      if (!checked) {
        return { ...current, mail: defaultDisabledMailValues() };
      }
      const currentMail = normalizeMailValues(current.mail);
      return {
        ...current,
        mail: {
          ...currentMail,
          enabled: true,
          smtp: currentMail.smtp ?? defaultSMTPValues(),
        },
      };
    });
  }, [updateValues]);

  const setMailStringField = useCallback((field: "from" | "reply_to", rawValue: string) => {
    updateValues((current) => {
      const currentMail = normalizeMailValues(current.mail);
      return {
        ...current,
        mail: {
          ...currentMail,
          [field]: rawValue.trim() || null,
        },
      };
    });
  }, [updateValues]);

  const setSMTPStringField = useCallback((field: keyof BootstrapConfigMailSMTPValues, rawValue: string) => {
    updateValues((current) => {
      const currentMail = normalizeMailValues(current.mail);
      const currentSMTP = currentMail.smtp ?? defaultSMTPValues();
      return {
        ...current,
        mail: {
          ...currentMail,
          enabled: true,
          smtp: {
            ...currentSMTP,
            [field]: rawValue.trim() || null,
          },
        },
      };
    });
  }, [updateValues]);

  const setSMTPNumberField = useCallback((field: keyof BootstrapConfigMailSMTPValues, rawValue: string) => {
    const parsed = parseNullableInteger(rawValue);
    updateValues((current) => {
      const currentMail = normalizeMailValues(current.mail);
      const currentSMTP = currentMail.smtp ?? defaultSMTPValues();
      return {
        ...current,
        mail: {
          ...currentMail,
          enabled: true,
          smtp: {
            ...currentSMTP,
            [field]: parsed,
          },
        },
      };
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
    const mailEnabled = values ? normalizeMailValues(values.mail).enabled : false;
    for (const secretKey of SECRET_KEYS) {
      if (secretKey === "mail.smtp.password" && !mailEnabled) {
        continue;
      }
      const replacement = secretInputs[secretKey].trim();
      const masked = bootstrapConfig?.secrets[secretKey].masked ?? "";
      if (secretKey !== "runtime.secretEncryptionKey" && replacement && replacement !== masked) {
        updates[secretKey] = { action: "replace", value: replacement };
      }
    }
    return updates;
  }, [bootstrapConfig, secretInputs, values]);

  const changedCapabilityFields = useMemo(() => {
    if (!bootstrapConfig || !values) {
      return [] as string[];
    }
    return getChangedCapabilityFields(bootstrapConfig, values, corsOriginsText, secretUpdates);
  }, [bootstrapConfig, corsOriginsText, secretUpdates, values]);

  const changedCapabilityFieldSet = useMemo(() => new Set(changedCapabilityFields), [changedCapabilityFields]);

  const dangerousConfirmations = useMemo<DangerousConfirmation[]>(() => {
    if (!bootstrapConfig) {
      return [];
    }
    return Object.entries(bootstrapConfig.apply_capabilities)
      .filter(([, capability]) => capability.mode === "restart_required" && Boolean(capability.confirmation_token))
      .map(([field, capability]) => ({
        token: capability.confirmation_token as BootstrapConfigConfirmationToken,
        label: getDangerousConfirmationLabel(copy, capability.confirmation_token ?? "", field),
        active: changedCapabilityFieldSet.has(field),
      }));
  }, [bootstrapConfig, changedCapabilityFieldSet, copy]);

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
    const mailValues = normalizeMailValues(values.mail);
    if (mailValues.enabled) {
      const smtpValues = mailValues.smtp ?? defaultSMTPValues();
      if (!mailValues.from?.trim()) {
        addError("mail.from", copy.mailFromRequired);
      }
      if (!smtpValues.host?.trim()) {
        addError("mail.smtp.host", copy.smtpHostRequired);
      }
      if (!smtpValues.port || smtpValues.port < 1 || smtpValues.port > 65535) {
        addError("mail.smtp.port", copy.smtpPortRange);
      }
      if (!isValidSMTPMode(smtpValues.mode)) {
        addError("mail.smtp.mode", copy.smtpModeRequired);
      }
      if (!smtpValues.timeout?.trim()) {
        addError("mail.smtp.timeout", copy.smtpTimeoutRequired);
      }
      if (!isValidSMTPAuth(smtpValues.auth)) {
        addError("mail.smtp.auth", copy.smtpAuthRequired);
      }
      const smtpPasswordUpdate = secretUpdates["mail.smtp.password"];
      const stagedInlinePassword = smtpPasswordUpdate.action === "replace";
      const passwordFileSet = Boolean(smtpValues.password_file?.trim());
      const preservedInlinePassword = Boolean(
        bootstrapConfig?.secrets["mail.smtp.password"].configured &&
        smtpPasswordUpdate.action === "preserve" &&
        !passwordFileSet,
      );
      if (stagedInlinePassword && passwordFileSet) {
        addError("mail.smtp.password_file", copy.smtpPasswordSourceConflict);
        addError("mail.smtp.password", copy.smtpPasswordSourceConflict);
      }
      if (smtpValues.auth === "plain") {
        if (!smtpValues.username?.trim()) {
          addError("mail.smtp.username", copy.smtpUsernameRequired);
        }
        const passwordSourceCount = [stagedInlinePassword, passwordFileSet, preservedInlinePassword].filter(Boolean).length;
        if (passwordSourceCount === 0) {
          addError("mail.smtp.password", copy.smtpPasswordSourceRequired);
        } else if (passwordSourceCount > 1) {
          addError("mail.smtp.password", copy.smtpPasswordSourceConflict);
        }
      }
    }
    return { errors, rows };
  }, [bootstrapConfig, copy, corsOriginsText, secretUpdates, values]);

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
    checkPositive("database.pools.total_max_conns", values.database.pools.total_max_conns);
    for (const lane of POSTGRES_POOL_LANES) {
      const pool = values.database.pools[lane];
      checkPositive(`database.pools.${lane}.max_conns`, pool.max_conns);
      checkNonNegative(`database.pools.${lane}.min_idle_conns`, pool.min_idle_conns);
      if ((pool.min_idle_conns ?? 0) > (pool.max_conns ?? 0)) {
        errors[`database.pools.${lane}.min_idle_conns`] = copy.minIdleMustNotExceedMax;
        rows.push({ field: `database.pools.${lane}`, message: copy.minIdleMustNotExceedMax, status: "error" });
      }
    }
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
    const nextValues = normalizeBootstrapValues(values);
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
      const response = await api.config.bootstrap.validate(request);
      setValidationRows(buildPlannedRows(response.planned_changes, copy));
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
      const summary = summarizeApplyResult(response.apply_result, copy);
      hydrateConfig(response);
      setSecretInputs(emptySecretInputs());
      setValidationRows(buildApplyResultRows(response.apply_result, copy));
      if (summary.status === "error") {
        toast.error(summary.toast);
      } else {
        toast.success(summary.toast);
      }
    } catch (error) {
      setSecretInputs(emptySecretInputs());
      const response = extractBootstrapResponse(error);
      if (response) {
        const summary = summarizeApplyResult(response.apply_result, copy);
        hydrateConfig(response);
        setValidationRows(buildApplyResultRows(response.apply_result, copy));
        toast.error(summary.toast);
      } else {
        const rows = extractBackendRows(error, copy);
        setValidationRows(rows.length ? rows : [{ field: "save", message: getErrorMessage(error, copy.failedToSave), status: "error" }]);
        toast.error(copy.failedToSave);
      }
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
    if (!bootstrapConfig) {
      return [] as string[];
    }
    const items: string[] = [];
    const hotCount = changedCapabilityFields.filter((field) => bootstrapConfig.apply_capabilities[field]?.mode === "hot_apply").length;
    const restartCount = changedCapabilityFields.filter((field) => bootstrapConfig.apply_capabilities[field]?.mode === "restart_required").length;
    if (hotCount > 0 && restartCount > 0) {
      items.push(copy.mixedChangesStaged(hotCount, restartCount));
    } else if (hotCount > 0) {
      items.push(copy.hotApplyChangesStaged(hotCount));
    } else if (restartCount > 0) {
      items.push(copy.restartChangesStaged(restartCount));
    }
    const replacements = SECRET_KEYS.filter((secretKey) => secretUpdates[secretKey].action === "replace");
    if (replacements.length > 0) {
      items.push(copy.secretReplacementCount(replacements.length));
    }
    return items;
  }, [bootstrapConfig, changedCapabilityFields, copy, secretUpdates]);

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
  const mailValues = normalizeMailValues(values.mail);
  const smtpValues = mailValues.smtp ?? defaultSMTPValues();
  const mailEnabled = Boolean(mailValues.enabled);
  const smtpControlsDisabled = controlsDisabled || !mailEnabled;
  const currentApplySummary = summarizeApplyResult(bootstrapConfig.apply_result, copy);
  const fieldEffect = (field: string) => <FieldEffectBadge capability={bootstrapConfig.apply_capabilities[field]} copy={copy} />;
  const sectionEffect = (fields: string[]) => <SectionEffectBadge capabilities={bootstrapConfig.apply_capabilities} copy={copy} fields={fields} />;

  return (
    <div className="flex flex-col gap-6">
      <Alert>
        <ShieldAlert />
        <AlertTitle>{copy.startupBootstrapConfigTitle}</AlertTitle>
        <AlertDescription>
          {copy.startupBootstrapConfigDescription}
        </AlertDescription>
      </Alert>

      {bootstrapConfig.apply_result ? (
        <Alert variant={currentApplySummary.status === "error" ? "destructive" : "default"}>
          <RefreshCw />
          <AlertTitle>{currentApplySummary.badge}</AlertTitle>
          <AlertDescription>
            {currentApplySummary.message}
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
              <Badge variant={bootstrapConfig.apply_result ? currentApplySummary.variant : "outline"}>{bootstrapConfig.apply_result ? currentApplySummary.badge : copy.loaded}</Badge>
            </span>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-6 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-sm"><Server />{copy.serverAndBrowserAccessTitle}{sectionEffect(SERVER_FIELD_PATHS)}</CardTitle>
            <CardDescription>{copy.serverAndBrowserAccessDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.server}</FieldLegend>
              <FieldGroup>
                <StartupInputField id="startup-server-host" label={copy.serverHost} effect={fieldEffect("server.host")} value={textValue(values.server.host)} error={fieldErrors["server.host"]} disabled={controlsDisabled} onChange={(value) => setServerField("host", value)} />
                <StartupInputField id="startup-server-port" label={copy.serverPort} effect={fieldEffect("server.port")} type="number" value={numberValue(values.server.port)} error={fieldErrors["server.port"]} disabled={controlsDisabled} onChange={(value) => setServerField("port", value)} />
                <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                  <FieldContent><FieldLabelWithEffect htmlFor="startup-docs-enabled" label={copy.docsEnabled} effect={fieldEffect("server.docs_enabled")} /><FieldDescription>{copy.docsEnabledDescription}</FieldDescription></FieldContent>
                  <Switch id="startup-docs-enabled" checked={Boolean(values.server.docs_enabled)} disabled={controlsDisabled} onCheckedChange={(checked) => setBooleanField("server.docs_enabled", checked)} />
                </Field>
                <StartupInputField id="startup-cors-origins" label={copy.corsAllowedOrigins} effect={fieldEffect("http.cors_allowed_origins")} value={corsOriginsText} error={fieldErrors["http.cors_allowed_origins"]} description={copy.corsOriginsDescription} disabled={controlsDisabled} onChange={setCorsOriginsText} />
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-sm"><Database />{copy.databaseAndCapacityTitle}{sectionEffect(DATABASE_FIELD_PATHS)}</CardTitle>
            <CardDescription>{copy.databaseAndCapacityDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.database}</FieldLegend>
              <FieldGroup>
                <SecretReplacementField id="startup-database-url" label={copy.databaseUrl} effect={fieldEffect("database.url")} secretKey="database.url" masked={bootstrapConfig.secrets["database.url"].masked} configured={bootstrapConfig.secrets["database.url"].configured} editable={bootstrapConfig.secrets["database.url"].editable && !controlsDisabled} value={secretInputs["database.url"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
                <StartupInputField id="startup-postgres-total-max-conns" label={copy.postgresTotalMaxConns} effect={fieldEffect("database.pools.total_max_conns")} type="number" value={numberValue(values.database.pools.total_max_conns)} error={fieldErrors["database.pools.total_max_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.pools.total_max_conns", value)} />
                <div className="grid gap-4 md:grid-cols-2">
                  {POSTGRES_POOL_LANES.map((lane) => {
                    const label = getPostgresPoolLaneLabel(copy, lane);
                    return (
                      <div key={lane} className="contents">
                        <StartupInputField id={`startup-${lane}-max-conns`} label={copy.postgresLaneMaxConns(label)} effect={fieldEffect(`database.pools.${lane}.max_conns`)} type="number" value={numberValue(values.database.pools[lane].max_conns)} error={fieldErrors[`database.pools.${lane}.max_conns`]} disabled={controlsDisabled} onChange={(value) => setNumberField(`database.pools.${lane}.max_conns`, value)} />
                        <StartupInputField id={`startup-${lane}-min-idle`} label={copy.postgresLaneMinIdle(label)} effect={fieldEffect(`database.pools.${lane}.min_idle_conns`)} type="number" value={numberValue(values.database.pools[lane].min_idle_conns)} error={fieldErrors[`database.pools.${lane}.min_idle_conns`]} disabled={controlsDisabled} onChange={(value) => setNumberField(`database.pools.${lane}.min_idle_conns`, value)} />
                      </div>
                    );
                  })}
                  <StartupInputField id="startup-m2-concurrent" label={copy.m2MaxConcurrent} effect={fieldEffect("database.management_admission.m2_max_concurrent")} type="number" value={numberValue(values.database.management_admission.m2_max_concurrent)} error={fieldErrors["database.management_admission.m2_max_concurrent"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.management_admission.m2_max_concurrent", value)} />
                  <StartupInputField id="startup-m3-concurrent" label={copy.m3MaxConcurrent} effect={fieldEffect("database.management_admission.m3_max_concurrent")} type="number" value={numberValue(values.database.management_admission.m3_max_concurrent)} error={fieldErrors["database.management_admission.m3_max_concurrent"]} disabled={controlsDisabled} onChange={(value) => setNumberField("database.management_admission.m3_max_concurrent", value)} />
                </div>
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-sm"><Network />{copy.transportTitle}{sectionEffect(TRANSPORT_FIELD_PATHS)}</CardTitle>
            <CardDescription>{copy.transportDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.transport}</FieldLegend>
              <FieldGroup>
                <Field>
                  <FieldLabelWithEffect htmlFor="startup-buffering-mode" label={copy.bufferingMode} effect={fieldEffect("runtime.buffering_mode")} />
                  <Select value={values.runtime.buffering_mode ?? "buffered"} disabled={controlsDisabled} onValueChange={(value) => setStringField("runtime.buffering_mode", value)}>
                    <SelectTrigger id="startup-buffering-mode"><SelectValue placeholder={copy.selectMode} /></SelectTrigger>
                    <SelectContent><SelectGroup><SelectItem value="buffered">{copy.buffered}</SelectItem><SelectItem value="streaming">{copy.streaming}</SelectItem></SelectGroup></SelectContent>
                  </Select>
                </Field>
                <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-max-idle-conns" label={copy.maxIdleConns} effect={fieldEffect("runtime.transport.max_idle_conns")} type="number" value={numberValue(values.runtime.transport.max_idle_conns)} error={fieldErrors["runtime.transport.max_idle_conns"]} disabled={controlsDisabled} onChange={(value) => setNumberField("runtime.transport.max_idle_conns", value)} />
                  <StartupInputField id="startup-max-idle-per-host" label={copy.maxIdlePerHost} effect={fieldEffect("runtime.transport.max_idle_conns_per_host")} type="number" value={numberValue(values.runtime.transport.max_idle_conns_per_host)} error={fieldErrors["runtime.transport.max_idle_conns_per_host"]} disabled={controlsDisabled} onChange={(value) => setNumberField("runtime.transport.max_idle_conns_per_host", value)} />
                  <StartupInputField id="startup-max-conns-per-host" label={copy.maxConnsPerHost} effect={fieldEffect("runtime.transport.max_conns_per_host")} type="number" value={numberValue(values.runtime.transport.max_conns_per_host)} error={fieldErrors["runtime.transport.max_conns_per_host"]} disabled={controlsDisabled} onChange={(value) => setNumberField("runtime.transport.max_conns_per_host", value)} />
                  <StartupInputField id="startup-idle-timeout" label={copy.idleConnTimeout} effect={fieldEffect("runtime.transport.idle_conn_timeout")} value={textValue(values.runtime.transport.idle_conn_timeout)} error={fieldErrors["runtime.transport.idle_conn_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.idle_conn_timeout", value)} />
                  <StartupInputField id="startup-request-timeout" label={copy.requestTimeout} effect={fieldEffect("runtime.transport.request_timeout")} value={textValue(values.runtime.transport.request_timeout)} error={fieldErrors["runtime.transport.request_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.request_timeout", value)} />
                  <StartupInputField id="startup-response-header-timeout" label={copy.responseHeaderTimeout} effect={fieldEffect("runtime.transport.response_header_timeout")} value={textValue(values.runtime.transport.response_header_timeout)} error={fieldErrors["runtime.transport.response_header_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.response_header_timeout", value)} />
                  <StartupInputField id="startup-tls-timeout" label={copy.tlsHandshakeTimeout} effect={fieldEffect("runtime.transport.tls_handshake_timeout")} value={textValue(values.runtime.transport.tls_handshake_timeout)} error={fieldErrors["runtime.transport.tls_handshake_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.tls_handshake_timeout", value)} />
                  <StartupInputField id="startup-expect-timeout" label={copy.expectContinueTimeout} effect={fieldEffect("runtime.transport.expect_continue_timeout")} value={textValue(values.runtime.transport.expect_continue_timeout)} error={fieldErrors["runtime.transport.expect_continue_timeout"]} disabled={controlsDisabled} onChange={(value) => setStringField("runtime.transport.expect_continue_timeout", value)} />
                </div>
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-sm"><KeyRound />{copy.authAndCookiesTitle}{sectionEffect(AUTH_FIELD_PATHS)}</CardTitle>
            <CardDescription>{copy.authAndCookiesDescription}</CardDescription>
          </CardHeader>
          <CardContent>
              <FieldSet disabled={controlsDisabled}>
                <FieldLegend>{copy.auth}</FieldLegend>
                <FieldGroup>
                  <SecretReplacementField id="startup-jwt-key" label={copy.jwtSigningKey} effect={fieldEffect("auth.jwtSigningKey")} secretKey="auth.jwtSigningKey" masked={bootstrapConfig.secrets["auth.jwtSigningKey"].masked} configured={bootstrapConfig.secrets["auth.jwtSigningKey"].configured} editable={bootstrapConfig.secrets["auth.jwtSigningKey"].editable && !controlsDisabled} value={secretInputs["auth.jwtSigningKey"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
                  <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-access-ttl" label={copy.accessTokenTtlSeconds} effect={fieldEffect("auth.access_token_ttl_seconds")} type="number" value={numberValue(values.auth.access_token_ttl_seconds)} error={fieldErrors["auth.access_token_ttl_seconds"]} disabled={controlsDisabled} onChange={(value) => setNumberField("auth.access_token_ttl_seconds", value)} />
                  <StartupInputField id="startup-refresh-ttl" label={copy.refreshTokenTtlSeconds} effect={fieldEffect("auth.refresh_token_ttl_seconds")} type="number" value={numberValue(values.auth.refresh_token_ttl_seconds)} error={fieldErrors["auth.refresh_token_ttl_seconds"]} disabled={controlsDisabled} onChange={(value) => setNumberField("auth.refresh_token_ttl_seconds", value)} />
                  <StartupInputField id="startup-reset-ttl" label={copy.resetCodeTtlSeconds} effect={fieldEffect("auth.reset_code_ttl_seconds")} type="number" value={numberValue(values.auth.reset_code_ttl_seconds)} error={fieldErrors["auth.reset_code_ttl_seconds"]} disabled={controlsDisabled} onChange={(value) => setNumberField("auth.reset_code_ttl_seconds", value)} />
                  <StartupInputField id="startup-access-cookie" label={copy.accessCookieName} effect={fieldEffect("auth.access_cookie_name")} value={textValue(values.auth.access_cookie_name)} error={fieldErrors["auth.access_cookie_name"]} disabled={controlsDisabled} onChange={(value) => setStringField("auth.access_cookie_name", value)} />
                  <StartupInputField id="startup-refresh-cookie" label={copy.refreshCookieName} effect={fieldEffect("auth.refresh_cookie_name")} value={textValue(values.auth.refresh_cookie_name)} error={fieldErrors["auth.refresh_cookie_name"]} disabled={controlsDisabled} onChange={(value) => setStringField("auth.refresh_cookie_name", value)} />
                </div>
                <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                  <FieldContent><FieldLabelWithEffect htmlFor="startup-cookie-secure" label={copy.secureCookies} effect={fieldEffect("auth.cookie_secure")} /><FieldDescription>{copy.secureCookiesDescription}</FieldDescription></FieldContent>
                  <Switch id="startup-cookie-secure" checked={Boolean(values.auth.cookie_secure)} disabled={controlsDisabled} onCheckedChange={(checked) => setBooleanField("auth.cookie_secure", checked)} />
                </Field>
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className="flex flex-wrap items-center gap-2 text-sm"><Mail />{copy.mailAndSmtpTitle}{sectionEffect(MAIL_FIELD_PATHS)}</CardTitle>
            <CardDescription>{copy.mailAndSmtpDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <FieldSet disabled={controlsDisabled}>
              <FieldLegend>{copy.mail}</FieldLegend>
              <FieldGroup>
                <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                  <FieldContent><FieldLabelWithEffect htmlFor="startup-mail-enabled" label={copy.mailEnabled} effect={fieldEffect("mail.enabled")} /><FieldDescription>{copy.mailEnabledDescription}</FieldDescription></FieldContent>
                  <Switch id="startup-mail-enabled" checked={mailEnabled} disabled={controlsDisabled} onCheckedChange={setMailEnabled} />
                </Field>
                <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-mail-from" label={copy.mailFrom} effect={fieldEffect("mail.from")} value={textValue(mailValues.from)} placeholder={copy.mailFromPlaceholder} error={fieldErrors["mail.from"]} disabled={smtpControlsDisabled} onChange={(value) => setMailStringField("from", value)} />
                  <StartupInputField id="startup-mail-reply-to" label={copy.mailReplyTo} effect={fieldEffect("mail.reply_to")} value={textValue(mailValues.reply_to)} placeholder={copy.mailReplyToPlaceholder} disabled={smtpControlsDisabled} onChange={(value) => setMailStringField("reply_to", value)} />
                </div>
              </FieldGroup>
            </FieldSet>
            <Separator className="my-6" />
            <FieldSet disabled={smtpControlsDisabled}>
              <FieldLegend>{copy.smtp}</FieldLegend>
              <FieldDescription>{mailEnabled ? copy.smtpDescription : copy.smtpDisabledDescription}</FieldDescription>
              <FieldGroup>
                <div className="grid gap-4 md:grid-cols-2">
                  <StartupInputField id="startup-smtp-host" label={copy.smtpHost} effect={fieldEffect("mail.smtp.host")} value={textValue(smtpValues.host)} placeholder={copy.smtpHostPlaceholder} error={fieldErrors["mail.smtp.host"]} disabled={smtpControlsDisabled} onChange={(value) => setSMTPStringField("host", value)} />
                  <StartupInputField id="startup-smtp-port" label={copy.smtpPort} effect={fieldEffect("mail.smtp.port")} type="number" value={numberValue(smtpValues.port)} error={fieldErrors["mail.smtp.port"]} disabled={smtpControlsDisabled} onChange={(value) => setSMTPNumberField("port", value)} />
                  <Field data-invalid={Boolean(fieldErrors["mail.smtp.mode"]) || undefined} data-disabled={smtpControlsDisabled || undefined}>
                    <FieldLabelWithEffect htmlFor="startup-smtp-mode" label={copy.smtpMode} effect={fieldEffect("mail.smtp.mode")} />
                    <Select value={smtpValues.mode ?? ""} disabled={smtpControlsDisabled} onValueChange={(value) => setSMTPStringField("mode", value)}>
                      <SelectTrigger id="startup-smtp-mode" aria-invalid={Boolean(fieldErrors["mail.smtp.mode"]) || undefined}><SelectValue placeholder={copy.selectMode} /></SelectTrigger>
                      <SelectContent><SelectGroup><SelectItem value="starttls_required">{copy.smtpModeStarttlsRequired}</SelectItem><SelectItem value="implicit_tls">{copy.smtpModeImplicitTls}</SelectItem><SelectItem value="plaintext_local_only">{copy.smtpModePlaintextLocalOnly}</SelectItem></SelectGroup></SelectContent>
                    </Select>
                    <FieldError>{fieldErrors["mail.smtp.mode"]}</FieldError>
                  </Field>
                  <StartupInputField id="startup-smtp-ehlo" label={copy.smtpEhloHostname} effect={fieldEffect("mail.smtp.ehlo_hostname")} value={textValue(smtpValues.ehlo_hostname)} placeholder={copy.smtpEhloHostnamePlaceholder} disabled={smtpControlsDisabled} onChange={(value) => setSMTPStringField("ehlo_hostname", value)} />
                  <Field data-invalid={Boolean(fieldErrors["mail.smtp.auth"]) || undefined} data-disabled={smtpControlsDisabled || undefined}>
                    <FieldLabelWithEffect htmlFor="startup-smtp-auth" label={copy.smtpAuth} effect={fieldEffect("mail.smtp.auth")} />
                    <Select value={smtpValues.auth ?? ""} disabled={smtpControlsDisabled} onValueChange={(value) => setSMTPStringField("auth", value)}>
                      <SelectTrigger id="startup-smtp-auth" aria-invalid={Boolean(fieldErrors["mail.smtp.auth"]) || undefined}><SelectValue placeholder={copy.smtpAuthPlaceholder} /></SelectTrigger>
                      <SelectContent><SelectGroup><SelectItem value="none">{copy.smtpAuthNone}</SelectItem><SelectItem value="plain">{copy.smtpAuthPlain}</SelectItem></SelectGroup></SelectContent>
                    </Select>
                    <FieldError>{fieldErrors["mail.smtp.auth"]}</FieldError>
                  </Field>
                  <StartupInputField id="startup-smtp-username" label={copy.smtpUsername} effect={fieldEffect("mail.smtp.username")} value={textValue(smtpValues.username)} placeholder={copy.smtpUsernamePlaceholder} error={fieldErrors["mail.smtp.username"]} disabled={smtpControlsDisabled || smtpValues.auth !== "plain"} onChange={(value) => setSMTPStringField("username", value)} />
                  <StartupInputField id="startup-smtp-password-file" label={copy.smtpPasswordFile} effect={fieldEffect("mail.smtp.password_file")} value={textValue(smtpValues.password_file)} placeholder={copy.smtpPasswordFilePlaceholder} description={copy.smtpPasswordFileDescription} error={fieldErrors["mail.smtp.password_file"]} disabled={smtpControlsDisabled} onChange={(value) => setSMTPStringField("password_file", value)} />
                  <StartupInputField id="startup-smtp-timeout" label={copy.smtpTimeout} effect={fieldEffect("mail.smtp.timeout")} value={textValue(smtpValues.timeout)} placeholder={copy.smtpTimeoutPlaceholder} error={fieldErrors["mail.smtp.timeout"]} disabled={smtpControlsDisabled} onChange={(value) => setSMTPStringField("timeout", value)} />
                  <StartupInputField id="startup-smtp-tls-server-name" label={copy.smtpTlsServerName} effect={fieldEffect("mail.smtp.tls_server_name")} value={textValue(smtpValues.tls_server_name)} placeholder={copy.smtpTlsServerNamePlaceholder} disabled={smtpControlsDisabled} onChange={(value) => setSMTPStringField("tls_server_name", value)} />
                </div>
                <SecretReplacementField id="startup-smtp-password" label={copy.smtpPassword} effect={fieldEffect("mail.smtp.password")} secretKey="mail.smtp.password" masked={bootstrapConfig.secrets["mail.smtp.password"].masked} configured={bootstrapConfig.secrets["mail.smtp.password"].configured} editable={mailEnabled && bootstrapConfig.secrets["mail.smtp.password"].editable && !controlsDisabled} value={secretInputs["mail.smtp.password"]} copy={copy} error={fieldErrors["mail.smtp.password"]} onChange={handleSecretInputChange} onClear={clearSecretInput} />
              </FieldGroup>
            </FieldSet>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2 text-sm"><ShieldAlert />{copy.stateTransferTitle}{sectionEffect(STATE_TRANSFER_FIELD_PATHS)}</CardTitle>
          <CardDescription>{copy.stateTransferDescription}</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldSet disabled={controlsDisabled}>
            <FieldLegend>{copy.secrets}</FieldLegend>
            <FieldGroup>
              <SecretReplacementField id="startup-bundle-key" label={copy.bundleEncryptionKey} effect={fieldEffect("stateTransfer.bundleEncryptionKey")} secretKey="stateTransfer.bundleEncryptionKey" masked={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].masked} configured={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].configured} editable={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].editable && !controlsDisabled} value={secretInputs["stateTransfer.bundleEncryptionKey"]} copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
              <SecretReplacementField id="startup-runtime-secret-key" label={copy.runtimeSecretEncryptionKey} effect={fieldEffect("runtime.secretEncryptionKey")} secretKey="runtime.secretEncryptionKey" masked={bootstrapConfig.secrets["runtime.secretEncryptionKey"].masked} configured={bootstrapConfig.secrets["runtime.secretEncryptionKey"].configured} editable={false} value="" copy={copy} onChange={handleSecretInputChange} onClear={clearSecretInput} />
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
