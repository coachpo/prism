import { useCallback, useEffect, useRef, useState } from "react"
import { api } from "@/lib/api"
import { getStaticMessages } from "@/i18n/staticMessages"
import type { RoutingDiagnosticsResult } from "@/lib/types"

interface UseModelRoutingDiagnosticsInput {
  modelConfigId: number | undefined
  revision: number
  enabled?: boolean
}

// Loads the authoritative static routing diagnostics for the model detail
// page. Diagnostics are auxiliary read-only data: failures never block static
// configuration editing, and every mutation success triggers refresh.
export function useModelRoutingDiagnostics({
  modelConfigId,
  revision,
  enabled = true,
}: UseModelRoutingDiagnosticsInput) {
  const [diagnostics, setDiagnostics] = useState<RoutingDiagnosticsResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const requestIdRef = useRef(0)
  const resetKey = `${enabled ? modelConfigId ?? "none" : "disabled"}:${revision}`
  const resetKeyRef = useRef(resetKey)

  const refreshDiagnostics = useCallback(async () => {
    if (!enabled || typeof modelConfigId !== "number" || !Number.isFinite(modelConfigId)) {
      requestIdRef.current += 1
      setDiagnostics(null)
      setError(null)
      return
    }
    const resolvedModelConfigId = modelConfigId
    const requestId = ++requestIdRef.current
    setLoading(true)
    try {
      const data = await api.models.routingDiagnostics.get(resolvedModelConfigId)
      if (requestId !== requestIdRef.current) {
        return
      }
      setDiagnostics(data)
      setError(null)
    } catch (reason) {
      if (requestId !== requestIdRef.current) {
        return
      }
      setError(reason instanceof Error ? reason.message : getStaticMessages().modelDetailData.loadDiagnosticsFailed)
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false)
      }
    }
  }, [enabled, modelConfigId])

  useEffect(() => {
    if (resetKeyRef.current !== resetKey) {
      resetKeyRef.current = resetKey
      requestIdRef.current += 1
      setDiagnostics(null)
      setError(null)
    }
    void refreshDiagnostics()
  }, [refreshDiagnostics, resetKey])

  return {
    diagnostics,
    diagnosticsLoading: loading,
    diagnosticsError: error,
    refreshDiagnostics,
  }
}
