import type { SidecarInstance } from "@/lib/types";

export type SidecarFormState = {
  name: string;
  base_url: string;
  environment_label: string;
  enabled: boolean;
  management_password: string;
  sync_interval_seconds: string;
  request_timeout_seconds: string;
  allow_private_network: boolean;
  allow_insecure_http: boolean;
  skip_tls_verify: boolean;
};

export type SidecarCreatePayload = {
  name: string;
  base_url: string;
  enabled: boolean;
  environment_label?: string | null;
  management_password: string;
  sync_interval_seconds: number;
  request_timeout_seconds: number;
  allow_private_network: boolean;
  allow_insecure_http: boolean;
  skip_tls_verify: boolean;
};

export type SidecarUpdatePayload = Partial<Omit<SidecarCreatePayload, "management_password">> & {
  management_password?: string | null;
};

export const DEFAULT_SIDECAR_FORM: SidecarFormState = {
  name: "",
  base_url: "",
  environment_label: "",
  enabled: true,
  management_password: "",
  sync_interval_seconds: "300",
  request_timeout_seconds: "30",
  allow_private_network: false,
  allow_insecure_http: false,
  skip_tls_verify: false,
};

export function sidecarFormStateFromInstance(sidecar: SidecarInstance): SidecarFormState {
  return {
    name: sidecar.name,
    base_url: sidecar.base_url,
    environment_label: sidecar.environment_label ?? "",
    enabled: sidecar.enabled,
    management_password: "",
    sync_interval_seconds: String(sidecar.sync_interval_seconds),
    request_timeout_seconds: String(sidecar.request_timeout_seconds),
    allow_private_network: sidecar.allow_private_network,
    allow_insecure_http: sidecar.allow_insecure_http,
    skip_tls_verify: sidecar.skip_tls_verify,
  };
}

function parsePositiveInteger(value: string, fieldLabel: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`${fieldLabel} must be a positive whole number.`);
  }
  return parsed;
}

function normalizeEnvironmentLabel(value: string): string | null {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function basePayload(form: SidecarFormState) {
  const name = form.name.trim();
  const baseUrl = form.base_url.trim();

  if (name.length === 0) {
    throw new Error("Sidecar name is required.");
  }
  if (baseUrl.length === 0) {
    throw new Error("Base URL is required.");
  }

  return {
    name,
    base_url: baseUrl,
    enabled: form.enabled,
    environment_label: normalizeEnvironmentLabel(form.environment_label),
    sync_interval_seconds: parsePositiveInteger(form.sync_interval_seconds, "Sync interval"),
    request_timeout_seconds: parsePositiveInteger(form.request_timeout_seconds, "Request timeout"),
    allow_private_network: form.allow_private_network,
    allow_insecure_http: form.allow_insecure_http,
    skip_tls_verify: form.skip_tls_verify,
  };
}

export function toSidecarCreatePayload(form: SidecarFormState): SidecarCreatePayload {
  const managementPassword = form.management_password.trim();
  if (managementPassword.length === 0) {
    throw new Error("Management password is required for new sidecars.");
  }

  return {
    ...basePayload(form),
    management_password: managementPassword,
  };
}

export function toSidecarUpdatePayload(form: SidecarFormState): SidecarUpdatePayload {
  const managementPassword = form.management_password.trim();
  const payload: SidecarUpdatePayload = basePayload(form);

  if (managementPassword.length > 0) {
    payload.management_password = managementPassword;
  }

  return payload;
}
