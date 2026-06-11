import { z } from "zod";
import type { Messages } from "@/i18n/messages/en";
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

type SidecarsPageMessages = Messages["sidecarsPage"];

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

const positiveIntegerString = z.string().trim().regex(/^\d+$/, "Must be a positive whole number").refine((value) => Number(value) > 0, "Must be a positive whole number");

export const sidecarBaseFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  base_url: z.string().trim().pipe(z.url("Base URL must be a valid URL")),
  environment_label: z.string(),
  enabled: z.boolean(),
  management_password: z.string(),
  sync_interval_seconds: positiveIntegerString,
  request_timeout_seconds: positiveIntegerString,
  allow_private_network: z.boolean(),
  allow_insecure_http: z.boolean(),
  skip_tls_verify: z.boolean(),
}) satisfies z.ZodType<SidecarFormState>;

export const sidecarCreateFormSchema = sidecarBaseFormSchema.superRefine((value, context) => {
  if (value.management_password.trim().length === 0) {
    context.addIssue({ code: "custom", path: ["management_password"], message: "Management password is required" });
  }
});

export const sidecarUpdateFormSchema = sidecarBaseFormSchema;

export type SidecarFormField = keyof SidecarFormState;
export type SidecarFormErrors = Partial<Record<SidecarFormField, string>>;

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

function normalizeEnvironmentLabel(value: string): string | null {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : null;
}

function sidecarFormErrors(error: z.ZodError<SidecarFormState>): SidecarFormErrors {
  const errors: SidecarFormErrors = {};
  for (const issue of error.issues) {
    const field = issue.path[0] as SidecarFormField | undefined;
    if (field && !errors[field]) {
      errors[field] = issue.message;
    }
  }
  return errors;
}

export function validateSidecarForm(form: SidecarFormState, options: { editing: boolean }): { errors: SidecarFormErrors; values: SidecarFormState | null } {
  const schema = options.editing ? sidecarUpdateFormSchema : sidecarCreateFormSchema;
  const result = schema.safeParse(form);
  if (result.success) {
    return { errors: {}, values: result.data };
  }
  return { errors: sidecarFormErrors(result.error), values: null };
}

function basePayload(form: SidecarFormState) {
  return {
    name: form.name.trim(),
    base_url: form.base_url.trim(),
    enabled: form.enabled,
    environment_label: normalizeEnvironmentLabel(form.environment_label),
    sync_interval_seconds: Number(form.sync_interval_seconds),
    request_timeout_seconds: Number(form.request_timeout_seconds),
    allow_private_network: form.allow_private_network,
    allow_insecure_http: form.allow_insecure_http,
    skip_tls_verify: form.skip_tls_verify,
  };
}

export function firstSidecarFormError(errors: SidecarFormErrors, copy: SidecarsPageMessages): string {
  return errors.name
    ?? errors.base_url
    ?? errors.management_password
    ?? (errors.sync_interval_seconds ? copy.validationPositiveWholeNumber(copy.syncIntervalLabel) : null)
    ?? (errors.request_timeout_seconds ? copy.validationPositiveWholeNumber(copy.requestTimeoutLabel) : null)
    ?? copy.saveFailed;
}

export function toSidecarCreatePayload(form: SidecarFormState, copy: SidecarsPageMessages): SidecarCreatePayload {
  const validation = validateSidecarForm(form, { editing: false });
  if (!validation.values) {
    throw new Error(firstSidecarFormError(validation.errors, copy));
  }
  return { ...basePayload(validation.values), management_password: validation.values.management_password.trim() };
}

export function toSidecarUpdatePayload(form: SidecarFormState, copy: SidecarsPageMessages): SidecarUpdatePayload {
  const validation = validateSidecarForm(form, { editing: true });
  if (!validation.values) {
    throw new Error(firstSidecarFormError(validation.errors, copy));
  }
  const managementPassword = validation.values.management_password.trim();
  const payload: SidecarUpdatePayload = basePayload(validation.values);
  if (managementPassword.length > 0) payload.management_password = managementPassword;
  return payload;
}
