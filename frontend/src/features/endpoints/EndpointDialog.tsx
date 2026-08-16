import { useEffect, useMemo, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { useLocale } from "@/i18n/useLocale"
import type { Endpoint, EndpointVerifyResult } from "@/lib/types"
import { AlertCircle, Loader2 } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/components/ui/form"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { endpointFormSchema, type EndpointFormValues } from "./endpointSchemas"
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system"
import { formatTimestampForLocale } from "@/i18n/format"

type VerifyDraft = {
  family: string
  revision: number
  result: EndpointVerifyResult | null
  errorMessage?: string
  phase: "pending" | "verifying" | "done" | "error"
}

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
  initialValues?: Endpoint
  serverError?: string | null
  fieldErrors?: Record<string, string> | null
}

export function EndpointDialog({
  open,
  mode,
  onOpenChange,
  onSubmit,
  initialValues,
  serverError,
  fieldErrors,
}: EndpointDialogProps) {
  const { messages, locale } = useLocale()
  const copy = messages.endpointsUi
  const isEdit = mode === "edit"
  const form = useForm<EndpointFormValues>({
    resolver: zodResolver(endpointFormSchema),
    defaultValues: { name: "", base_url: "", api_key: "" },
  })
  const [verifyDraft, setVerifyDraft] = useState<VerifyDraft | null>(null)
  const [family, setFamily] = useState<string>("")

  useEffect(() => {
    if (open) {
      setVerifyDraft(null)
      setFamily("")
      if (initialValues) {
        form.reset({ name: initialValues.name, base_url: initialValues.base_url, api_key: "" })
      } else {
        form.reset({ name: "", base_url: "", api_key: "" })
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, initialValues])

  // URL canonical preview without mutating keystrokes (§10.1).
  const baseUrlValue = form.watch("base_url") ?? ""
  const preview = useMemo(() => {
    const trimmed = baseUrlValue.trim()
    if (!trimmed) return null
    const withoutSlash = trimmed.replace(/\/+$/, "")
    if (withoutSlash !== trimmed) return withoutSlash
    return null
  }, [baseUrlValue])

  const handleSubmit = async (values: EndpointFormValues) => {
    if (!isEdit && !values.api_key.trim()) {
      form.setError("api_key", { message: copy.apiKeyRequired })
      return
    }
    const shouldVerify = verifyDraft?.phase === "pending"
    setVerifyDraft(shouldVerify ? { ...verifyDraft, phase: "verifying" } : null)
    const result = await onSubmit(values, shouldVerify ? family : undefined)
    if (!result) {
      if (shouldVerify && verifyDraft) {
        setVerifyDraft({ ...verifyDraft, phase: "pending" })
      }
      return
    }
    // Save succeeded. If save-and-verify was requested, verification already
    // ran against the committed response; show the dual result inline.
    if (shouldVerify) {
      if (result.verifyResult) {
        setVerifyDraft({ family: family || "openai", revision: result.endpoint.config_revision, result: result.verifyResult, phase: "done" })
      } else {
        if (result.currentEndpoint) {
          form.reset({ name: result.currentEndpoint.name, base_url: result.currentEndpoint.base_url, api_key: "" })
        }
        setVerifyDraft({
          family: family || "openai",
          revision: result.endpoint.config_revision,
          result: null,
          errorMessage: result.verifyError ?? messages.endpointsData.verifyFailed,
          phase: "error",
        })
      }
      return
    }
    onOpenChange(false)
  }

  const description = isEdit && initialValues
    ? copy.editDescription(initialValues.name)
    : copy.createDescription

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen && verifyDraft?.phase !== "verifying") onOpenChange(false) }}>
      <DialogContent className="max-h-[90vh] sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? copy.editEndpointTitle : copy.newEndpointTitle}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {serverError ? (
          <Alert variant="destructive" data-testid="endpoint-form-server-error">
            <AlertCircle />
            <AlertTitle>{isEdit ? messages.endpointsData.updateFailed : messages.endpointsData.createFailed}</AlertTitle>
            <AlertDescription className="whitespace-pre-line">{serverError}</AlertDescription>
          </Alert>
        ) : null}
        <Form {...form}>
          <form onSubmit={form.handleSubmit(handleSubmit)} className="flex min-h-0 flex-col gap-5" data-testid="endpoint-form">
            <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
              <OperatorInsetPanel>
                <FormField control={form.control} name="name" render={({ field }) => (
                  <FormItem>
                    <FormLabel>{copy.name}</FormLabel>
                    <FormControl><Input autoComplete="off" placeholder={copy.namePlaceholder} {...field} /></FormControl>
                    {fieldErrors?.name ? <FormMessage>{copy[fieldErrors.name as keyof typeof copy] as string ?? fieldErrors.name}</FormMessage> : <FormMessage />}
                  </FormItem>
                )} />
                <FormField control={form.control} name="base_url" render={({ field }) => (
                  <FormItem>
                    <FormLabel>{copy.baseUrl}</FormLabel>
                    <FormControl><Input autoComplete="off" placeholder={copy.baseUrlPlaceholder} {...field} /></FormControl>
                    {preview ? <FormDescription data-testid="base-url-preview">{copy.baseUrlPreview(preview)}</FormDescription> : null}
                    {fieldErrors?.base_url ? <FormMessage>{copy[fieldErrors.base_url as keyof typeof copy] as string ?? fieldErrors.base_url}</FormMessage> : <FormMessage />}
                  </FormItem>
                )} />
              </OperatorInsetPanel>
              <OperatorInsetPanel className="bg-panel">
                {isEdit && initialValues?.has_api_key ? (
                  <div className="mb-2 flex flex-col gap-1 text-xs text-muted-foreground" data-testid="current-key-summary">
                    <span className="font-mono text-foreground">{copy.fingerprintLabel(initialValues.api_key_fingerprint ?? "—")}</span>
                    <span>
                      {initialValues.api_key_updated_at
                        ? messages.endpoints.keyUpdatedAt(formatTimestampForLocale(locale, "UTC", initialValues.api_key_updated_at, { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }))
                        : copy.keyTimeUnknown}
                    </span>
                  </div>
                ) : null}
                <FormField control={form.control} name="api_key" render={({ field }) => (
                  <FormItem>
                    <FormLabel>{messages.proxyApiKeys.apiKey}</FormLabel>
                    <FormControl>
                      <Input type="password" autoComplete="new-password" placeholder="" {...field} />
                    </FormControl>
                    <FormDescription>{isEdit ? copy.keepStoredKey : copy.apiKeyRequired}</FormDescription>
                    <FormMessage />
                  </FormItem>
                )} />
              </OperatorInsetPanel>

              {verifyDraft?.phase === "pending" || verifyDraft?.phase === "verifying" ? (
                <div className="flex flex-col gap-3 rounded-lg border p-3" data-testid="verify-section">
                  <FormItem>
                    <FormLabel>{copy.verifyFamily}</FormLabel>
                    <Select value={family} onValueChange={setFamily} disabled={verifyDraft.phase === "verifying"}>
                      <SelectTrigger className="w-full" aria-label={copy.verifyFamily}>
                        <SelectValue placeholder={copy.verifyFamily} />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="openai">{copy.verifyFamilyOpenAI}</SelectItem>
                        <SelectItem value="anthropic">{copy.verifyFamilyAnthropic}</SelectItem>
                        <SelectItem value="gemini">{copy.verifyFamilyGemini}</SelectItem>
                      </SelectContent>
                    </Select>
                  </FormItem>
                  {verifyDraft.phase === "verifying" ? (
                    <p role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
                      <Loader2 className="size-3 animate-spin" />
                      {copy.verifying}
                    </p>
                  ) : null}
                </div>
              ) : null}

              {verifyDraft?.phase === "done" && verifyDraft.result ? (
                <VerifyResultCallout result={verifyDraft.result} />
              ) : null}
              {verifyDraft?.phase === "error" ? (
                <OperatorCallout
                  intent="warning"
                  role="alert"
                  title={copy.verifyResultSavedButFailed}
                  description={verifyDraft.errorMessage}
                  data-testid="verify-result-error"
                />
              ) : null}
            </DialogBody>
            <DialogFooter className="flex-col-reverse gap-2 sm:flex-row sm:justify-between">
              <div className="flex items-center gap-2">
                <Button type="button" variant="outline" disabled={form.formState.isSubmitting || verifyDraft?.phase === "verifying"} onClick={() => onOpenChange(false)}>
                  {messages.settingsDialogs.cancel}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={form.formState.isSubmitting || verifyDraft?.phase === "verifying" || verifyDraft?.phase === "pending"}
                  aria-busy={form.formState.isSubmitting}
                  onClick={() => setVerifyDraft({ family: family || "openai", revision: initialValues?.config_revision ?? 1, result: null, phase: "pending" })}
                >
                  {copy.saveAndVerify}
                </Button>
              </div>
              <Button type="submit" disabled={form.formState.isSubmitting || verifyDraft?.phase === "verifying" || (verifyDraft?.phase === "pending" && !family)} aria-busy={form.formState.isSubmitting} data-testid="endpoint-save-only">
                {form.formState.isSubmitting ? <span className="inline-flex items-center gap-2"><Loader2 className="size-4 animate-spin" />{copy.saving}</span> : copy.saveOnly}
              </Button>
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
  const intent = result.outcome === "verified" && result.is_current ? "success" : result.outcome === "verified" ? "warning" : "warning"
  return (
    <OperatorCallout
      intent={intent}
      role={result.outcome === "verified" ? "note" : "alert"}
      title={result.is_current ? text : copy.verifyResultStale}
      description={result.is_current ? undefined : text}
      data-testid="verify-result"
    />
  )
}

function verifyResultText(copy: Record<string, unknown>, result: EndpointVerifyResult): string {
  switch (result.outcome) {
    case "verified":
      return copy.verifyResultVerified as string
    case "authentication_failed":
      return copy.verifyResultAuthenticationFailed as string
    case "probe_unsupported":
      return copy.verifyResultProbeUnsupported as string
    case "api_mismatch":
      return copy.verifyResultApiMismatch as string
    case "upstream_rejected":
      return (copy.verifyResultUpstreamRejected as (s: string) => string)(result.upstream_status != null ? String(result.upstream_status) : "?")
    case "upstream_unavailable":
      return (copy.verifyResultUpstreamUnavailable as (s: string) => string)(result.upstream_status != null ? String(result.upstream_status) : "?")
    case "unreachable":
      return (copy.verifyResultUnreachable as (s: string) => string)(result.error_summary ?? "")
    case "timeout":
      return copy.verifyResultTimeout as string
    default:
      return copy.verifyUnknownOutcome as string
  }
}
