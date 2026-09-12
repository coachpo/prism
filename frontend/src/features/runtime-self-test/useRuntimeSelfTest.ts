import { useEffect, useRef, useState } from "react"
import { buildSelfTestResult, reconcileSelfTestTelemetry, runRuntimeSelfTestDirect } from "./selfTestRunner"
import type { RuntimeSelfTestResult, SelfTestEntryContext, SelfTestRequestSpec } from "./selfTestTypes"

type RunState = "idle" | "running" | "polling" | "done"

export function useRuntimeSelfTest(spec: SelfTestRequestSpec | null, context: SelfTestEntryContext) {
  const [runState, setRunState] = useState<RunState>("idle")
  const [result, setResult] = useState<RuntimeSelfTestResult | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  useEffect(() => () => abortRef.current?.abort(), [])

  async function readRecord(direct: RuntimeSelfTestResult, controller: AbortController) {
    if (!direct.ingressRequestId) return
    setRunState("polling")
    try {
      const telemetry = await reconcileSelfTestTelemetry(direct.ingressRequestId, controller.signal)
      if (!controller.signal.aborted) {
        setResult(buildSelfTestResult({ ...direct.direct, ingressRequestId: direct.ingressRequestId }, context, telemetry))
      }
    } catch {
      if (!controller.signal.aborted) setResult({ ...direct, telemetryState: "unavailable" })
    }
  }

  async function run() {
    if (!spec) return
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setRunState("running")
    setResult(null)
    try {
      const direct = await runRuntimeSelfTestDirect(spec, context, controller.signal)
      if (controller.signal.aborted) return
      const initial = buildSelfTestResult(direct, context, null)
      setResult(initial)
      await readRecord(initial, controller)
    } catch {
      if (!controller.signal.aborted) {
        setResult(buildSelfTestResult({ ingressRequestId: null, statusCode: null, state: "network_error" }, context, null))
      }
    } finally {
      if (!controller.signal.aborted) setRunState("done")
    }
  }

  async function recheck() {
    if (!result?.ingressRequestId) return
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    await readRecord(result, controller)
    if (!controller.signal.aborted) setRunState("done")
  }

  function cancel() {
    abortRef.current?.abort()
    setResult((previous) => previous
      ? { ...previous, telemetryState: "cancelled" }
      : buildSelfTestResult({ ingressRequestId: null, statusCode: null, state: "cancelled" }, context, null))
    setRunState("done")
  }

  return { runState, result, run, recheck, cancel, busy: runState === "running" || runState === "polling" }
}
