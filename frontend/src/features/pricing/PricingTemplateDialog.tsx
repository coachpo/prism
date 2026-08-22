import { useEffect } from "react";
import { useForm, useWatch, type FieldPath } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
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
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type { PricingTemplate, PricingTemplateImpact } from "@/lib/types";
import { OperatorCallout, OperatorErrorState, OperatorInsetPanel, OperatorRetryButton } from "@/shared/design-system";
import {
  fieldErrorsFromServerValidation,
  type ServerValidationResult,
} from "@/shared/forms/serverValidation";
import {
  DEFAULT_PRICING_TEMPLATE_FORM,
  pricingTemplateFormSchema,
  pricingTemplateFormStateFromTemplate,
  type PricingTemplateFormValues,
} from "./pricingSchemas";
import { PricingCardFields } from "./PricingCardFields";
import { PricingPeakValleyFields } from "./PricingPeakValleyFields";
import { PricingTierFields } from "./PricingTierFields";

interface PricingTemplateDialogProps {
  editingPricingTemplate: PricingTemplate | null;
  impact: PricingTemplateImpact | null;
  impactError: string | null;
  impactLoading: boolean;
  onClose: () => void;
  onOpenChange: (open: boolean) => void;
  onRetryImpact: () => void;
  onSave: (values: PricingTemplateFormValues) => Promise<void>;
  open: boolean;
  pricingTemplateSaving: boolean;
  serverValidation?: ServerValidationResult | null;
}

const staticPricingWireFields = [
  "name", "template_kind", "card.input_price", "card.output_price", "card.cached_input_price", "card.cache_creation_price", "card.reasoning_price",
  "base_card.input_price", "base_card.output_price", "base_card.cached_input_price", "base_card.cache_creation_price", "base_card.reasoning_price",
  "tier.input_tokens_above", "tier.card.input_price", "tier.card.output_price", "tier.card.cached_input_price", "tier.card.cache_creation_price", "tier.card.reasoning_price",
  "peak_card.input_price", "peak_card.output_price", "peak_card.cached_input_price", "peak_card.cache_creation_price", "peak_card.reasoning_price",
  "offpeak_card.input_price", "offpeak_card.output_price", "offpeak_card.cached_input_price", "offpeak_card.cache_creation_price", "offpeak_card.reasoning_price", "schedule.timezone",
] as const;

function pricingFormPath(path: string): FieldPath<PricingTemplateFormValues> | null {
  if (path === "schedule.timezone") return "schedule_timezone";
  const window = /^schedule\.windows\[(\d+)]\.(weekday_mask|start_minute|end_minute)$/.exec(path);
  if (window) return `schedule_windows.${window[1]}.${window[2]}` as FieldPath<PricingTemplateFormValues>;
  if (path.startsWith("tier.card.")) return `tier.${path.slice("tier.card.".length)}` as FieldPath<PricingTemplateFormValues>;
  if (path.startsWith("base_card.")) return path.slice("base_card.".length) as FieldPath<PricingTemplateFormValues>;
  if (path.startsWith("card.")) return path.slice("card.".length) as FieldPath<PricingTemplateFormValues>;
  if (path.startsWith("peak_card.") || path.startsWith("offpeak_card.") || path === "tier.input_tokens_above" || path === "name" || path === "template_kind") return path as FieldPath<PricingTemplateFormValues>;
  return null;
}

export function PricingTemplateDialog({
  editingPricingTemplate,
  impact,
  impactError,
  impactLoading,
  onClose,
  onOpenChange,
  onRetryImpact,
  onSave,
  open,
  pricingTemplateSaving,
  serverValidation,
}: PricingTemplateDialogProps) {
  const { messages } = useLocale();
  const dialogMessages = messages.pricingTemplateDialog;
  const form = useForm<PricingTemplateFormValues>({
    resolver: zodResolver(pricingTemplateFormSchema),
    defaultValues: DEFAULT_PRICING_TEMPLATE_FORM,
  });

  const templateKind = useWatch({
    control: form.control,
    name: "template_kind",
  });

  useEffect(() => {
    if (!open) return;
    form.reset(
      editingPricingTemplate
        ? pricingTemplateFormStateFromTemplate(editingPricingTemplate)
        : DEFAULT_PRICING_TEMPLATE_FORM,
    );
  }, [editingPricingTemplate, form, open]);

  useEffect(() => {
    if (!serverValidation) return;
    const staticErrors = fieldErrorsFromServerValidation(serverValidation, staticPricingWireFields);
    for (const [wirePath, message] of Object.entries(staticErrors)) {
      const path = pricingFormPath(wirePath);
      if (path && message) form.setError(path, { type: "server", message });
    }
    for (const issue of serverValidation.issues) {
      const path = pricingFormPath(issue.field);
      if (path) form.setError(path, { type: "server", message: issue.message });
    }
  }, [form, serverValidation]);

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onClose();
        else onOpenChange(nextOpen);
      }}
    >
      <DialogContent className="max-h-[90vh] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {editingPricingTemplate
              ? dialogMessages.editTitle
              : dialogMessages.addTitle}
          </DialogTitle>
          <DialogDescription>{dialogMessages.description}</DialogDescription>
        </DialogHeader>
        {serverValidation ? (
          <OperatorCallout intent="danger" data-testid="pricing-form-server-error">
            <span className="whitespace-pre-line">{serverValidation.summary}</span>
          </OperatorCallout>
        ) : null}
        <Form {...form}>
          <form
            onSubmit={form.handleSubmit((values) => void onSave(values))}
            className="flex min-h-0 flex-col gap-5"
          >
            <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
              <div className="flex flex-col gap-5">
                <OperatorInsetPanel>
                  <p className="text-sm font-medium text-foreground">
                    {dialogMessages.detailsSectionTitle}
                  </p>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <FormField
                      control={form.control}
                      name="name"
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{dialogMessages.nameLabel}</FormLabel>
                          <FormControl>
                            <Input
                              autoComplete="off"
                              placeholder={dialogMessages.namePlaceholder}
                              {...field}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name="description"
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {dialogMessages.descriptionLabel}
                          </FormLabel>
                          <FormControl>
                            <Input
                              autoComplete="off"
                              placeholder={
                                dialogMessages.descriptionPlaceholder
                              }
                              {...field}
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                    <FormField
                      control={form.control}
                      name="template_kind"
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {dialogMessages.pricingKindLabel}
                          </FormLabel>
                          <Select
                            value={field.value}
                            onValueChange={field.onChange}
                          >
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent>
                              <SelectGroup>
                                <SelectItem value="standard">
                                  {dialogMessages.standardKindLabel}
                                </SelectItem>
                                <SelectItem value="tiered">
                                  {dialogMessages.tieredKindLabel}
                                </SelectItem>
                                <SelectItem value="peak_valley">
                                  {dialogMessages.peakValleyKindLabel}
                                </SelectItem>
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>
                </OperatorInsetPanel>
                {editingPricingTemplate ? (
                  impactLoading && !impact ? <OperatorInsetPanel><p className="text-xs text-muted-foreground">{dialogMessages.impactLoading}</p></OperatorInsetPanel>
                    : impactError && !impact ? <OperatorErrorState title={dialogMessages.impactUnavailable} description={impactError} action={<OperatorRetryButton onClick={onRetryImpact}>{messages.common.retry}</OperatorRetryButton>} />
                      : impact ? <OperatorInsetPanel><p className="text-sm font-medium text-foreground">{dialogMessages.impactTitle}</p><p className="mt-1 text-xs text-muted-foreground">{dialogMessages.impactSummary(impact.current_version, impact.next_version, impact.reference_count)}</p>{impact.references.length > 0 ? <ul className="mt-2 list-inside list-disc text-xs text-muted-foreground">{impact.references.map((reference) => <li key={reference.connection_id}>{reference.connection_name || dialogMessages.impactUnknownConnection} · {reference.model_id} · {reference.endpoint_name}</li>)}</ul> : <p className="mt-2 text-xs text-muted-foreground">{dialogMessages.impactNone}</p>}</OperatorInsetPanel>
                      : null
                ) : null}
                <div className="flex flex-col gap-4">
                  {templateKind !== "peak_valley" ? (
                    <>
                      <p className="text-sm text-muted-foreground">
                        {dialogMessages.rateUnitNote(
                          messages.costingUi.per1mTokens,
                        )}
                      </p>
                      <PricingCardFields
                        control={form.control}
                        path="base"
                        title={dialogMessages.baseRatesSectionTitle}
                      />
                      <PricingTierFields control={form.control} />
                    </>
                  ) : (
                    <PricingPeakValleyFields control={form.control} />
                  )}
                </div>
              </div>
            </DialogBody>
            <DialogFooter className="sm:justify-between">
              <Button type="button" variant="outline" onClick={onClose}>
                {dialogMessages.cancel}
              </Button>
              <Button type="submit" disabled={pricingTemplateSaving || Boolean(editingPricingTemplate && (impactLoading || impactError || !impact))}>
                {pricingTemplateSaving
                  ? dialogMessages.saving
                  : dialogMessages.save}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
