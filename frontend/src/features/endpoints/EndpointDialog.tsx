import { useEffect } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useLocale } from "@/i18n/useLocale"
import type { Endpoint } from "@/lib/types"
import { AlertCircle } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form"
import { ENDPOINT_FORM_DEFAULT_VALUES, endpointFormSchema, type EndpointFormValues } from "./endpointSchemas"
import { OperatorInsetPanel } from "@/shared/design-system"

interface EndpointDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (values: EndpointFormValues) => Promise<void>
  description: string
  initialValues?: Endpoint
  serverError?: string | null
  title: string
  submitLabel: string
}

export function EndpointDialog({
  open,
  onOpenChange,
  onSubmit,
  description,
  initialValues,
  serverError,
  title,
  submitLabel,
}: EndpointDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const form = useForm<EndpointFormValues>({
    resolver: zodResolver(endpointFormSchema),
    defaultValues: ENDPOINT_FORM_DEFAULT_VALUES,
  })

  useEffect(() => {
    if (open && initialValues) {
      form.reset({ name: initialValues.name, base_url: initialValues.base_url, api_key: "" })
    } else if (open) {
      form.reset(ENDPOINT_FORM_DEFAULT_VALUES)
    }
  }, [form, initialValues, open])

  const handleSubmit = async (values: EndpointFormValues) => {
    if (!initialValues && !values.api_key.trim()) {
      form.setError("api_key", { message: copy.apiKeyRequired })
      return
    }
    await onSubmit(values)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {serverError ? (
          <Alert variant="destructive" data-testid="endpoint-form-server-error">
            <AlertCircle />
            <AlertTitle>{initialValues ? messages.endpointsData.updateFailed : messages.endpointsData.createFailed}</AlertTitle>
            <AlertDescription className="whitespace-pre-line">{serverError}</AlertDescription>
          </Alert>
        ) : null}
        <Form {...form}>
          <form onSubmit={form.handleSubmit(handleSubmit)} className="flex min-h-0 flex-col gap-5">
            <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
              <OperatorInsetPanel>
                <FormField control={form.control} name="name" render={({ field }) => (
                  <FormItem>
                    <FormLabel>{copy.name}</FormLabel>
                    <FormControl><Input autoComplete="off" placeholder={copy.namePlaceholder} {...field} /></FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
                <FormField control={form.control} name="base_url" render={({ field }) => (
                  <FormItem>
                    <FormLabel>{copy.baseUrl}</FormLabel>
                    <FormControl><Input autoComplete="off" placeholder={copy.baseUrlPlaceholder} {...field} /></FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
              </OperatorInsetPanel>
              <OperatorInsetPanel className="bg-surface">
                <FormField control={form.control} name="api_key" render={({ field }) => (
                  <FormItem>
                    <FormLabel>{messages.proxyApiKeys.apiKey}</FormLabel>
                    <FormControl>
                      <Input type="password" autoComplete="off" placeholder={initialValues?.masked_api_key || messages.modelDetail.endpointApiKeyPlaceholder} {...field} />
                    </FormControl>
                    <FormDescription>{initialValues ? copy.keepStoredKey : copy.apiKeyRequired}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )} />
              </OperatorInsetPanel>
            </DialogBody>
            <DialogFooter className="sm:justify-between">
              <Button type="button" variant="outline" disabled={form.formState.isSubmitting} onClick={() => onOpenChange(false)}>
                {messages.settingsDialogs.cancel}
              </Button>
              <Button type="submit" disabled={form.formState.isSubmitting}>{submitLabel}</Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
