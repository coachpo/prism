import type { Dispatch, FormEvent, ReactNode, SetStateAction } from "react";
import { Loader2, ShieldCheck } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import type { SidecarInstance } from "@/lib/types";
import type { SidecarFormState } from "./sidecarFormState";

interface SidecarDialogProps {
  editingSidecar: SidecarInstance | null;
  onClose: () => void;
  onOpenChange: (open: boolean) => void;
  onSave: () => Promise<void>;
  open: boolean;
  setSidecarForm: Dispatch<SetStateAction<SidecarFormState>>;
  sidecarForm: SidecarFormState;
  sidecarSaving: boolean;
}

type FieldProps = {
  children: ReactNode;
  description?: string;
  id: string;
  label: string;
};

function Field({ children, description, id, label }: FieldProps) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      {children}
    </div>
  );
}

type SwitchFieldProps = {
  checked: boolean;
  description: string;
  label: string;
  onCheckedChange: (checked: boolean) => void;
};

function SwitchField({ checked, description, label, onCheckedChange }: SwitchFieldProps) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border bg-muted/20 p-4">
      <div className="space-y-1">
        <p className="text-sm font-medium">{label}</p>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} aria-label={label} />
    </div>
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
  sidecarSaving,
}: SidecarDialogProps) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;
  const passwordConfigured = editingSidecar?.credential_state.management_password_configured ?? false;

  const updateField = (field: keyof SidecarFormState, value: string | boolean) => {
    setSidecarForm((current) => ({ ...current, [field]: value }));
  };

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
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

          <DialogBody className="space-y-5">
            <div className="rounded-xl border bg-muted/20 p-4">
              <div className="mb-4 flex items-center gap-2 text-sm font-semibold">
                <ShieldCheck className="h-4 w-4" />
                {copy.connectionSectionTitle}
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <Field id="sidecar-name" label={copy.nameLabel}>
                  <Input
                    id="sidecar-name"
                    value={sidecarForm.name}
                    placeholder={copy.namePlaceholder}
                    onChange={(event) => updateField("name", event.target.value)}
                    required
                  />
                </Field>
                <Field id="sidecar-environment" label={copy.environmentLabel}>
                  <Input
                    id="sidecar-environment"
                    value={sidecarForm.environment_label}
                    placeholder={copy.environmentPlaceholder}
                    onChange={(event) => updateField("environment_label", event.target.value)}
                  />
                </Field>
                <Field id="sidecar-base-url" label={copy.baseUrlLabel} description={copy.baseUrlDescription}>
                  <Input
                    id="sidecar-base-url"
                    value={sidecarForm.base_url}
                    placeholder={copy.baseUrlPlaceholder}
                    onChange={(event) => updateField("base_url", event.target.value)}
                    required
                  />
                </Field>
                <Field
                  id="sidecar-management-password"
                  label={copy.managementPasswordLabel}
                  description={editingSidecar ? copy.managementPasswordEditDescription : copy.managementPasswordCreateDescription}
                >
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
                      required={!editingSidecar}
                    />
                  </div>
                </Field>
              </div>
            </div>

            <div className="rounded-xl border bg-muted/20 p-4">
              <div className="mb-4 text-sm font-semibold">{copy.runtimeSectionTitle}</div>
              <div className="grid gap-4 md:grid-cols-2">
                <Field id="sidecar-sync-interval" label={copy.syncIntervalLabel}>
                  <Input
                    id="sidecar-sync-interval"
                    type="number"
                    min={1}
                    value={sidecarForm.sync_interval_seconds}
                    onChange={(event) => updateField("sync_interval_seconds", event.target.value)}
                    required
                  />
                </Field>
                <Field id="sidecar-request-timeout" label={copy.requestTimeoutLabel}>
                  <Input
                    id="sidecar-request-timeout"
                    type="number"
                    min={1}
                    value={sidecarForm.request_timeout_seconds}
                    onChange={(event) => updateField("request_timeout_seconds", event.target.value)}
                    required
                  />
                </Field>
              </div>
              <div className="mt-4 grid gap-3 md:grid-cols-2">
                <SwitchField
                  label={copy.enabledLabel}
                  description={copy.enabledDescription}
                  checked={sidecarForm.enabled}
                  onCheckedChange={(checked) => updateField("enabled", checked)}
                />
                <SwitchField
                  label={copy.allowPrivateNetworkLabel}
                  description={copy.allowPrivateNetworkDescription}
                  checked={sidecarForm.allow_private_network}
                  onCheckedChange={(checked) => updateField("allow_private_network", checked)}
                />
                <SwitchField
                  label={copy.allowInsecureHttpLabel}
                  description={copy.allowInsecureHttpDescription}
                  checked={sidecarForm.allow_insecure_http}
                  onCheckedChange={(checked) => updateField("allow_insecure_http", checked)}
                />
                <SwitchField
                  label={copy.skipTlsVerifyLabel}
                  description={copy.skipTlsVerifyDescription}
                  checked={sidecarForm.skip_tls_verify}
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
