import { useEffect } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { AlertCircle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/i18n/useLocale"
import type { PricingTemplate } from "@/lib/types"
import { DEFAULT_PRICING_TEMPLATE_FORM, pricingTemplateFormSchema, pricingTemplateFormStateFromTemplate, type PriceField, type PricingTemplateFormValues } from "./pricingSchemas"

interface PricingTemplateDialogProps {
  editingPricingTemplate: PricingTemplate | null
  onClose: () => void
  onOpenChange: (open: boolean) => void
  onSave: (values: PricingTemplateFormValues) => Promise<void>
  open: boolean
  pricingTemplateSaving: boolean
  serverError?: string | null
}

type PricingFieldCardProps = {
  control: ReturnType<typeof useForm<PricingTemplateFormValues>>["control"]
  label: string
  name: PriceField
  placeholder: string
}

function PricingFieldCard({ control, label, name, placeholder }: PricingFieldCardProps) {
  return (
    <div className="rounded-lg border bg-background p-3">
      <FormField control={control} name={name} render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl><Input autoComplete="off" placeholder={placeholder} {...field} /></FormControl>
          <FormMessage />
        </FormItem>
      )} />
    </div>
  )
}

export function PricingTemplateDialog({ editingPricingTemplate, onClose, onOpenChange, onSave, open, pricingTemplateSaving, serverError }: PricingTemplateDialogProps) {
  const { messages } = useLocale()
  const dialogMessages = messages.pricingTemplateDialog
  const form = useForm<PricingTemplateFormValues>({
    resolver: zodResolver(pricingTemplateFormSchema),
    defaultValues: DEFAULT_PRICING_TEMPLATE_FORM,
  })

  useEffect(() => {
    if (!open) return
    form.reset(editingPricingTemplate ? pricingTemplateFormStateFromTemplate(editingPricingTemplate) : DEFAULT_PRICING_TEMPLATE_FORM)
  }, [editingPricingTemplate, form, open])

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose(); else onOpenChange(nextOpen) }}>
      <DialogContent className="max-h-[90vh] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{editingPricingTemplate ? dialogMessages.editTitle : dialogMessages.addTitle}</DialogTitle>
          <DialogDescription>{dialogMessages.description}</DialogDescription>
        </DialogHeader>
        {serverError ? (
          <Alert variant="destructive" data-testid="pricing-form-server-error">
            <AlertCircle />
            <AlertTitle>{messages.pricingTemplatesData.saveFailed}</AlertTitle>
            <AlertDescription className="whitespace-pre-line">{serverError}</AlertDescription>
          </Alert>
        ) : null}
        <Form {...form}>
          <form onSubmit={form.handleSubmit((values) => void onSave(values))} className="flex min-h-0 flex-col gap-5">
            <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
              <div className="flex flex-col gap-5">
                <section className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
                  <p className="text-sm font-medium text-foreground">{dialogMessages.detailsSectionTitle}</p>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <FormField control={form.control} name="name" render={({ field }) => (
                      <FormItem><FormLabel>{dialogMessages.nameLabel}</FormLabel><FormControl><Input autoComplete="off" placeholder={dialogMessages.namePlaceholder} {...field} /></FormControl><FormMessage /></FormItem>
                    )} />
                    <FormField control={form.control} name="pricing_currency_code" render={({ field }) => (
                      <FormItem><FormLabel>{dialogMessages.currencyCodeLabel}</FormLabel><FormControl><Input autoComplete="off" placeholder={dialogMessages.currencyCodePlaceholder} maxLength={3} {...field} onChange={(event) => field.onChange(event.target.value.toUpperCase())} /></FormControl><FormMessage /></FormItem>
                    )} />
                  </div>
                  <FormField control={form.control} name="description" render={({ field }) => (
                    <FormItem><FormLabel>{dialogMessages.descriptionLabel}</FormLabel><FormControl><Input autoComplete="off" placeholder={dialogMessages.descriptionPlaceholder} {...field} /></FormControl><FormMessage /></FormItem>
                  )} />
                </section>
                <section className="flex flex-col gap-4 rounded-lg border p-4">
                  <p className="text-sm font-medium text-foreground">{dialogMessages.baseRatesSectionTitle}</p>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <PricingFieldCard control={form.control} name="input_price" label={dialogMessages.inputPriceLabel} placeholder={dialogMessages.pricePlaceholder} />
                    <PricingFieldCard control={form.control} name="output_price" label={dialogMessages.outputPriceLabel} placeholder={dialogMessages.pricePlaceholder} />
                  </div>
                </section>
                <section className="flex flex-col gap-4 rounded-lg border bg-muted/15 p-4">
                  <div className="flex flex-col gap-1"><p className="text-sm font-medium text-foreground">{dialogMessages.componentRatesSectionTitle}</p><p className="text-sm text-muted-foreground">{dialogMessages.componentRatesSectionDescription}</p></div>
                  <div className="grid gap-3 md:grid-cols-3">
                    <PricingFieldCard control={form.control} name="cached_input_price" label={dialogMessages.cachedInputPriceLabel} placeholder={dialogMessages.pricePlaceholder} />
                    <PricingFieldCard control={form.control} name="cache_creation_price" label={dialogMessages.cacheCreationPriceLabel} placeholder={dialogMessages.pricePlaceholder} />
                    <PricingFieldCard control={form.control} name="reasoning_price" label={dialogMessages.reasoningPriceLabel} placeholder={dialogMessages.pricePlaceholder} />
                  </div>
                </section>
              </div>
            </DialogBody>
            <DialogFooter className="sm:justify-between"><Button type="button" variant="outline" onClick={onClose}>{dialogMessages.cancel}</Button><Button type="submit" disabled={pricingTemplateSaving}>{pricingTemplateSaving ? dialogMessages.saving : dialogMessages.save}</Button></DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
