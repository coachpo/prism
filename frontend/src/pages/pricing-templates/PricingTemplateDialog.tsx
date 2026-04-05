import type { Dispatch, FormEvent, SetStateAction } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
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
        <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            {editingPricingTemplate ? dialogMessages.editTitle : dialogMessages.addTitle}
          </DialogTitle>
          <DialogDescription>{dialogMessages.description}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="grid gap-4 py-4">
          <input
            type="hidden"
            name="missing_special_token_price_policy"
            value={pricingTemplateForm.missing_special_token_price_policy}
          />
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
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
            <div className="space-y-2">
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

          <div className="space-y-2">
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

          <div className="grid grid-cols-2 gap-4 rounded-md border bg-muted/20 p-4">
            <div className="space-y-2">
              <Label htmlFor="template-input-price">{dialogMessages.inputPriceLabel}</Label>
              <Input
                id="template-input-price"
                name="input_price"
                autoComplete="off"
                value={pricingTemplateForm.input_price}
                onChange={(event) =>
                  setPricingTemplateForm((prev) => ({ ...prev, input_price: event.target.value }))
                }
                placeholder={dialogMessages.pricePlaceholder}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="template-output-price">{dialogMessages.outputPriceLabel}</Label>
              <Input
                id="template-output-price"
                name="output_price"
                autoComplete="off"
                value={pricingTemplateForm.output_price}
                onChange={(event) =>
                  setPricingTemplateForm((prev) => ({ ...prev, output_price: event.target.value }))
                }
                placeholder={dialogMessages.pricePlaceholder}
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4 rounded-md border bg-muted/20 p-4">
            <div className="space-y-2">
              <Label htmlFor="template-cached-input-price">{dialogMessages.cachedInputPriceLabel}</Label>
              <Input
                id="template-cached-input-price"
                name="cached_input_price"
                autoComplete="off"
                value={pricingTemplateForm.cached_input_price}
                onChange={(event) =>
                  setPricingTemplateForm((prev) => ({
                    ...prev,
                    cached_input_price: event.target.value,
                  }))
                }
                placeholder={dialogMessages.pricePlaceholder}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="template-cache-creation-price">{dialogMessages.cacheCreationPriceLabel}</Label>
              <Input
                id="template-cache-creation-price"
                name="cache_creation_price"
                autoComplete="off"
                value={pricingTemplateForm.cache_creation_price}
                onChange={(event) =>
                  setPricingTemplateForm((prev) => ({
                    ...prev,
                    cache_creation_price: event.target.value,
                  }))
                }
                placeholder={dialogMessages.pricePlaceholder}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="template-reasoning-price">{dialogMessages.reasoningPriceLabel}</Label>
              <Input
                id="template-reasoning-price"
                name="reasoning_price"
                autoComplete="off"
                value={pricingTemplateForm.reasoning_price}
                onChange={(event) =>
                  setPricingTemplateForm((prev) => ({
                    ...prev,
                    reasoning_price: event.target.value,
                  }))
                }
                placeholder={dialogMessages.pricePlaceholder}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="template-missing-special-token-policy">
              {dialogMessages.missingSpecialTokenPolicyLabel}
            </Label>
            <Select
              value={pricingTemplateForm.missing_special_token_price_policy}
              onValueChange={(value: "MAP_TO_OUTPUT" | "ZERO_COST") =>
                setPricingTemplateForm((prev) => ({
                  ...prev,
                  missing_special_token_price_policy: value,
                }))
              }
            >
              <SelectTrigger id="template-missing-special-token-policy">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="MAP_TO_OUTPUT">{dialogMessages.mapToOutputPrice}</SelectItem>
                <SelectItem value="ZERO_COST">{dialogMessages.zeroCost}</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{dialogMessages.missingSpecialTokenPolicyHint}</p>
          </div>
          <DialogFooter>
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
