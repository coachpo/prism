import { useEffect, useMemo, useState, type ReactNode } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useLocale } from "@/i18n/useLocale"
import type { Endpoint, EndpointVerifyResult } from "@/lib/types"
import { Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { EndpointVerificationAttempt } from "./useEndpointFormMutations"
import { endpointFormSchema, type EndpointFormValues } from "./endpointSchemas"
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system"

type EndpointSubmitResult = {
  endpoint: Endpoint
  verifyFamily?: string
  verifyResult?: EndpointVerifyResult | null
  verifyError?: string
  currentEndpoint?: Endpoint
}

interface EndpointDialogProps {
  open: boolean
  mode: "create" | "edit"
  onOpenChange: (open: boolean) => void
  onSubmit: (values: EndpointFormValues, verifyFamily?: string) => Promise<EndpointSubmitResult | null>
  onContinue?: (endpoint: Endpoint) => void
  onVerify?: (endpointId: number, family: string, revision: number) => Promise<EndpointVerificationAttempt>
  initialValues?: Endpoint
  serverError?: string | null
  fieldErrors?: Record<string, string> | null
  impactContent?: ReactNode
}

export function EndpointDialog({ open, mode, onOpenChange, onSubmit, onContinue, onVerify, initialValues, serverError, fieldErrors, impactContent }: EndpointDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const isEdit = mode === "edit"
  const schema = useMemo(() => endpointFormSchema.superRefine((values, context) => {
    if (!isEdit && !values.api_key.trim()) context.addIssue({ code: "custom", path: ["api_key"], message: copy.apiKeyRequired })
  }), [isEdit, copy.apiKeyRequired])
  const form = useForm<EndpointFormValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: "", base_url: "", api_key: "" },
  })
  const [family, setFamily] = useState("")
  const [familyError, setFamilyError] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [saved, setSaved] = useState<EndpointSubmitResult | null>(null)

  useEffect(() => {
    if (!open) return
    setSaved(null)
    setFamily("")
    setFamilyError(false)
    setVerifying(false)
    form.reset(initialValues ? { name: initialValues.name, base_url: initialValues.base_url, api_key: "" } : { name: "", base_url: "", api_key: "" })
    // A server conflict must not erase the draft; reset only for a new dialog session.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initialValues?.id])

  const baseUrlValue = form.watch("base_url") ?? ""
  const preview = useMemo(() => {
    const trimmed = baseUrlValue.trim()
    const normalized = trimmed.replace(/\/+$/, "")
    return trimmed && trimmed !== normalized ? normalized : null
  }, [baseUrlValue])
  const serviceRoot = /\/v1\/*$/i.test(baseUrlValue.trim()) ? baseUrlValue.trim().replace(/\/v1\/*$/i, "") : null
  const busy = form.formState.isSubmitting || verifying

  const submit = (withVerification: boolean) => form.handleSubmit(async (values) => {
    if (saved) return
    if (withVerification && !family) {
      setFamilyError(true)
      return
    }
    setFamilyError(false)
    setVerifying(withVerification)
    try {
      const result = await onSubmit(values, withVerification ? family : undefined)
      if (result) {
        setSaved(result)
        form.reset({ name: result.endpoint.name, base_url: result.endpoint.base_url, api_key: "" })
      }
    } finally {
      setVerifying(false)
    }
  })

  const retryVerification = async () => {
    if (!saved?.verifyFamily || !onVerify || busy) return
    setVerifying(true)
    try {
      const endpoint = saved.currentEndpoint ?? saved.endpoint
      const verification = await onVerify(endpoint.id, saved.verifyFamily, endpoint.config_revision)
      setSaved({ ...saved, endpoint: verification.currentEndpoint ?? endpoint, verifyResult: verification.result, verifyError: verification.errorMessage, currentEndpoint: verification.currentEndpoint })
    } finally {
      setVerifying(false)
    }
  }

  const requiredMark = <span className="text-failing"><span aria-hidden="true"> *</span><span className="sr-only">{messages.common.required}</span></span>

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen && !busy) onOpenChange(false) }}>
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>{saved ? copy.savedTitle : isEdit ? copy.editEndpointTitle : copy.newEndpointTitle}</DialogTitle>
          <DialogDescription>{saved ? isEdit ? copy.updatedDescription : copy.savedDescription : isEdit && initialValues ? copy.editDescription(initialValues.name) : copy.createDescription}</DialogDescription>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={submit(false)} className="flex min-h-0 flex-col gap-5" data-testid="endpoint-form">
            <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
              {serverError && !saved ? <OperatorCallout intent="danger" role="alert" title={copy.saveFailed} description={serverError} data-testid="endpoint-form-server-error" /> : null}
              {saved ? (
                <>
                  <OperatorInsetPanel>
                    <p className="font-medium">{saved.endpoint.name}</p>
                    <p className="break-all font-mono text-xs">{saved.endpoint.base_url}</p>
                  </OperatorInsetPanel>
                  {saved.verifyResult ? <VerifyResultCallout result={saved.verifyResult} /> : saved.verifyFamily ? (
                    <OperatorCallout intent="warning" role="alert" title={copy.verifyResultSavedButFailed} description={saved.verifyError ?? messages.endpointsData.verifyFailed} data-testid="verify-result-error" />
                  ) : <OperatorCallout intent="info" description={copy.notVerified} />}
                </>
              ) : (
                <>
                  {isEdit ? <><OperatorCallout intent="info" description={copy.editImpact} />{impactContent}</> : null}
                  <OperatorInsetPanel>
                    <FormField control={form.control} name="name" render={({ field }) => (
                      <FormItem>
                        <FormLabel>{copy.name}{requiredMark}</FormLabel>
                        <FormControl><Input autoComplete="off" aria-required="true" disabled={busy} placeholder={copy.namePlaceholder} {...field} /></FormControl>
                        {fieldErrors?.name ? <FormMessage>{copy.nameRejected}</FormMessage> : <FormMessage />}
                      </FormItem>
                    )} />
                    <FormField control={form.control} name="base_url" render={({ field }) => (
                      <FormItem>
                        <FormLabel>{copy.baseUrl}{requiredMark}</FormLabel>
                        <FormControl><Input autoComplete="off" aria-required="true" disabled={busy} placeholder={copy.baseUrlPlaceholder} {...field} /></FormControl>
                        <FormDescription>{copy.baseUrlHelp}</FormDescription>
                        {serviceRoot ? <OperatorCallout intent="warning" description={copy.baseUrlVersionWarning} action={<Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => form.setValue("base_url", serviceRoot, { shouldValidate: true, shouldDirty: true })}>{copy.useServiceRoot}</Button>} /> : null}
                        {preview ? <FormDescription className="text-xs" data-testid="base-url-preview">{copy.baseUrlPreview(preview)}</FormDescription> : null}
                        {fieldErrors?.base_url ? <FormMessage>{copy.addressRejected}</FormMessage> : <FormMessage />}
                      </FormItem>
                    )} />
                    <FormField control={form.control} name="api_key" render={({ field }) => (
                      <FormItem>
                        <FormLabel>{messages.proxyApiKeys.apiKey}{isEdit ? null : requiredMark}</FormLabel>
                        <FormControl><Input type="password" autoComplete="new-password" aria-required={isEdit ? undefined : true} disabled={busy} {...field} /></FormControl>
                        <FormDescription className="text-xs">{isEdit ? copy.keepStoredKey : copy.apiKeyRequired}</FormDescription>
                        {fieldErrors?.api_key ? <FormMessage>{copy.keyRejected}</FormMessage> : <FormMessage />}
                      </FormItem>
                    )} />
                  </OperatorInsetPanel>
                  <OperatorInsetPanel data-testid="verify-section">
                    <label htmlFor="endpoint-verify-family" className="text-sm font-medium">{copy.verifyFamily}</label>
                    <Select value={family} onValueChange={(value) => { setFamily(value); setFamilyError(false) }} disabled={busy}>
                      <SelectTrigger id="endpoint-verify-family" className="w-full" aria-label={copy.verifyFamily} aria-describedby="endpoint-verify-description" aria-invalid={familyError || undefined}>
                        <SelectValue placeholder={copy.verifyFamilyPlaceholder} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                        <SelectItem value="openai">{copy.verifyFamilyOpenAI}</SelectItem>
                        <SelectItem value="anthropic">{copy.verifyFamilyAnthropic}</SelectItem>
                        <SelectItem value="gemini">{copy.verifyFamilyGemini}</SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    {familyError ? <p role="alert" className="text-xs text-failing">{copy.verifyFamilyRequired}</p> : null}
                    <p id="endpoint-verify-description" className="text-xs text-muted-foreground">{copy.verifyDescription}</p>
                  </OperatorInsetPanel>
                </>
              )}
            </DialogBody>
            <DialogFooter>
              {saved ? (
                <>
                  <Button type="button" variant={isEdit ? "default" : "outline"} disabled={busy} onClick={() => onOpenChange(false)}>{copy.returnToServices}</Button>
                  {saved.verifyFamily && onVerify ? <Button type="button" variant="outline" disabled={busy} onClick={() => void retryVerification()}>{verifying ? copy.verifying : copy.verifyRetry}</Button> : null}
                  {!isEdit && onContinue ? <Button type="button" disabled={busy} onClick={() => { onOpenChange(false); onContinue(saved.currentEndpoint ?? saved.endpoint) }}>{messages.endpointsPage.attachToModel}</Button> : null}
                </>
              ) : (
                <>
                  <Button type="button" variant="outline" disabled={busy} onClick={() => onOpenChange(false)}>{copy.cancel}</Button>
                  <Button type="button" variant="outline" disabled={busy} aria-busy={busy && verifying} onClick={() => void submit(true)()}>{verifying ? <><Loader2 className="size-4 animate-spin" />{copy.verifying}</> : copy.saveAndVerify}</Button>
                  <Button type="submit" disabled={busy} aria-busy={busy && !verifying} data-testid="endpoint-save-only">{busy && !verifying ? <><Loader2 className="size-4 animate-spin" />{copy.saving}</> : copy.saveOnly}</Button>
                </>
              )}
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}

function VerifyResultCallout({ result }: { result: EndpointVerifyResult }) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const text = verifyResultText(copy, result)
  return <OperatorCallout intent={result.outcome === "verified" && result.is_current ? "success" : "warning"} role={result.outcome === "verified" ? "note" : "alert"} title={result.is_current ? text : copy.verifyResultStale} description={result.is_current ? undefined : text} data-testid="verify-result" />
}

function verifyResultText(copy: Record<string, unknown>, result: EndpointVerifyResult): string {
  switch (result.outcome) {
    case "verified": return copy.verifyResultVerified as string
    case "authentication_failed": return copy.verifyResultAuthenticationFailed as string
    case "probe_unsupported": return copy.verifyResultProbeUnsupported as string
    case "api_mismatch": return copy.verifyResultApiMismatch as string
    case "upstream_rejected": return (copy.verifyResultUpstreamRejected as (s: string) => string)(result.upstream_status != null ? String(result.upstream_status) : "?")
    case "upstream_unavailable": return (copy.verifyResultUpstreamUnavailable as (s: string) => string)(result.upstream_status != null ? String(result.upstream_status) : "?")
    case "unreachable": return copy.verifyResultUnreachable as string
    case "timeout": return copy.verifyResultTimeout as string
    default: return copy.verifyUnknownOutcome as string
  }
}
