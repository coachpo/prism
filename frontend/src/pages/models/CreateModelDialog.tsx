import { useEffect, useMemo, useState } from "react"
import { Loader2, Plus, Sparkles, Trash2 } from "lucide-react"
import { ApiFamilySelect } from "@/components/ApiFamilySelect"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useLocale } from "@/i18n/useLocale"
import type {
  ApiFamily,
  Endpoint,
  LoadbalanceStrategy,
  OpenAIAcceptedFormat,
  OpenAITextCapability,
  PricingTemplate,
} from "@/lib/types"
import { getLoadbalanceStrategyTypeLabel } from "@/lib/loadbalanceRoutingPolicy"
import { OperatorCallout, OperatorInsetPanel, OperatorStatusBadge, OperatorSwitchField } from "@/shared/design-system"
import { getSharedEndpoints, getSharedPricingTemplates } from "@/lib/referenceData"
import { classifyOpenAICoverage } from "@/pages/model-detail/classifyOpenAICoverage"
import type { ModelCreatePayloadWithTarget } from "./createModelDialogPayload"

interface CreateModelDialogProps {
  isOpen: boolean
  onOpenChange: (open: boolean) => void
  revision?: number
  loadbalanceStrategies: LoadbalanceStrategy[]
  createLoadbalanceStrategyDefaultsPending?: boolean
  onCreateLoadbalanceStrategyDefaults?: () => Promise<void>
  onSubmit: (payload: ModelCreatePayloadWithTarget) => Promise<{ model: { id: number; access_targets: Array<{ connection_id: number | null }> }; configuration_warnings: unknown[] }>
}

const CANONICAL_DEFAULT_STRATEGY_NAME = "Default fill-first routing"

interface HeaderRow {
  id: string
  key: string
  value: string
}

let headerRowSequence = 0

function newHeaderRow(): HeaderRow {
  headerRowSequence += 1
  return { id: `create-target-header-${headerRowSequence}`, key: "", value: "" }
}

function getOpenAIAcceptedFormatLabel(format: OpenAIAcceptedFormat, copy: { openaiAcceptedFormatResponsesOnly: string; openaiAcceptedFormatChatCompletionsOnly: string; openaiAcceptedFormatDualNative: string }) {
  switch (format) {
    case "responses_only":
      return copy.openaiAcceptedFormatResponsesOnly
    case "chat_completions_only":
      return copy.openaiAcceptedFormatChatCompletionsOnly
    default:
      return copy.openaiAcceptedFormatDualNative
  }
}

// CreateModelDialog is the one-shot model creation flow: the operator creates
// the model and its first Terminal Target in a single atomic submit, or
// explicitly chooses "configure later" for a disabled model. Capability
// defaults follow the owner accepted format; None is not selectable.
export function CreateModelDialog({
  isOpen,
  onOpenChange,
  revision = 0,
  loadbalanceStrategies,
  createLoadbalanceStrategyDefaultsPending = false,
  onCreateLoadbalanceStrategyDefaults,
  onSubmit,
}: CreateModelDialogProps) {
  const { messages } = useLocale()
  const fieldCopy = messages.common
  const copy = messages.modelsUi
  const detailCopy = messages.modelDetail
  const routingCopy = messages.routing
  const strategyCopy = messages.loadbalanceStrategyCopy

  const [apiFamily, setApiFamily] = useState<ApiFamily>("openai")
  const [modelId, setModelId] = useState("")
  const [displayName, setDisplayName] = useState("")
  const [acceptedFormat, setAcceptedFormat] = useState<OpenAIAcceptedFormat>("dual_native")
  const [strategyId, setStrategyId] = useState<number | null>(null)
  const [strategyTouched, setStrategyTouched] = useState(false)
  const [mode, setMode] = useState<"ready" | "configure_later">("ready")
  const [endpointMode, setEndpointMode] = useState<"select" | "new">("select")
  const [selectedEndpointId, setSelectedEndpointId] = useState("")
  const [newEndpointName, setNewEndpointName] = useState("")
  const [newEndpointBaseUrl, setNewEndpointBaseUrl] = useState("")
  const [newEndpointApiKey, setNewEndpointApiKey] = useState("")
  const [targetName, setTargetName] = useState("")
  const [capability, setCapability] = useState<OpenAITextCapability | null>(null)
  const [capabilityTouched, setCapabilityTouched] = useState(false)
  const [pricingTemplateId, setPricingTemplateId] = useState("")
  const [qpsLimit, setQpsLimit] = useState("")
  const [maxInFlightNonStream, setMaxInFlightNonStream] = useState("")
  const [maxInFlightStream, setMaxInFlightStream] = useState("")
  const [headerRows, setHeaderRows] = useState<HeaderRow[]>([])
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [globalEndpoints, setGlobalEndpoints] = useState<Endpoint[]>([])
  const [pricingTemplates, setPricingTemplates] = useState<PricingTemplate[]>([])

  useEffect(() => {
    if (!isOpen) return
    let cancelled = false
    void Promise.all([getSharedEndpoints(revision), getSharedPricingTemplates(revision)]).then(([endpoints, templates]) => {
      if (cancelled) return
      setGlobalEndpoints(endpoints)
      setPricingTemplates(templates)
    })
    return () => {
      cancelled = true
    }
  }, [isOpen, revision])

  const canonicalStrategy = useMemo(
    () =>
      loadbalanceStrategies.find(
        (strategy) =>
          strategy.name === CANONICAL_DEFAULT_STRATEGY_NAME && strategy.legacy_strategy_type === "fill-first",
      ) ?? null,
    [loadbalanceStrategies],
  )
  const effectiveStrategyId = strategyId ?? canonicalStrategy?.id ?? null
  const strategyMissing = !strategyTouched && canonicalStrategy == null

  const derivedCapability: OpenAITextCapability =
    apiFamily === "openai" ? (acceptedFormat as OpenAITextCapability) : "dual_native"
  const effectiveCapability = capabilityTouched ? (capability ?? "dual_native") : derivedCapability
  const capabilityPreview = useMemo(() => {
    if (apiFamily !== "openai") return null
    return classifyOpenAICoverage(acceptedFormat, effectiveCapability)
  }, [acceptedFormat, apiFamily, effectiveCapability])

  const endpointReady = endpointMode === "select"
    ? selectedEndpointId !== ""
    : newEndpointName.trim() !== "" && newEndpointBaseUrl.trim() !== "" && newEndpointApiKey.trim() !== ""
  const readySubmitDisabled =
    submitting
    || strategyMissing
    || effectiveStrategyId == null
    || modelId.trim() === ""
    || (mode === "ready" && (!endpointReady || capabilityPreview?.coverage === "none"))

  const handleSubmit = async () => {
    if (readySubmitDisabled || effectiveStrategyId == null) return
    setSubmitting(true)
    setSubmitError(null)
    try {
      const payload: ModelCreatePayloadWithTarget = {
        api_family: apiFamily,
        model_id: modelId.trim(),
        display_name: displayName.trim() || modelId.trim(),
        loadbalance_strategy_id: effectiveStrategyId,
        is_enabled: mode === "configure_later" ? false : true,
      }
      if (apiFamily === "openai") {
        payload.openai_accepted_format = acceptedFormat
      }
      if (mode === "ready") {
        payload.initial_terminal_target = {
          name: targetName.trim() || undefined,
          openai_text_capability: apiFamily === "openai" ? effectiveCapability : undefined,
          pricing_template_id: pricingTemplateId !== "" ? Number.parseInt(pricingTemplateId, 10) : undefined,
          qps_limit: qpsLimit !== "" ? Number.parseInt(qpsLimit, 10) : undefined,
          max_in_flight_non_stream: maxInFlightNonStream !== "" ? Number.parseInt(maxInFlightNonStream, 10) : undefined,
          max_in_flight_stream: maxInFlightStream !== "" ? Number.parseInt(maxInFlightStream, 10) : undefined,
          custom_headers: Object.fromEntries(
            headerRows.filter((row) => row.key.trim() !== "").map((row) => [row.key.trim(), row.value]),
          ),
        }
        if (endpointMode === "select") {
          payload.initial_terminal_target.endpoint_id = Number.parseInt(selectedEndpointId, 10)
        } else {
          payload.initial_terminal_target.endpoint_create = {
            name: newEndpointName.trim(),
            base_url: newEndpointBaseUrl.trim(),
            api_key: newEndpointApiKey,
          }
        }
      }
      await onSubmit(payload)
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : String(error))
    } finally {
      setSubmitting(false)
    }
  }

  const resetState = () => {
    setApiFamily("openai")
    setModelId("")
    setDisplayName("")
    setAcceptedFormat("dual_native")
    setStrategyId(null)
    setStrategyTouched(false)
    setMode("ready")
    setEndpointMode("select")
    setSelectedEndpointId("")
    setNewEndpointName("")
    setNewEndpointBaseUrl("")
    setNewEndpointApiKey("")
    setTargetName("")
    setCapability(null)
    setCapabilityTouched(false)
    setPricingTemplateId("")
    setQpsLimit("")
    setMaxInFlightNonStream("")
    setMaxInFlightStream("")
    setHeaderRows([])
    setSubmitError(null)
  }

  return (
    <Dialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open && !submitting) {
          resetState()
          onOpenChange(false)
        }
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-hidden sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{messages.modelsPage.newModel}</DialogTitle>
          <DialogDescription>{copy.newModelDescription}</DialogDescription>
        </DialogHeader>
        <form
          className="flex min-h-0 flex-1 flex-col gap-5"
          autoComplete="off"
          noValidate
          onSubmit={(event) => {
            event.preventDefault()
            void handleSubmit()
          }}
        >
          <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
            {submitError ? <OperatorCallout intent="danger" description={submitError} /> : null}

            <OperatorInsetPanel>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex min-w-0 flex-col gap-2">
                  <Label>{fieldCopy.apiFamily}</Label>
                  <ApiFamilySelect
                    value={apiFamily}
                    onValueChange={(value) => {
                      setApiFamily(value as ApiFamily)
                      if (value !== "openai") {
                        setCapability(null)
                        setCapabilityTouched(false)
                      }
                    }}
                    showAll={false}
                    className="w-full"
                    placeholder={detailCopy.selectApiFamily}
                  />
                </div>
                {apiFamily === "openai" ? (
                  <div className="flex min-w-0 flex-col gap-2">
                    <Label htmlFor="create-model-accepted-format">{copy.openaiAcceptedFormat}</Label>
                    <Select
                      value={acceptedFormat}
                      onValueChange={(value) => setAcceptedFormat(value as OpenAIAcceptedFormat)}
                    >
                      <SelectTrigger id="create-model-accepted-format" className="h-auto w-full min-w-0 max-w-full items-start py-2 text-left whitespace-normal">
                        <SelectValue>{getOpenAIAcceptedFormatLabel(acceptedFormat, copy)}</SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {(["dual_native", "chat_completions_only", "responses_only"] as OpenAIAcceptedFormat[]).map((format) => (
                            <SelectItem key={format} value={format}>
                              {getOpenAIAcceptedFormatLabel(format, copy)}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </div>
                ) : null}
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="create-model-id">{copy.modelId}</Label>
                <Input
                  id="create-model-id"
                  autoComplete="off"
                  value={modelId}
                  onChange={(event) => setModelId(event.target.value)}
                  placeholder={copy.modelIdPlaceholder}
                  required
                />
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="create-model-display-name">{copy.displayNameOptional}</Label>
                <Input
                  id="create-model-display-name"
                  autoComplete="off"
                  value={displayName}
                  onChange={(event) => setDisplayName(event.target.value)}
                  placeholder={copy.optionalFriendlyName}
                />
              </div>
            </OperatorInsetPanel>

            <OperatorInsetPanel className="bg-surface">
              <div className="flex flex-col gap-1">
                <p className="text-sm font-medium text-foreground">{detailCopy.loadbalanceStrategy}</p>
                <p className="text-sm text-muted-foreground">{copy.routingTypeDescription}</p>
              </div>
              {loadbalanceStrategies.length === 0 ? (
                <div className="flex flex-col items-start gap-3">
                  <p className="text-sm text-muted-foreground">{detailCopy.noLoadbalanceStrategiesAvailable}</p>
                  {onCreateLoadbalanceStrategyDefaults ? (
                    <Button
                      type="button"
                      disabled={createLoadbalanceStrategyDefaultsPending}
                      onClick={() => { void onCreateLoadbalanceStrategyDefaults(); }}
                    >
                      {createLoadbalanceStrategyDefaultsPending ? (
                        <Loader2 data-icon="inline-start" className="animate-spin" />
                      ) : (
                        <Sparkles data-icon="inline-start" />
                      )}
                      {messages.loadbalanceStrategiesTable.createDefaults}
                    </Button>
                  ) : null}
                </div>
              ) : (
                <>
                  <Select
                    value={effectiveStrategyId != null ? String(effectiveStrategyId) : ""}
                    onValueChange={(value) => {
                      setStrategyId(Number.parseInt(value, 10))
                      setStrategyTouched(true)
                    }}
                  >
                    <SelectTrigger id="create-model-strategy" className="h-auto w-full min-w-0 max-w-full items-start py-2 text-left whitespace-normal">
                      <SelectValue placeholder={strategyMissing ? copy.selectStrategyRequired : detailCopy.selectStrategy}>
                        {effectiveStrategyId != null
                          ? (() => {
                              const strategy = loadbalanceStrategies.find((candidate) => candidate.id === effectiveStrategyId)
                              return strategy ? `${strategy.name} (${getLoadbalanceStrategyTypeLabel(strategy, strategyCopy)})` : null
                            })()
                          : null}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {loadbalanceStrategies.map((strategy) => (
                          <SelectItem key={strategy.id} value={String(strategy.id)}>
                            {strategy.name} ({getLoadbalanceStrategyTypeLabel(strategy, strategyCopy)})
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  {canonicalStrategy == null && !strategyTouched ? (
                    <p className="text-sm text-muted-foreground">{copy.canonicalDefaultStrategyMissing}</p>
                  ) : null}
                </>
              )}
            </OperatorInsetPanel>

            <OperatorInsetPanel className="bg-surface">
              <div className="flex flex-col gap-1">
                <p className="text-sm font-medium text-foreground">{copy.initialTerminalTarget}</p>
                <p className="text-sm text-muted-foreground">{copy.initialTerminalTargetDescription}</p>
              </div>

              <OperatorSwitchField
                label={copy.configureLater}
                description={copy.configureLaterDescription}
                checked={mode === "configure_later"}
                onCheckedChange={(checked) => setMode(checked ? "configure_later" : "ready")}
                className="border-outline-variant bg-surface-container-low"
              />

              {mode === "ready" ? (
                <div className="flex flex-col gap-4">
                  <div className="flex flex-col gap-2">
                    <Label>{detailCopy.endpointSource}</Label>
                    <div className="flex gap-2">
                      <Button
                        type="button"
                        variant={endpointMode === "select" ? "default" : "outline"}
                        size="sm"
                        onClick={() => setEndpointMode("select")}
                      >
                        {copy.existingEndpoint}
                      </Button>
                      <Button
                        type="button"
                        variant={endpointMode === "new" ? "default" : "outline"}
                        size="sm"
                        onClick={() => setEndpointMode("new")}
                      >
                        {detailCopy.createNew}
                      </Button>
                    </div>
                    {endpointMode === "select" ? (
                      <Select value={selectedEndpointId} onValueChange={setSelectedEndpointId}>
                        <SelectTrigger id="create-target-endpoint-select" className="w-full">
                          <SelectValue placeholder={detailCopy.selectEndpoint} />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            {globalEndpoints.map((endpoint) => (
                              <SelectItem key={endpoint.id} value={String(endpoint.id)}>
                                {endpoint.name} ({endpoint.base_url})
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    ) : (
                      <div className="grid gap-3 sm:grid-cols-3">
                        <div className="flex flex-col gap-2">
                          <Label htmlFor="create-target-endpoint-name">{detailCopy.endpointName}</Label>
                          <Input id="create-target-endpoint-name" value={newEndpointName} onChange={(event) => setNewEndpointName(event.target.value)} />
                        </div>
                        <div className="flex flex-col gap-2">
                          <Label htmlFor="create-target-endpoint-url">{detailCopy.endpointBaseUrl}</Label>
                          <Input id="create-target-endpoint-url" value={newEndpointBaseUrl} onChange={(event) => setNewEndpointBaseUrl(event.target.value)} placeholder={detailCopy.endpointBaseUrlPlaceholder} />
                        </div>
                        <div className="flex flex-col gap-2">
                          <Label htmlFor="create-target-endpoint-key">{detailCopy.endpointApiKey}</Label>
                          <Input id="create-target-endpoint-key" type="password" value={newEndpointApiKey} onChange={(event) => setNewEndpointApiKey(event.target.value)} placeholder={detailCopy.endpointApiKeyPlaceholder} />
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="grid gap-4 sm:grid-cols-2">
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="create-target-name">{detailCopy.connectionNameOptional}</Label>
                      <Input id="create-target-name" value={targetName} onChange={(event) => setTargetName(event.target.value)} placeholder={detailCopy.connectionDisplayNamePlaceholder} />
                    </div>
                    {apiFamily === "openai" ? (
                      <div className="flex flex-col gap-2">
                        <Label htmlFor="create-target-capability">{routingCopy.capabilityTitle}</Label>
                        <Select
                          value={effectiveCapability}
                          onValueChange={(value) => {
                            setCapability(value as OpenAITextCapability)
                            setCapabilityTouched(true)
                          }}
                        >
                          <SelectTrigger id="create-target-capability" className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectGroup>
                              {(["dual_native", "chat_completions_only", "responses_only"] as OpenAITextCapability[]).map((option) => {
                                const preview = classifyOpenAICoverage(acceptedFormat, option)
                                const disabled = preview.coverage === "none"
                                return (
                                  <SelectItem key={option} value={option} disabled={disabled}>
                                    <span className="flex flex-col gap-0.5">
                                      <span>
                                        {routingCopy[option === "dual_native" ? "capabilityDual" : option === "chat_completions_only" ? "capabilityChatOnly" : "capabilityResponsesOnly"]}
                                        {" · "}
                                        {routingCopy[preview.coverage === "full" ? "coverageFull" : preview.coverage === "partial" ? "coveragePartial" : "coverageNone"]}
                                      </span>
                                      {disabled ? <span className="text-xs text-muted-foreground">{routingCopy.noneCapabilityReason}</span> : null}
                                    </span>
                                  </SelectItem>
                                )
                              })}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        {capabilityPreview && capabilityPreview.coverage === "partial" ? (
                          <OperatorCallout
                            intent="warning"
                            description={routingCopy.missingOperations(capabilityPreview.unsupportedAcceptedOperations.join("、"))}
                          />
                        ) : null}
                        {capabilityPreview && capabilityPreview.coverage === "none" ? (
                          <OperatorCallout intent="danger" description={routingCopy.noneCapabilityReason} />
                        ) : null}
                      </div>
                    ) : null}
                  </div>

                  <div className="grid gap-4 sm:grid-cols-2">
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="create-target-pricing">{detailCopy.pricingTemplate}</Label>
                      <Select value={pricingTemplateId} onValueChange={setPricingTemplateId}>
                        <SelectTrigger id="create-target-pricing" className="w-full">
                          <SelectValue placeholder={routingCopy.noPricingTemplate} />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            {pricingTemplates.map((template) => (
                              <SelectItem key={template.id} value={String(template.id)}>{template.name}</SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="create-target-qps">{routingCopy.qpsLimitLabel}</Label>
                      <Input id="create-target-qps" inputMode="numeric" value={qpsLimit} onChange={(event) => setQpsLimit(event.target.value)} placeholder={routingCopy.leaveBlankForUnlimited} />
                    </div>
                  </div>

                  <div className="grid gap-4 sm:grid-cols-2">
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="create-target-inflight-non-stream">{detailCopy.maxInFlightNonStream}</Label>
                      <Input id="create-target-inflight-non-stream" inputMode="numeric" value={maxInFlightNonStream} onChange={(event) => setMaxInFlightNonStream(event.target.value)} placeholder={routingCopy.leaveBlankForUnlimited} />
                    </div>
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="create-target-inflight-stream">{detailCopy.maxInFlightStream}</Label>
                      <Input id="create-target-inflight-stream" inputMode="numeric" value={maxInFlightStream} onChange={(event) => setMaxInFlightStream(event.target.value)} placeholder={routingCopy.leaveBlankForUnlimited} />
                    </div>
                  </div>

                  <div className="flex flex-col gap-2">
                    <Label>{detailCopy.customHeaders}</Label>
                    <div className="flex flex-col gap-2">
                      {headerRows.map((row, index) => (
                        <div key={row.id} className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
                          <Input
                            aria-label={`${detailCopy.headerKey} ${index + 1}`}
                            value={row.key}
                            onChange={(event) => setHeaderRows((current) => current.map((candidate) => (candidate.id === row.id ? { ...candidate, key: event.target.value } : candidate)))}
                          />
                          <Input
                            aria-label={`${detailCopy.headerValue} ${index + 1}`}
                            value={row.value}
                            onChange={(event) => setHeaderRows((current) => current.map((candidate) => (candidate.id === row.id ? { ...candidate, value: event.target.value } : candidate)))}
                          />
                          <Button
                            type="button"
                            variant="outline"
                            size="icon-sm"
                            aria-label={`${detailCopy.removeHeader} ${index + 1}`}
                            onClick={() => setHeaderRows((current) => current.filter((candidate) => candidate.id !== row.id))}
                          >
                            <Trash2 />
                          </Button>
                        </div>
                      ))}
                      <Button type="button" variant="outline" size="sm" onClick={() => setHeaderRows((current) => [...current, newHeaderRow()])}>
                        <Plus data-icon="inline-start" />
                        {detailCopy.addHeader}
                      </Button>
                    </div>
                  </div>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{copy.configureLaterSavedAsDisabled}</p>
              )}
            </OperatorInsetPanel>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
              {messages.settingsDialogs.cancel}
            </Button>
            <div className="flex items-center gap-3">
              {mode === "ready" && capabilityPreview?.coverage === "none" ? (
                <OperatorStatusBadge intent="danger" label={routingCopy.coverageNone} preserveLabel />
              ) : null}
              <Button type="submit" disabled={readySubmitDisabled} aria-busy={submitting}>
                {submitting ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
                {mode === "configure_later" ? copy.createDisabledModel : copy.createAndEnable}
              </Button>
            </div>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
