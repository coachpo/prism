import type { ComponentProps, Dispatch, SetStateAction } from "react";
import { AlertCircle, Loader2, ShieldCheck } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldContent, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type { SidecarInstance } from "@/lib/types";
import type { SidecarFormErrors, SidecarFormState } from "./sidecarFormState";

interface SidecarDialogProps {
  editingSidecar: SidecarInstance | null;
  onClose: () => void;
  onOpenChange: (open: boolean) => void;
  onSave: () => Promise<void>;
  open: boolean;
  setSidecarForm: Dispatch<SetStateAction<SidecarFormState>>;
  sidecarForm: SidecarFormState;
  sidecarFormErrors: SidecarFormErrors;
  sidecarSaving: boolean;
  serverError: string | null;
}

type SwitchFieldProps = {
  checked: boolean;
  description: string;
  disabled: boolean;
  label: string;
  onCheckedChange: (checked: boolean) => void;
};

function SwitchField({ checked, description, disabled, label, onCheckedChange }: SwitchFieldProps) {
  return (
    <Field orientation="horizontal" data-disabled={disabled || undefined} className="rounded-lg border bg-muted/20 p-4">
      <FieldContent>
        <FieldLabel>{label}</FieldLabel>
        <FieldDescription>{description}</FieldDescription>
      </FieldContent>
      <Switch checked={checked} onCheckedChange={onCheckedChange} aria-label={label} disabled={disabled} />
    </Field>
  );
}

export function SidecarDialog({
  editingSidecar,
  onClose,
  onOpenChange,
  onSave,
  open,
  setSidecarForm,
  sidecarForm,
  sidecarFormErrors,
  sidecarSaving,
  serverError,
}: SidecarDialogProps) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;
  const passwordConfigured = editingSidecar?.credential_state.management_password_configured ?? false;

  const updateField = (field: keyof SidecarFormState, value: string | boolean) => {
    setSidecarForm((current) => ({ ...current, [field]: value }));
  };

  const handleSubmit: NonNullable<ComponentProps<"form">["onSubmit"]> = (event) => {
    event.preventDefault();
    void onSave();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="sm:max-w-2xl" showCloseButton={!sidecarSaving}>
        <form onSubmit={handleSubmit} className="contents">
          <DialogHeader>
            <DialogTitle>{editingSidecar ? copy.editTitle : copy.createTitle}</DialogTitle>
            <DialogDescription>{copy.dialogDescription}</DialogDescription>
          </DialogHeader>

          <DialogBody className="flex flex-col gap-5">
            {serverError ? (
              <Alert variant="destructive" data-testid="sidecar-form-server-error">
                <AlertCircle />
                <AlertTitle>{copy.saveFailed}</AlertTitle>
                <AlertDescription className="whitespace-pre-line">{serverError}</AlertDescription>
              </Alert>
            ) : null}
            <div className="rounded-xl border bg-muted/20 p-4">
              <div className="mb-4 flex items-center gap-2 text-sm font-semibold">
                <ShieldCheck className="h-4 w-4" />
                {copy.connectionSectionTitle}
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <Field data-invalid={Boolean(sidecarFormErrors.name) || undefined} data-disabled={sidecarSaving || undefined}>
                  <FieldLabel htmlFor="sidecar-name">{copy.nameLabel}</FieldLabel>
                  <Input
                    id="sidecar-name"
                    value={sidecarForm.name}
                    placeholder={copy.namePlaceholder}
                    onChange={(event) => updateField("name", event.target.value)}
                    disabled={sidecarSaving}
                    aria-invalid={Boolean(sidecarFormErrors.name) || undefined}
                  />
                  <FieldError>{sidecarFormErrors.name}</FieldError>
                </Field>
                <Field data-invalid={Boolean(sidecarFormErrors.environment_label) || undefined} data-disabled={sidecarSaving || undefined}>
                  <FieldLabel htmlFor="sidecar-environment">{copy.environmentLabel}</FieldLabel>
                  <Input
                    id="sidecar-environment"
                    value={sidecarForm.environment_label}
                    placeholder={copy.environmentPlaceholder}
                    onChange={(event) => updateField("environment_label", event.target.value)}
                    disabled={sidecarSaving}
                    aria-invalid={Boolean(sidecarFormErrors.environment_label) || undefined}
                  />
                  <FieldError>{sidecarFormErrors.environment_label}</FieldError>
                </Field>
                <Field data-invalid={Boolean(sidecarFormErrors.base_url) || undefined} data-disabled={sidecarSaving || undefined}>
                  <FieldLabel htmlFor="sidecar-base-url">{copy.baseUrlLabel}</FieldLabel>
                  <FieldDescription>{copy.baseUrlDescription}</FieldDescription>
                  <Input
                    id="sidecar-base-url"
                    value={sidecarForm.base_url}
                    placeholder={copy.baseUrlPlaceholder}
                    onChange={(event) => updateField("base_url", event.target.value)}
                    disabled={sidecarSaving}
                    aria-invalid={Boolean(sidecarFormErrors.base_url) || undefined}
                  />
                  <FieldError>{sidecarFormErrors.base_url}</FieldError>
                </Field>
                <Field data-invalid={Boolean(sidecarFormErrors.management_password) || undefined} data-disabled={sidecarSaving || undefined}>
                  <FieldLabel htmlFor="sidecar-management-password">{copy.managementPasswordLabel}</FieldLabel>
                  <FieldDescription>{editingSidecar ? copy.managementPasswordEditDescription : copy.managementPasswordCreateDescription}</FieldDescription>
                  <div className="flex flex-col gap-2">
                    {editingSidecar ? (
                      <Badge variant="outline" className="w-fit text-[10px] text-muted-foreground">
                        {passwordConfigured ? copy.passwordConfigured : copy.passwordMissing}
                      </Badge>
                    ) : null}
                    <Input
                      id="sidecar-management-password"
                      type="password"
                      value={sidecarForm.management_password}
                      placeholder={editingSidecar ? copy.managementPasswordEditPlaceholder : copy.managementPasswordCreatePlaceholder}
                      onChange={(event) => updateField("management_password", event.target.value)}
                      disabled={sidecarSaving}
                      aria-invalid={Boolean(sidecarFormErrors.management_password) || undefined}
                    />
                  </div>
                  <FieldError>{sidecarFormErrors.management_password}</FieldError>
                </Field>
              </div>
            </div>

            <div className="rounded-xl border bg-muted/20 p-4">
              <div className="mb-4 text-sm font-semibold">{copy.runtimeSectionTitle}</div>
              <div className="grid gap-4 md:grid-cols-2">
                <Field data-invalid={Boolean(sidecarFormErrors.sync_interval_seconds) || undefined} data-disabled={sidecarSaving || undefined}>
                  <FieldLabel htmlFor="sidecar-sync-interval">{copy.syncIntervalLabel}</FieldLabel>
                  <Input
                    id="sidecar-sync-interval"
                    type="number"
                    min={1}
                    value={sidecarForm.sync_interval_seconds}
                    onChange={(event) => updateField("sync_interval_seconds", event.target.value)}
                    disabled={sidecarSaving}
                    aria-invalid={Boolean(sidecarFormErrors.sync_interval_seconds) || undefined}
                  />
                  <FieldError>{sidecarFormErrors.sync_interval_seconds}</FieldError>
                </Field>
                <Field data-invalid={Boolean(sidecarFormErrors.request_timeout_seconds) || undefined} data-disabled={sidecarSaving || undefined}>
                  <FieldLabel htmlFor="sidecar-request-timeout">{copy.requestTimeoutLabel}</FieldLabel>
                  <Input
                    id="sidecar-request-timeout"
                    type="number"
                    min={1}
                    value={sidecarForm.request_timeout_seconds}
                    onChange={(event) => updateField("request_timeout_seconds", event.target.value)}
                    disabled={sidecarSaving}
                    aria-invalid={Boolean(sidecarFormErrors.request_timeout_seconds) || undefined}
                  />
                  <FieldError>{sidecarFormErrors.request_timeout_seconds}</FieldError>
                </Field>
              </div>
              <div className="mt-4 grid gap-3 md:grid-cols-2">
                <SwitchField
                  label={copy.enabledLabel}
                  description={copy.enabledDescription}
                  checked={sidecarForm.enabled}
                  disabled={sidecarSaving}
                  onCheckedChange={(checked) => updateField("enabled", checked)}
                />
                <SwitchField
                  label={copy.allowPrivateNetworkLabel}
                  description={copy.allowPrivateNetworkDescription}
                  checked={sidecarForm.allow_private_network}
                  disabled={sidecarSaving}
                  onCheckedChange={(checked) => updateField("allow_private_network", checked)}
                />
                <SwitchField
                  label={copy.allowInsecureHttpLabel}
                  description={copy.allowInsecureHttpDescription}
                  checked={sidecarForm.allow_insecure_http}
                  disabled={sidecarSaving}
                  onCheckedChange={(checked) => updateField("allow_insecure_http", checked)}
                />
                <SwitchField
                  label={copy.skipTlsVerifyLabel}
                  description={copy.skipTlsVerifyDescription}
                  checked={sidecarForm.skip_tls_verify}
                  disabled={sidecarSaving}
                  onCheckedChange={(checked) => updateField("skip_tls_verify", checked)}
                />
              </div>
            </div>
          </DialogBody>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={sidecarSaving}>
              {copy.cancel}
            </Button>
            <Button type="submit" disabled={sidecarSaving}>
              {sidecarSaving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {sidecarSaving ? copy.saving : copy.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
