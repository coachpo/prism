import type { Dispatch, FormEvent, SetStateAction } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import type { PricingTemplate } from "@/lib/types";
import type { PricingTemplateFormState } from "./pricingTemplateFormState";

interface PricingTemplateDialogProps {
  editingPricingTemplate: PricingTemplate | null;
  onClose: () => void;
  onOpenChange: (open: boolean) => void;
  onSave: () => Promise<void>;
  open: boolean;
  pricingTemplateForm: PricingTemplateFormState;
  pricingTemplateSaving: boolean;
  setPricingTemplateForm: Dispatch<SetStateAction<PricingTemplateFormState>>;
}

type PricingFieldCardProps = {
  id: string;
  label: string;
  name: keyof Pick<
    PricingTemplateFormState,
    | "input_price"
    | "output_price"
    | "cached_input_price"
    | "cache_creation_price"
    | "reasoning_price"
  >;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
};

function PricingFieldCard({
  id,
  label,
  name,
  onChange,
  placeholder,
  value,
}: PricingFieldCardProps) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border bg-background p-3">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        name={name}
        autoComplete="off"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
      />
    </div>
  );
}

export function PricingTemplateDialog({
  editingPricingTemplate,
  onClose,
  onOpenChange,
  onSave,
  open,
  pricingTemplateForm,
  pricingTemplateSaving,
  setPricingTemplateForm,
}: PricingTemplateDialogProps) {
  const { messages } = useLocale();
  const dialogMessages = messages.pricingTemplateDialog;

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
          return;
        }
        onOpenChange(nextOpen);
      }}
    >
      <DialogContent aria-describedby={undefined} className="max-h-[90vh] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {editingPricingTemplate ? dialogMessages.editTitle : dialogMessages.addTitle}
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="flex min-h-0 flex-col gap-5">
          <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
            <div className="flex flex-col gap-5">
              <section className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
                <div className="flex flex-col gap-1">
                  <p className="text-sm font-medium text-foreground">{dialogMessages.detailsSectionTitle}</p>
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="template-name">{dialogMessages.nameLabel}</Label>
                    <Input
                      id="template-name"
                      name="name"
                      autoComplete="off"
                      value={pricingTemplateForm.name}
                      onChange={(event) =>
                        setPricingTemplateForm((prev) => ({ ...prev, name: event.target.value }))
                      }
                      placeholder={dialogMessages.namePlaceholder}
                    />
                  </div>

                  <div className="flex flex-col gap-2">
                    <Label htmlFor="template-currency">{dialogMessages.currencyCodeLabel}</Label>
                    <Input
                      id="template-currency"
                      name="pricing_currency_code"
                      autoComplete="off"
                      value={pricingTemplateForm.pricing_currency_code}
                      onChange={(event) =>
                        setPricingTemplateForm((prev) => ({
                          ...prev,
                          pricing_currency_code: event.target.value.toUpperCase(),
                        }))
                      }
                      placeholder={dialogMessages.currencyCodePlaceholder}
                      maxLength={3}
                    />
                  </div>
                </div>

                <div className="flex flex-col gap-2">
                  <Label htmlFor="template-description">{dialogMessages.descriptionLabel}</Label>
                  <Input
                    id="template-description"
                    name="description"
                    autoComplete="off"
                    value={pricingTemplateForm.description}
                    onChange={(event) =>
                      setPricingTemplateForm((prev) => ({ ...prev, description: event.target.value }))
                    }
                    placeholder={dialogMessages.descriptionPlaceholder}
                  />
                </div>
              </section>

              <section className="flex flex-col gap-4 rounded-lg border p-4">
                <div className="flex flex-col gap-1">
                  <p className="text-sm font-medium text-foreground">{dialogMessages.baseRatesSectionTitle}</p>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <PricingFieldCard
                    id="template-input-price"
                    label={dialogMessages.inputPriceLabel}
                    name="input_price"
                    value={pricingTemplateForm.input_price}
                    onChange={(value) =>
                      setPricingTemplateForm((prev) => ({ ...prev, input_price: value }))
                    }
                    placeholder={dialogMessages.pricePlaceholder}
                  />
                  <PricingFieldCard
                    id="template-output-price"
                    label={dialogMessages.outputPriceLabel}
                    name="output_price"
                    value={pricingTemplateForm.output_price}
                    onChange={(value) =>
                      setPricingTemplateForm((prev) => ({ ...prev, output_price: value }))
                    }
                    placeholder={dialogMessages.pricePlaceholder}
                  />
                </div>
              </section>

              <section className="flex flex-col gap-4 rounded-lg border bg-muted/15 p-4">
                <div className="flex flex-col gap-1">
                  <p className="text-sm font-medium text-foreground">{dialogMessages.componentRatesSectionTitle}</p>
                  <p className="text-sm text-muted-foreground">{dialogMessages.componentRatesSectionDescription}</p>
                </div>

                <div className="grid gap-3 md:grid-cols-3">
                  <PricingFieldCard
                    id="template-cached-input-price"
                    label={dialogMessages.cachedInputPriceLabel}
                    name="cached_input_price"
                    value={pricingTemplateForm.cached_input_price}
                    onChange={(value) =>
                      setPricingTemplateForm((prev) => ({ ...prev, cached_input_price: value }))
                    }
                    placeholder={dialogMessages.pricePlaceholder}
                  />
                  <PricingFieldCard
                    id="template-cache-creation-price"
                    label={dialogMessages.cacheCreationPriceLabel}
                    name="cache_creation_price"
                    value={pricingTemplateForm.cache_creation_price}
                    onChange={(value) =>
                      setPricingTemplateForm((prev) => ({ ...prev, cache_creation_price: value }))
                    }
                    placeholder={dialogMessages.pricePlaceholder}
                  />
                  <PricingFieldCard
                    id="template-reasoning-price"
                    label={dialogMessages.reasoningPriceLabel}
                    name="reasoning_price"
                    value={pricingTemplateForm.reasoning_price}
                    onChange={(value) =>
                      setPricingTemplateForm((prev) => ({ ...prev, reasoning_price: value }))
                    }
                    placeholder={dialogMessages.pricePlaceholder}
                  />
                </div>
              </section>
            </div>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" onClick={onClose}>
              {dialogMessages.cancel}
            </Button>
            <Button type="submit" disabled={pricingTemplateSaving}>
              {pricingTemplateSaving ? dialogMessages.saving : dialogMessages.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
