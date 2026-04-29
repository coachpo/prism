export type BootstrapConfigSecretKey =
  | "database.url"
  | "runtime.secretEncryptionKey"
  | "auth.jwtSigningKey"
  | "stateTransfer.bundleEncryptionKey"
  | "mail.smtp.password";

export type BootstrapConfigSecretAction = "preserve" | "replace";

export type BootstrapConfigConfirmationToken =
  | "server-host-change"
  | "server-port-change"
  | "database-url-change"
  | "auth-jwt-signing-key-change"
  | "state-transfer-bundle-encryption-key-change";

export interface BootstrapConfigServerValues {
  host: string | null;
  port: number | null;
  docs_enabled: boolean | null;
}

export interface BootstrapConfigDatabasePoolValues {
  max_conns: number | null;
  min_idle_conns: number | null;
}

export interface BootstrapConfigManagementAdmissionValues {
  m2_max_concurrent: number | null;
  m3_max_concurrent: number | null;
}

export interface BootstrapConfigDatabaseValues {
  runtime_pool: BootstrapConfigDatabasePoolValues;
  management_pool: BootstrapConfigDatabasePoolValues;
  management_admission: BootstrapConfigManagementAdmissionValues;
}

export interface BootstrapConfigRuntimeTransportValues {
  max_idle_conns: number | null;
  max_idle_conns_per_host: number | null;
  max_conns_per_host: number | null;
  idle_conn_timeout: string | null;
  request_timeout: string | null;
  response_header_timeout: string | null;
  tls_handshake_timeout: string | null;
  expect_continue_timeout: string | null;
}

export interface BootstrapConfigRuntimeValues {
  buffering_mode: string | null;
  transport: BootstrapConfigRuntimeTransportValues;
}

export interface BootstrapConfigHTTPValues {
  cors_allowed_origins: string[] | null;
}

export interface BootstrapConfigAuthValues {
  access_token_ttl_seconds: number | null;
  refresh_token_ttl_seconds: number | null;
  reset_code_ttl_seconds: number | null;
  access_cookie_name: string | null;
  refresh_cookie_name: string | null;
  cookie_secure: boolean | null;
}

export interface BootstrapConfigMailSMTPValues {
  host: string | null;
  port: number | null;
  mode: string | null;
  ehlo_hostname: string | null;
  auth: string | null;
  username: string | null;
  password_file: string | null;
  timeout: string | null;
  tls_server_name: string | null;
}

export interface BootstrapConfigMailValues {
  enabled: boolean | null;
  from: string | null;
  reply_to: string | null;
  smtp: BootstrapConfigMailSMTPValues | null;
}

export interface BootstrapConfigValues {
  server: BootstrapConfigServerValues;
  database: BootstrapConfigDatabaseValues;
  runtime: BootstrapConfigRuntimeValues;
  http: BootstrapConfigHTTPValues;
  auth: BootstrapConfigAuthValues;
  mail?: BootstrapConfigMailValues | null;
}

export interface BootstrapConfigSecretMetadata {
  configured: boolean;
  editable: boolean;
  masked: string;
}

export type BootstrapConfigSecrets = Record<
  BootstrapConfigSecretKey,
  BootstrapConfigSecretMetadata
>;

export type BootstrapConfigSecretUpdate =
  | { action: "preserve"; value?: never }
  | { action: "replace"; value: string };

export type BootstrapConfigSecretUpdates = Record<
  BootstrapConfigSecretKey,
  BootstrapConfigSecretUpdate
>;

export interface BootstrapConfigResponse {
  config_path: string;
  schema_version: number;
  file_revision: number;
  loaded_revision: number;
  document_etag: string;
  loaded_document_etag: string;
  created_at: string;
  updated_at: string;
  restart_required: boolean;
  writable: boolean;
  values: BootstrapConfigValues;
  secrets: BootstrapConfigSecrets;
}

export interface BootstrapConfigUpdateRequest {
  expected_revision: number;
  expected_etag: string;
  values: BootstrapConfigValues;
  secret_updates: BootstrapConfigSecretUpdates;
  confirmations?: BootstrapConfigConfirmationToken[];
}
