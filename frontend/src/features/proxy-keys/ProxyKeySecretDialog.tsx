import { KeyRound, TriangleAlert } from "lucide-react"
import { useMemo, useRef, useState } from "react"
import { CopyButton } from "@/components/CopyButton"
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
import { Checkbox } from "@/components/ui/checkbox"
import { useLocale } from "@/i18n/useLocale"
import { OperatorInsetPanel } from "@/shared/design-system"
import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin"
import { buildSelfTestCurl, type CurlBuildOutput } from "@/features/runtime-self-test/curlBuilder"
import type { ModelConfigListItem } from "@/lib/types"
import { RuntimeSelfTestDialog } from "@/features/runtime-self-test/RuntimeSelfTestDialog"
import type { SelfTestRequestSpec } from "@/features/runtime-self-test/selfTestTypes"
import type { GeneratedProxyKeyState } from "./generatedSecretSession"
import { runtimeSelfTestModelCandidates } from "@/features/runtime-self-test/modelCandidates"

interface ProxyKeySecretDialogProps {
  state: GeneratedProxyKeyState
  models: ModelConfigListItem[]
  modelsError: boolean
  modelsLoading: boolean
  onRequestClose: (intent: "close" | "navigate") => void
  onKeepEditing: () => void
  onAbandonAndLeave: () => void
  /** Refetches only the model list. Must never reload the page: the raw key
   * lives in memory and a reload would destroy it. */
  onRetryModels: () => void
  onSetSavedAck: (acknowledged: boolean) => void
  onFinish: () => void
}

export function ProxyKeySecretDialog({
  state,
  models,
  modelsError,
  modelsLoading,
  onRequestClose,
  onKeepEditing,
  onAbandonAndLeave,
  onRetryModels,
  onSetSavedAck,
  onFinish,
}: ProxyKeySecretDialogProps) {
  const { messages } = useLocale()
  const copy = messages.proxyApiKeys
  const session = state.kind === "idle" ? null : state.session
  const open = state.kind !== "idle"
  const acknowledged = session?.savedAcknowledged ?? false
  // The selected model id initializes lazily to the first eligible model;
  // the dialog is remounted per session via key at the call site, so no
  // effect is needed to seed it.
  const [selectedModelId, setSelectedModelId] = useState<string>(() =>
    runtimeSelfTestModelCandidates(models)[0]?.model_id ?? "",
  )
  const [selectedOpenAIOperation, setSelectedOpenAIOperation] = useState<"responses" | "chat_completions">("responses")
  const [selfTestOpen, setSelfTestOpen] = useState(false)
  const [closeAttemptAnnounced, setCloseAttemptAnnounced] = useState(false)
  const announcementRef = useRef<HTMLDivElement | null>(null)

  const candidates = useMemo(() => runtimeSelfTestModelCandidates(models), [models])
  const selectedModel = useMemo(
    () => candidates.find((model) => model.model_id === selectedModelId) ?? candidates[0] ?? null,
    [candidates, selectedModelId],
  )

  const origin = useMemo(() => {
    try {
      return getEffectiveBackendOrigin().origin
    } catch {
      return null
    }
  }, [])

  const curl: CurlBuildOutput | null = useMemo(() => {
    if (!session || !selectedModel) {
      return null
    }
    try {
      return buildSelfTestCurl({
        apiFamily: selectedModel.api_family,
        openaiAcceptedFormat: selectedModel.openai_accepted_format,
        modelId: selectedModel.model_id,
        proxyKey: session.rawKey,
        openaiOperation:
          selectedModel.api_family === "openai" && selectedModel.openai_accepted_format === "dual_native"
            ? selectedOpenAIOperation
            : undefined,
      })
    } catch {
      return null
    }
  }, [selectedModel, selectedOpenAIOperation, session])

  const selfTestSpec: SelfTestRequestSpec | null = useMemo(() => {
    if (!session || !curl) {
      return null
    }
    return {
      url: curl.url,
      method: "POST",
      headers: curl.headers,
      body: curl.body,
    }
  }, [curl, session])

  const handleCloseAttempt = () => {
    if (!acknowledged) {
      setCloseAttemptAnnounced(true)
      requestAnimationFrame(() => {
        announcementRef.current?.focus()
      })
      onRequestClose("close")
      return
    }
    onFinish()
  }

  const handleEscapeAttempt = (event: KeyboardEvent) => {
    // Escape must not dismiss the dialog while the raw key is unacknowledged.
    event.preventDefault()
    handleCloseAttempt()
  }

  if (!session) {
    return null
  }

  return (
    <>
      <Dialog open={open}>
        <DialogContent
          size="lg"
          showCloseButton={false}
          onEscapeKeyDown={handleEscapeAttempt}
          onPointerDownOutside={(event: { preventDefault: () => void }) => {
            // Mask clicks must not dismiss the dialog while unacknowledged.
            event.preventDefault()
            handleCloseAttempt()
          }}
        >
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <KeyRound className="text-primary" />
              {copy.newSecret}
            </DialogTitle>
            <DialogDescription>{copy.newSecretDescription}</DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-4">
            <div
              ref={announcementRef}
              tabIndex={-1}
              role="status"
              aria-live="assertive"
              className="sr-only"
            >
              {closeAttemptAnnounced ? copy.newSecretCloseBlocked : ""}
            </div>

            <OperatorInsetPanel
              data-testid="proxy-key-secret"
              className="flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <p className="min-w-0 break-all font-mono text-sm text-foreground">{session.rawKey}</p>
              <CopyButton
                value={session.rawKey}
                label={copy.copyKey}
                targetLabel={copy.apiKey}
                variant="outline"
                className="shrink-0"
              />
            </OperatorInsetPanel>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="flex flex-col gap-1">
                <span className="text-xs font-medium text-muted-foreground">{copy.accessGatewayOrigin}</span>
                <OperatorInsetPanel className="flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                  <p className="min-w-0 break-all font-mono text-xs">{origin ?? copy.accessOriginUnavailable}</p>
                  {origin ? (
                    <CopyButton value={origin} label={copy.copyGatewayOrigin} targetLabel={copy.accessGatewayOrigin} variant="outline" className="shrink-0" />
                  ) : null}
                </OperatorInsetPanel>
              </div>
              <div className="flex flex-col gap-1">
                <span className="text-xs font-medium text-muted-foreground">{copy.accessFamilyBaseUrl}</span>
                <OperatorInsetPanel className="flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                  <p className="min-w-0 break-all font-mono text-xs">{curl?.familyBaseUrl ?? copy.accessBaseUnavailable}</p>
                  {curl ? (
                    <CopyButton value={curl.familyBaseUrl} label={copy.copyFamilyBaseUrl} targetLabel={copy.accessFamilyBaseUrl} variant="outline" className="shrink-0" />
                  ) : null}
                </OperatorInsetPanel>
              </div>
            </div>

            <div className="flex flex-col gap-1">
              <label htmlFor="proxy-key-model-select" className="text-xs font-medium text-muted-foreground">
                {copy.accessModel}
              </label>
              {modelsLoading ? (
                <p className="text-sm text-muted-foreground">{copy.accessModelsLoading}</p>
              ) : modelsError ? (
                <div className="flex flex-col gap-2">
                  <p className="text-sm text-destructive">{copy.accessModelsError}</p>
                  <Button type="button" variant="outline" size="sm" className="w-fit" onClick={onRetryModels}>
                    {copy.retry}
                  </Button>
                </div>
              ) : candidates.length === 0 ? (
                <p className="text-sm text-muted-foreground">{copy.accessNoEligibleModels}</p>
              ) : (
                <select
                  id="proxy-key-model-select"
                  className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm"
                  value={selectedModel?.model_id ?? ""}
                  onChange={(event) => setSelectedModelId(event.target.value)}
                >
                  {candidates.map((model) => (
                    <option key={model.model_id} value={model.model_id}>
                      {model.model_id}
                      {model.display_name ? ` · ${model.display_name}` : ""} · {model.api_family}
                      {model.openai_accepted_format ? ` · ${model.openai_accepted_format}` : ""}
                    </option>
                  ))}
                </select>
              )}
            </div>

            {selectedModel && selectedModel.api_family === "openai" && selectedModel.openai_accepted_format === "dual_native" ? (
              <div className="flex flex-col gap-1">
                <label htmlFor="proxy-key-operation-select" className="text-xs font-medium text-muted-foreground">
                  {copy.accessOperation}
                </label>
                <select
                  id="proxy-key-operation-select"
                  className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm sm:w-64"
                  value={selectedOpenAIOperation}
                  onChange={(event) => setSelectedOpenAIOperation(event.target.value as "responses" | "chat_completions")}
                >
                  <option value="responses">{copy.accessOperationResponses}</option>
                  <option value="chat_completions">{copy.accessOperationChatCompletions}</option>
                </select>
              </div>
            ) : null}

            {curl ? (
              <div className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium text-muted-foreground">{copy.accessCurl}</span>
                  <CopyButton value={curl.curl} label={copy.copyCurl} targetLabel={copy.accessCurl} variant="outline" size="sm" />
                </div>
                <pre className="overflow-x-auto rounded-md border border-border bg-inset p-3 text-[11px] leading-relaxed">
                  <code>{curl.curl}</code>
                </pre>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{copy.accessCurlUnavailable}</p>
            )}

            <OperatorInsetPanel className="flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-2">
                <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0 text-degraded" />
                <p className="text-xs text-muted-foreground">{copy.selfTestCostWarning}</p>
              </div>
              <Button type="button" variant="outline" size="sm" className="shrink-0" disabled={!selfTestSpec} onClick={() => setSelfTestOpen(true)}>
                {copy.selfTestRun}
              </Button>
            </OperatorInsetPanel>

            <div className="flex items-start gap-2 rounded-md border border-border bg-inset p-3">
              <Checkbox
                id="proxy-key-saved-ack"
                checked={acknowledged}
                onCheckedChange={(checked) => onSetSavedAck(checked === true)}
              />
              <label htmlFor="proxy-key-saved-ack" className="text-sm">
                {copy.newSecretAcknowledge}
              </label>
            </div>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={handleCloseAttempt} disabled={!acknowledged}>
              {messages.common.close}
            </Button>
            <Button type="button" onClick={handleCloseAttempt} disabled={!acknowledged}>
              {copy.newSecretFinish}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {state.kind === "closing_confirm" ? (
        <Dialog open>
          <DialogContent size="sm" showCloseButton={false}>
            <DialogHeader>
              <DialogTitle>{copy.newSecretCloseTitle}</DialogTitle>
              <DialogDescription>{copy.newSecretCloseDescription}</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onKeepEditing}>
                {copy.newSecretKeepEditing}
              </Button>
              <Button type="button" variant="destructive" onClick={onAbandonAndLeave}>
                {copy.newSecretAbandon}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}

      <RuntimeSelfTestDialog
        open={selfTestOpen}
        onOpenChange={setSelfTestOpen}
        spec={selfTestSpec}
        context={{
          source: "generated_secret",
          requestedModelId: selectedModel?.model_id ?? "",
          proxyKey: session.rawKey,
          explicitNoKey: false,
          expectedProxyApiKeyId: session.keyId,
        }}
      />
    </>
  )
}
