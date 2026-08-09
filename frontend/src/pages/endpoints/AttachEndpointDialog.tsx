import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"
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
import { getSharedModels } from "@/lib/referenceData"
import type { Endpoint, ModelConfigListItem, OpenAITextCapability } from "@/lib/types"
import { OperatorCallout } from "@/shared/design-system"
import { classifyOpenAICoverage } from "@/pages/model-detail/classifyOpenAICoverage"

interface AttachEndpointDialogProps {
  endpoint: Endpoint | null
  onOpenChange: (open: boolean) => void
  onSubmit: (endpoint: Endpoint, destinationModelConfigId: number, capability: string | null, targetName: string) => Promise<void>
}

// AttachEndpointDialog lets the operator attach an Endpoint to a destination
// model: the endpoint is preselected and locked, the Terminal Target is
// created through the ordinary owner-scoped create (a new private Connection).
export function AttachEndpointDialog({ endpoint, onOpenChange, onSubmit }: AttachEndpointDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const routingCopy = messages.routing
  const [models, setModels] = useState<ModelConfigListItem[]>([])
  const [destinationModelId, setDestinationModelId] = useState("")
  const [capability, setCapability] = useState<OpenAITextCapability | null>(null)
  const [targetName, setTargetName] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!endpoint) return
    let cancelled = false
    void getSharedModels(0).then((loaded) => {
      if (cancelled) return
      setModels(loaded)
      setDestinationModelId("")
      setCapability(null)
      setTargetName("")
      setError(null)
    })
    return () => {
      cancelled = true
    }
  }, [endpoint])

  const destinationModel = models.find((model) => model.id === Number.parseInt(destinationModelId, 10)) ?? null
  const preview = destinationModel && capability
    ? classifyOpenAICoverage(destinationModel.openai_accepted_format ?? "dual_native", capability)
    : null
  const capabilityDisabled = preview?.coverage === "none"
  const canSubmit = endpoint != null && destinationModel != null && !capabilityDisabled && !submitting

  const handleSubmit = async () => {
    if (!endpoint || !canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      await onSubmit(endpoint, Number.parseInt(destinationModelId, 10), capability, targetName)
      onOpenChange(false)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={endpoint != null} onOpenChange={(open) => { if (!open && !submitting) onOpenChange(false) }}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{endpoint ? copy.attachEndpoint(endpoint.name) : ""}</DialogTitle>
          <DialogDescription>{copy.attachEndpointDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {error ? <OperatorCallout intent="danger" description={error} /> : null}
          {endpoint ? (
            <div className="flex flex-col gap-1 rounded-md border border-outline-variant bg-surface-container-low px-3 py-2">
              <p className="text-sm font-medium">{endpoint.name}</p>
              <p className="break-all font-mono text-xs text-muted-foreground">{endpoint.base_url}</p>
            </div>
          ) : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="attach-destination-model">{copy.attachDestinationModel}</Label>
            <Select value={destinationModelId} onValueChange={setDestinationModelId}>
              <SelectTrigger id="attach-destination-model" className="w-full">
                <SelectValue placeholder={copy.attachDestinationModelPlaceholder} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {models.map((model) => (
                    <SelectItem key={model.id} value={String(model.id)}>
                      {model.display_name || model.model_id} ({model.api_family})
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          {destinationModel ? (
            <div className="flex flex-col gap-2">
              <Label htmlFor="attach-capability">{routingCopy.capabilityTitle}</Label>
              <Select
                value={capability ?? (destinationModel.openai_accepted_format ?? "dual_native")}
                onValueChange={(value) => setCapability(value as OpenAITextCapability)}
              >
                <SelectTrigger id="attach-capability" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {(["dual_native", "chat_completions_only", "responses_only"] as OpenAITextCapability[]).map((option) => {
                      const optionPreview = classifyOpenAICoverage(destinationModel.openai_accepted_format ?? "dual_native", option)
                      return (
                        <SelectItem key={option} value={option} disabled={optionPreview.coverage === "none"}>
                          {routingCopy[option === "dual_native" ? "capabilityDual" : option === "chat_completions_only" ? "capabilityChatOnly" : "capabilityResponsesOnly"]}
                          {" · "}
                          {routingCopy[optionPreview.coverage === "full" ? "coverageFull" : optionPreview.coverage === "partial" ? "coveragePartial" : "coverageNone"]}
                        </SelectItem>
                      )
                    })}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {preview && preview.coverage === "partial" ? (
                <p className="text-xs text-warning">{routingCopy.missingOperations(preview.unsupportedAcceptedOperations.join("、"))}</p>
              ) : null}
              {preview && preview.coverage === "none" ? (
                <p className="text-xs text-destructive">{routingCopy.noneCapabilityReason}</p>
              ) : null}
            </div>
          ) : null}
          <div className="flex flex-col gap-2">
            <Label htmlFor="attach-target-name">{messages.modelDetail.connectionNameOptional}</Label>
            <Input id="attach-target-name" value={targetName} onChange={(event) => setTargetName(event.target.value)} placeholder={messages.modelDetail.connectionDisplayNamePlaceholder} />
          </div>
          {destinationModel && !destinationModel.is_enabled ? (
            <p className="text-xs text-muted-foreground">{copy.attachDisabledModelHint}</p>
          ) : null}
          {preview && preview.coverage !== "none" ? (
            <p className="text-xs text-muted-foreground">{copy.attachFlowHint}</p>
          ) : null}
        </DialogBody>
        <DialogFooter className="sm:justify-between">
          <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button type="button" disabled={!canSubmit} aria-busy={submitting} onClick={() => void handleSubmit()}>
            {submitting ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
            {copy.attachAndCreate}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
