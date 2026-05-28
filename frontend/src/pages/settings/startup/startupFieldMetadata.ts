import { ApiError } from "@/lib/api";
import type { Messages } from "@/i18n/messages/en";
import type {
  BootstrapConfigApplyMode,
  BootstrapConfigApplyResult,
  BootstrapConfigFieldChange,
  BootstrapConfigPlannedChanges,
  BootstrapConfigResponse,
  BootstrapConfigSecretKey,
  BootstrapConfigSecretUpdates,
  BootstrapConfigDatabasePoolValues,
  BootstrapConfigDatabasePoolsValues,
  BootstrapConfigMailSMTPValues,
  BootstrapConfigMailValues,
  BootstrapConfigValues,
} from "@/lib/types";

export const SECRET_KEYS: BootstrapConfigSecretKey[] = [
  "database.url",
  "runtime.secretEncryptionKey",
  "auth.jwtSigningKey",
  "stateTransfer.bundleEncryptionKey",
  "mail.smtp.password",
];

export type ValidationStatus = "success" | "warning" | "error";
export type SecretInputState = Record<BootstrapConfigSecretKey, string>;
export type FieldErrors = Record<string, string>;
export type SettingsStartupCopy = Messages["settingsStartup"];
export type PostgresPoolLane = Exclude<keyof BootstrapConfigDatabasePoolsValues, "total_max_conns">;

export interface ValidationRow {
  field: string;
  message: string;
  status: ValidationStatus;
}

export interface DangerousConfirmation {
  token: string;
  label: string;
  active: boolean;
}

export const emptySecretInputs = (): SecretInputState => ({
  "database.url": "",
  "runtime.secretEncryptionKey": "",
  "auth.jwtSigningKey": "",
  "stateTransfer.bundleEncryptionKey": "",
  "mail.smtp.password": "",
});

// Backend bootstrap responses own canonical pool sizes; legacy or incomplete payloads render missing lanes as empty fields only.
function emptyPostgresPoolForIncompletePayload(): BootstrapConfigDatabasePoolValues {
  return { max_conns: null, min_idle_conns: null };
}

export const POSTGRES_POOL_LANES: PostgresPoolLane[] = [
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
  "runtime.transport.max_idle_conns": (copy) => copy.maxIdleConns,
  "runtime.transport.max_idle_conns_per_host": (copy) => copy.maxIdlePerHost,
  "runtime.transport.max_conns_per_host": (copy) => copy.maxConnsPerHost,
  "runtime.transport.idle_conn_timeout": (copy) => copy.idleConnTimeout,
  "runtime.transport.request_timeout": (copy) => copy.requestTimeout,
  "runtime.transport.response_header_timeout": (copy) => copy.responseHeaderTimeout,
  "runtime.transport.tls_handshake_timeout": (copy) => copy.tlsHandshakeTimeout,
  "runtime.transport.expect_continue_timeout": (copy) => copy.expectContinueTimeout,
  "runtime.side_effects.attempt_timeout": (copy) => copy.sideEffectsAttemptTimeout,
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

export const SERVER_FIELD_PATHS = ["server.host", "server.port", "http.cors_allowed_origins"];
export const DATABASE_FIELD_PATHS = [
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
export const TRANSPORT_FIELD_PATHS = [
  "runtime.transport.max_idle_conns",
  "runtime.transport.max_idle_conns_per_host",
  "runtime.transport.max_conns_per_host",
  "runtime.transport.idle_conn_timeout",
  "runtime.transport.request_timeout",
  "runtime.transport.response_header_timeout",
  "runtime.transport.tls_handshake_timeout",
  "runtime.transport.expect_continue_timeout",
];
export const SIDE_EFFECT_FIELD_PATHS = ["runtime.side_effects.attempt_timeout"];
export const RUNTIME_FIELD_PATHS = [...TRANSPORT_FIELD_PATHS, ...SIDE_EFFECT_FIELD_PATHS];
export const AUTH_FIELD_PATHS = [
  "auth.jwtSigningKey",
  "auth.access_token_ttl_seconds",
  "auth.refresh_token_ttl_seconds",
  "auth.reset_code_ttl_seconds",
  "auth.access_cookie_name",
  "auth.refresh_cookie_name",
  "auth.cookie_secure",
];
export const MAIL_FIELD_PATHS = [
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
export const SECRET_FIELD_PATHS = new Set<string>(SECRET_KEYS);
export const STATE_TRANSFER_FIELD_PATHS = ["stateTransfer.bundleEncryptionKey", "runtime.secretEncryptionKey"];

export const cloneValues = (values: BootstrapConfigValues): BootstrapConfigValues => structuredClone(values);

function normalizePoolValues(pool: BootstrapConfigDatabasePoolValues | null | undefined): BootstrapConfigDatabasePoolValues {
  if (!pool) {
    return emptyPostgresPoolForIncompletePayload();
  }
  return {
    max_conns: pool.max_conns === undefined ? null : pool.max_conns,
    min_idle_conns: pool.min_idle_conns === undefined ? null : pool.min_idle_conns,
  };
}

function normalizePostgresPools(values: BootstrapConfigValues): BootstrapConfigDatabasePoolsValues {
  const pools = values.database.pools;
  return {
    total_max_conns: pools?.total_max_conns === undefined ? null : pools.total_max_conns,
    management: normalizePoolValues(pools?.management ?? values.database.management_pool),
    runtime_execution: normalizePoolValues(pools?.runtime_execution ?? values.database.runtime_pool),
    runtime_telemetry: normalizePoolValues(pools?.runtime_telemetry),
    runtime_feedback: normalizePoolValues(pools?.runtime_feedback),
    realtime: normalizePoolValues(pools?.realtime),
    cache_refresh: normalizePoolValues(pools?.cache_refresh),
    background_jobs: normalizePoolValues(pools?.background_jobs),
  };
}

export function emptyDisabledMailValuesForUiState(): BootstrapConfigMailValues {
  return {
    enabled: false,
    from: null,
    reply_to: null,
    smtp: null,
  };
}

// Used when an operator enables mail or a legacy payload lacks SMTP details; backend-provided SMTP values win when present.
export function smtpValuesForNewOrIncompleteMailConfig(): BootstrapConfigMailSMTPValues {
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

export function normalizeMailValues(mail: BootstrapConfigMailValues | null | undefined): BootstrapConfigMailValues {
  if (!mail || !mail.enabled) {
    return emptyDisabledMailValuesForUiState();
  }
  return {
    ...mail,
    enabled: true,
    smtp: mail.smtp ?? smtpValuesForNewOrIncompleteMailConfig(),
  };
}

export function normalizeBootstrapValues(values: BootstrapConfigValues): BootstrapConfigValues {
  const nextValues = cloneValues(values);
  nextValues.runtime = {
    transport: nextValues.runtime.transport,
    side_effects: nextValues.runtime.side_effects,
  };
  nextValues.database = {
    pools: normalizePostgresPools(nextValues),
    management_admission: nextValues.database.management_admission,
  };
  nextValues.mail = normalizeMailValues(nextValues.mail);
  return nextValues;
}

export function textValue(value: string | null): string {
  return value ?? "";
}

export function numberValue(value: number | null): string {
  return value === null ? "" : String(value);
}

export function parseNullableInteger(rawValue: string): number | null {
  const trimmed = rawValue.trim();
  if (!trimmed) {
    return null;
  }
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isNaN(parsed) ? null : parsed;
}

export function parseOrigins(rawValue: string): string[] {
  return rawValue.split(",").map((origin) => origin.trim()).filter(Boolean);
}

export function formatOrigins(origins: string[] | null): string {
  return (origins ?? []).join(", ");
}

export function formatDateTime(value: string | null | undefined, fallback: string): string {
  if (!value) {
    return fallback;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function isAbsoluteUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return Boolean(url.protocol && url.host);
  } catch {
    return false;
  }
}

export function isPositiveInteger(value: number | null): boolean {
  return Number.isInteger(value) && value !== null && value > 0;
}

export function isNonNegativeInteger(value: number | null): boolean {
  return Number.isInteger(value) && value !== null && value >= 0;
}

export function isValidSMTPMode(value: string | null): boolean {
  return value === "starttls_required" || value === "implicit_tls" || value === "plaintext_local_only";
}

export function isValidSMTPAuth(value: string | null): boolean {
  return value === "none" || value === "plain";
}

interface StartupValidationInput {
  bootstrapConfig: BootstrapConfigResponse | null;
  copy: SettingsStartupCopy;
  corsOriginsText: string;
  secretUpdates: BootstrapConfigSecretUpdates;
  values: BootstrapConfigValues | null;
}

export function validateStartupValues({
  bootstrapConfig,
  copy,
  corsOriginsText,
  secretUpdates,
  values,
}: StartupValidationInput): { errors: FieldErrors; rows: ValidationRow[] } {
  if (!values) {
    return { errors: {}, rows: [] };
  }
  const errors: FieldErrors = {};
  const rows: ValidationRow[] = [];
  const addError = (field: string, message: string) => {
    errors[field] = message;
    rows.push({ field, message, status: "error" });
  };
  if (!values.server.host?.trim()) addError("server.host", copy.serverHostRequired);
  if (!values.server.port || values.server.port < 1 || values.server.port > 65535) addError("server.port", copy.serverPortRange);
  const origins = parseOrigins(corsOriginsText);
  const uniqueOrigins = new Set(origins);
  if (origins.length === 0) addError("http.cors_allowed_origins", copy.corsOriginsRequired);
  else if (uniqueOrigins.size !== origins.length) addError("http.cors_allowed_origins", copy.corsOriginsUnique);
  else if (origins.some((origin) => !isAbsoluteUrl(origin))) addError("http.cors_allowed_origins", copy.corsOriginsAbsolute);
  const mailValues = normalizeMailValues(values.mail);
  if (mailValues.enabled) {
    const smtpValues = mailValues.smtp ?? smtpValuesForNewOrIncompleteMailConfig();
    if (!mailValues.from?.trim()) addError("mail.from", copy.mailFromRequired);
    if (!smtpValues.host?.trim()) addError("mail.smtp.host", copy.smtpHostRequired);
    if (!smtpValues.port || smtpValues.port < 1 || smtpValues.port > 65535) addError("mail.smtp.port", copy.smtpPortRange);
    if (!isValidSMTPMode(smtpValues.mode)) addError("mail.smtp.mode", copy.smtpModeRequired);
    if (!smtpValues.timeout?.trim()) addError("mail.smtp.timeout", copy.smtpTimeoutRequired);
    if (!isValidSMTPAuth(smtpValues.auth)) addError("mail.smtp.auth", copy.smtpAuthRequired);
    const smtpPasswordUpdate = secretUpdates["mail.smtp.password"];
    const stagedInlinePassword = smtpPasswordUpdate.action === "replace";
    const passwordFileSet = Boolean(smtpValues.password_file?.trim());
    const preservedInlinePassword = Boolean(bootstrapConfig?.secrets["mail.smtp.password"].configured && smtpPasswordUpdate.action === "preserve" && !passwordFileSet);
    if (stagedInlinePassword && passwordFileSet) {
      addError("mail.smtp.password_file", copy.smtpPasswordSourceConflict);
      addError("mail.smtp.password", copy.smtpPasswordSourceConflict);
    }
    if (smtpValues.auth === "plain") {
      if (!smtpValues.username?.trim()) addError("mail.smtp.username", copy.smtpUsernameRequired);
      const passwordSourceCount = [stagedInlinePassword, passwordFileSet, preservedInlinePassword].filter(Boolean).length;
      if (passwordSourceCount === 0) addError("mail.smtp.password", copy.smtpPasswordSourceRequired);
      else if (passwordSourceCount > 1) addError("mail.smtp.password", copy.smtpPasswordSourceConflict);
    }
  }
  const add = (field: string, message: string) => addError(field, message);
  const checkPositive = (field: string, value: number | null) => { if (!isPositiveInteger(value)) add(field, copy.usePositiveInteger); };
  const checkNonNegative = (field: string, value: number | null) => { if (!isNonNegativeInteger(value)) add(field, copy.useZeroOrPositiveInteger); };
  checkPositive("database.pools.total_max_conns", values.database.pools.total_max_conns);
  for (const lane of POSTGRES_POOL_LANES) {
    const pool = values.database.pools[lane];
    checkPositive(`database.pools.${lane}.max_conns`, pool.max_conns);
    checkNonNegative(`database.pools.${lane}.min_idle_conns`, pool.min_idle_conns);
    if ((pool.min_idle_conns ?? 0) > (pool.max_conns ?? 0)) add(`database.pools.${lane}.min_idle_conns`, copy.minIdleMustNotExceedMax);
  }
  checkPositive("database.management_admission.m2_max_concurrent", values.database.management_admission.m2_max_concurrent);
  checkPositive("database.management_admission.m3_max_concurrent", values.database.management_admission.m3_max_concurrent);
  checkPositive("runtime.transport.max_idle_conns", values.runtime.transport.max_idle_conns);
  checkPositive("runtime.transport.max_idle_conns_per_host", values.runtime.transport.max_idle_conns_per_host);
  checkNonNegative("runtime.transport.max_conns_per_host", values.runtime.transport.max_conns_per_host);
  checkPositive("auth.access_token_ttl_seconds", values.auth.access_token_ttl_seconds);
  checkPositive("auth.refresh_token_ttl_seconds", values.auth.refresh_token_ttl_seconds);
  checkPositive("auth.reset_code_ttl_seconds", values.auth.reset_code_ttl_seconds);
  if ((values.database.management_admission.m3_max_concurrent ?? 0) > (values.database.management_admission.m2_max_concurrent ?? 0)) add("database.management_admission.m3_max_concurrent", copy.m3ConcurrencyLimit);
  const checkRequiredString = (field: string, value: string | null, message = copy.useRequiredValue) => { if (!value?.trim()) add(field, message); };
  checkRequiredString("runtime.transport.idle_conn_timeout", values.runtime.transport.idle_conn_timeout);
  checkRequiredString("runtime.transport.request_timeout", values.runtime.transport.request_timeout);
  checkRequiredString("runtime.transport.response_header_timeout", values.runtime.transport.response_header_timeout);
  checkRequiredString("runtime.transport.tls_handshake_timeout", values.runtime.transport.tls_handshake_timeout);
  checkRequiredString("runtime.transport.expect_continue_timeout", values.runtime.transport.expect_continue_timeout);
  checkRequiredString("runtime.side_effects.attempt_timeout", values.runtime.side_effects.attempt_timeout, copy.sideEffectsAttemptTimeoutRequired);
  checkRequiredString("auth.access_cookie_name", values.auth.access_cookie_name);
  checkRequiredString("auth.refresh_cookie_name", values.auth.refresh_cookie_name);
  return { errors, rows };
}

export function buildPreserveSecretUpdates(): BootstrapConfigSecretUpdates {
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

export function extractBackendRows(error: unknown, copy: SettingsStartupCopy): ValidationRow[] {
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

export function getErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

export function getValidationStatusLabel(status: ValidationStatus, copy: SettingsStartupCopy): string {
  if (status === "error") {
    return copy.validationStatusError;
  }
  if (status === "warning") {
    return copy.validationStatusWarning;
  }
  return copy.validationStatusSuccess;
}

export function getPostgresPoolLaneLabel(copy: SettingsStartupCopy, lane: PostgresPoolLane): string {
  return copy[POSTGRES_POOL_LABEL_KEYS[lane]] as string;
}

function formatFieldPath(path: string): string {
  return path.split(".").map((part) => part.replaceAll("_", " ")).join(" / ");
}

export function getFieldLabel(copy: SettingsStartupCopy, path: string): string {
  const resolver = (FIELD_LABELS as Record<string, FieldLabelResolver>)[path];
  return resolver?.(copy) ?? formatFieldPath(path);
}

export function getCapabilityLabel(copy: SettingsStartupCopy, mode: BootstrapConfigApplyMode): string {
  return mode === "hot_apply" ? copy.appliesImmediately : copy.restartRequired;
}

export function getCapabilityVariant(mode: BootstrapConfigApplyMode): "secondary" | "outline" {
  return mode === "hot_apply" ? "secondary" : "outline";
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

export function getChangedCapabilityFields(
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

export function getDangerousConfirmationLabel(copy: SettingsStartupCopy, token: string, field: string): string {
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

export function buildPlannedRows(plannedChanges: BootstrapConfigPlannedChanges | undefined, copy: SettingsStartupCopy): ValidationRow[] {
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

export function summarizeApplyResult(applyResult: BootstrapConfigApplyResult | undefined, copy: SettingsStartupCopy) {
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

export function buildApplyResultRows(applyResult: BootstrapConfigApplyResult | undefined, copy: SettingsStartupCopy): ValidationRow[] {
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

export function extractBootstrapResponse(error: unknown): BootstrapConfigResponse | null {
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
