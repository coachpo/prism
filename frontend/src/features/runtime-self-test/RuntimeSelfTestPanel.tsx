import { useEffect } from "react"
import { Link } from "@tanstack/react-router"
import { ArrowRight, Loader2, RefreshCw, Send, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system"
import type { RuntimeSelfTestResult, SelfTestEntryContext, SelfTestRequestSpec } from "./selfTestTypes"
import { useRuntimeSelfTest } from "./useRuntimeSelfTest"

interface RuntimeSelfTestPanelProps {
  spec: SelfTestRequestSpec | null
  context: SelfTestEntryContext
  onBusyChange?: (busy: boolean) => void
  onClose?: () => void
}

export function RuntimeSelfTestPanel({ spec, context, onBusyChange, onClose }: RuntimeSelfTestPanelProps) {
  const { messages } = useLocale()
  const copy = messages.proxyApiKeys
  const test = useRuntimeSelfTest(spec, context)
  const { result, busy } = test
  useEffect(() => { onBusyChange?.(busy) }, [busy, onBusyChange])
  return (
    <div className="flex flex-col gap-3">
      <OperatorCallout intent="warning" description={copy.selfTestCostWarning} />
      {result ? <SelfTestResultPanel result={result} polling={test.runState === "polling"} /> : null}
      {busy ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
          <Loader2 className="size-4 animate-spin" />
          {test.runState === "running" ? copy.selfTestRunning : copy.selfTestReconciling}
        </div>
      ) : null}
      <div className="flex flex-wrap justify-end gap-2">
        {onClose ? <Button type="button" variant="outline" onClick={onClose}>{messages.common.close}</Button> : null}
        {result?.ingressRequestId && result.telemetryState !== "ready" && !busy ? (
          <Button type="button" variant="outline" onClick={() => void test.recheck()}>
            <RefreshCw data-icon="inline-start" />{copy.selfTestRecheck}
          </Button>
        ) : null}
        {busy ? (
          <Button type="button" variant="outline" onClick={test.cancel}>
            <X data-icon="inline-start" />{copy.selfTestCancel}
          </Button>
        ) : (
          <Button type="button" disabled={!spec} onClick={() => void test.run()}>
            <Send data-icon="inline-start" />{result ? copy.selfTestRunAgain : copy.selfTestRun}
          </Button>
        )}
      </div>
    </div>
  )
}

function SelfTestResultPanel({ result, polling }: { result: RuntimeSelfTestResult; polling: boolean }) {
  const { messages, formatNumber } = useLocale()
  const copy = messages.proxyApiKeys
  const succeeded = result.direct.state === "succeeded" && result.execution.state === "completed"
  const received = result.direct.state === "succeeded" && result.execution.state === "evidence_pending"
  const cancelled = result.direct.state === "cancelled"
  const interrupted = result.direct.state === "response_interrupted"
  const title = succeeded ? copy.selfTestDirectSucceeded : received ? copy.selfTestResponseReceived
    : cancelled ? copy.selfTestDirectCancelled : interrupted ? copy.selfTestResponseInterrupted : copy.selfTestDirectHttpError
  const description = succeeded ? copy.selfTestSuccessHelp
    : received ? copy.selfTestResponseReceivedHelp
    : cancelled ? copy.selfTestCancelHelp
    : interrupted ? copy.selfTestResponseInterruptedHelp
    : result.direct.state === "network_error" ? copy.selfTestDirectNetworkError
    : result.direct.statusCode === 401 || result.direct.statusCode === 403 ? copy.selfTestAccessRejected
    : result.direct.statusCode === 429 ? copy.selfTestLimited
    : copy.selfTestFailureHelp
  const recordReady = result.telemetryState === "ready"
  const cost = result.pricing.costMicros
  return (
    <div className="flex flex-col gap-3" aria-live="polite">
      <OperatorCallout intent={succeeded ? "success" : received ? "info" : cancelled || interrupted ? "warning" : "danger"} title={title} description={description} />
      {recordReady ? (
        <OperatorInsetPanel className="flex-col gap-2 text-sm">
          <p>{copy.selfTestLayerCredential}：{result.credential.attributionState === "identified" ? copy.selfTestCredentialIdentified : result.credential.attributionState === "none" ? copy.selfTestCredentialNone : copy.selfTestCredentialUnknown}</p>
          <p>{copy.selfTestLayerExecution}：{result.execution.state === "completed" ? copy.selfTestExecutionCompleted(result.execution.endpointLabelSnapshot ?? copy.selfTestExecutionUnknownTarget) : result.execution.state === "failed" ? copy.selfTestExecutionFailed : copy.selfTestEvidencePending}</p>
          <p>{copy.selfTestLayerPricing}：{result.pricing.state === "priced" && cost !== null && result.pricing.currency !== null && result.pricing.currency !== ""
            ? copy.selfTestPricingPriced(formatNumber(cost / 1_000_000, { maximumFractionDigits: 6 }), result.pricing.currency)
            : result.pricing.state === "unpriced" ? copy.selfTestPricingUnpriced
            : result.pricing.state === "ineligible" ? copy.selfTestPricingIneligible : copy.selfTestPricingMissing}</p>
        </OperatorInsetPanel>
      ) : !polling && !cancelled ? (
        <OperatorCallout intent="info" description={result.ingressRequestId ? result.telemetryState === "cancelled" ? copy.selfTestRecordCancelled : result.telemetryState === "unavailable" ? copy.selfTestRecordFailed : copy.selfTestTelemetryPending : copy.selfTestNoRecordLink} />
      ) : null}
      {result.routing.requestedModelId ? (
        <Button asChild variant="link" className="h-auto justify-start self-start p-0">
          <Link to="/observe/requests" search={recordReady && result.ingressRequestId ? { view: "ingress_chains", ingress_request_id: result.ingressRequestId } : { view: "ingress_chains", ingress_model_id: result.routing.requestedModelId, time_range: "1h" }}>
            {recordReady ? copy.selfTestViewInRequests : copy.selfTestViewRecentRequests}<ArrowRight data-icon="inline-end" />
          </Link>
        </Button>
      ) : null}
    </div>
  )
}
