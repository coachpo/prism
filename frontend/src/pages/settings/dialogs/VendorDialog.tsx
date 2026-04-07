import { VendorIcon } from "@/components/VendorIcon";
import { vendorIconPresetOptions } from "@/components/vendorIconRegistry";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type { Vendor } from "@/lib/types";
import type { FormEvent } from "react";
import type { VendorFormState } from "../vendorManagementFormState";

interface VendorDialogProps {
  editingVendor: Vendor | null;
  onClose: () => void;
  onSave: () => Promise<void>;
  open: boolean;
  setVendorForm: (updater: VendorFormState | ((current: VendorFormState) => VendorFormState)) => void;
  vendorForm: VendorFormState;
  vendorSaving: boolean;
}

export function VendorDialog({
  editingVendor,
  onClose,
  onSave,
  open,
  setVendorForm,
  vendorForm,
  vendorSaving,
}: VendorDialogProps) {
  const { messages } = useLocale();
  const isEditing = editingVendor !== null;
  const isReadonlyEditing = editingVendor?.is_readonly === true;
  const currentIconValue = vendorIconPresetOptions.some((option) => option.icon_key === vendorForm.icon_key)
    ? vendorForm.icon_key
    : "__fallback__";
  const previewVendor = {
    key: vendorForm.key,
    name: vendorForm.name,
    icon_key: vendorForm.icon_key,
  };
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void onSave();
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isEditing ? messages.vendorManagement.editVendor : messages.vendorManagement.createVendor}
          </DialogTitle>
          <DialogDescription>{messages.vendorManagement.sectionDescription}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="flex flex-col gap-5">
          <input type="hidden" name="icon_key" value={vendorForm.icon_key ?? ""} />
          <DialogBody>
            <div className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="vendor-name">{messages.vendorManagement.nameLabel}</Label>
                  <Input
                    id="vendor-name"
                    name="name"
                    autoComplete="off"
                    value={vendorForm.name}
                    disabled={isReadonlyEditing}
                    onChange={(event) =>
                      setVendorForm((current) => ({ ...current, name: event.target.value }))
                    }
                    placeholder={messages.vendorManagement.namePlaceholder}
                  />
                </div>

                <div className="flex flex-col gap-2">
                  <Label htmlFor="vendor-key">{messages.vendorManagement.keyLabel}</Label>
                  <Input
                    id="vendor-key"
                    name="key"
                    autoComplete="off"
                    value={vendorForm.key}
                    disabled={isReadonlyEditing}
                    onChange={(event) =>
                      setVendorForm((current) => ({ ...current, key: event.target.value }))
                    }
                    placeholder={messages.vendorManagement.keyPlaceholder}
                  />
                </div>
              </div>
            </div>

            <div className="flex flex-col gap-4 rounded-lg border p-4">
              <div className="flex flex-col gap-2">
                <Label htmlFor="vendor-description">{messages.vendorManagement.descriptionLabel}</Label>
                <Input
                  id="vendor-description"
                  name="description"
                  autoComplete="off"
                  value={vendorForm.description}
                  disabled={isReadonlyEditing}
                  onChange={(event) =>
                    setVendorForm((current) => ({ ...current, description: event.target.value }))
                  }
                  placeholder={messages.vendorManagement.descriptionPlaceholder}
                />
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="vendor-icon-key">{messages.vendorManagement.iconPresetLabel}</Label>
                <Select
                  value={currentIconValue ?? "__fallback__"}
                  disabled={isReadonlyEditing}
                  onValueChange={(value) =>
                    setVendorForm((current) => ({
                      ...current,
                      icon_key: value === "__fallback__" ? null : value,
                    }))
                  }
                >
                  <SelectTrigger id="vendor-icon-key">
                    <SelectValue placeholder={messages.vendorManagement.iconPresetPlaceholder} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__fallback__">
                      {messages.vendorManagement.iconPresetFallbackOption}
                    </SelectItem>
                    {vendorIconPresetOptions.map((option) => (
                      <SelectItem key={option.icon_key} value={option.icon_key}>
                        <span className="flex items-center gap-2">
                          <VendorIcon
                            vendor={{ key: option.icon_key, name: option.label, icon_key: option.icon_key }}
                            size={14}
                          />
                          <span>{option.label}</span>
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <p className="text-xs leading-5 text-muted-foreground">{messages.vendorManagement.iconPresetHelp}</p>
              </div>
            </div>

            <div className="flex items-start gap-4 rounded-lg border bg-muted/25 p-4">
              <VendorIcon vendor={previewVendor} size={40} className="rounded-lg" />
              <div className="flex min-w-0 flex-1 flex-col gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate text-sm font-medium text-foreground">
                    {vendorForm.name.trim() || vendorForm.key.trim() || "—"}
                  </p>
                  {vendorForm.key.trim() ? (
                    <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                      {vendorForm.key.trim()}
                    </code>
                  ) : null}
                </div>
                <p className="text-xs font-medium text-foreground">
                  {messages.vendorManagement.currentIconPreviewLabel}
                </p>
                <p className="text-xs leading-5 text-muted-foreground">
                  {messages.vendorManagement.fallbackPreviewDescription}
                </p>
              </div>
            </div>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" onClick={onClose}>
              {messages.vendorManagement.cancel}
            </Button>
            <Button type="submit" disabled={vendorSaving || isReadonlyEditing}>
              {vendorSaving
                ? messages.vendorManagement.saving
                : isEditing
                  ? messages.vendorManagement.saveEdit
                  : messages.vendorManagement.saveCreate}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
