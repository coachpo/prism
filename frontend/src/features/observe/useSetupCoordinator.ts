import { useCallback, useEffect, useRef, useState, type FocusEvent, type RefObject } from "react"
import { useAuth } from "@/context/useAuth"
import {
  createInitialSetupState,
  fetchSetupReadiness,
  setupCollapseDecision,
  type SetupReadinessOptions,
  type SetupReadinessSnapshot,
} from "./setupCoordinator"

export interface SetupCoordinatorController {
  state: SetupReadinessSnapshot
  collapsed: boolean
  loading: boolean
  cardRef: RefObject<HTMLDivElement | null>
  refresh: () => void
  toggleDisclosure: () => void
  handleBlurCapture: (event: FocusEvent<HTMLDivElement>) => void
}

/**
 * 操作者手动展开首次配置卡的选择要跨导航保留：去端点页配一下再回来，
 * 卡片又自己收起来，等于每次都要重新展开一遍。与侧栏折叠用同一套写法。
 */
const SETUP_REOPENED_STORAGE_KEY = ["prism", "setup", "reopened"].join(".")

function readManuallyReopened(): boolean {
  if (typeof window === "undefined") return false
  try {
    return window.localStorage?.getItem(SETUP_REOPENED_STORAGE_KEY) === "true"
  } catch {
    return false
  }
}

function writeManuallyReopened(value: boolean): void {
  if (typeof window === "undefined") return
  try {
    if (value) window.localStorage?.setItem(SETUP_REOPENED_STORAGE_KEY, "true")
    else window.localStorage?.removeItem(SETUP_REOPENED_STORAGE_KEY)
  } catch {
    // 存储被禁用时不影响开合本身。
  }
}

function authModeForPhase(phase: { kind: string }): "enabled" | "disabled" | "unknown" {
  if (phase.kind === "AUTH_DISABLED") return "disabled"
  if (phase.kind === "AUTHENTICATED") return "enabled"
  return "unknown"
}

export function useSetupCoordinator(options: SetupReadinessOptions = {}): SetupCoordinatorController {
  const { phase } = useAuth()
  const [state, setState] = useState<SetupReadinessSnapshot>(createInitialSetupState)
  const [collapsed, setCollapsed] = useState(false)
  const sequenceRef = useRef(0)
  const cardRef = useRef<HTMLDivElement | null>(null)
  const previousReadyRef = useRef(false)
  const pendingCollapseRef = useRef(false)
  const manuallyReopenedRef = useRef(readManuallyReopened())
  const cycleKeyRef = useRef<string | null>(null)
  const optionsRef = useRef(options)
  useEffect(() => {
    optionsRef.current = options
  }, [options])

  const refresh = useCallback(() => {
    if (phase.kind !== "AUTHENTICATED" && phase.kind !== "AUTH_DISABLED") return
    const sequence = ++sequenceRef.current
    setState((previous) => ({ ...previous, phase: "loading", error: null }))
    void fetchSetupReadiness(authModeForPhase(phase), optionsRef.current).then((next) => {
      if (sequenceRef.current !== sequence) return
      const ready = next.phase === "fresh" && next.route_configured_count === 4
      const cycleKey = next.route_witness_generation ?? "fresh-without-generation"
      if (!ready) {
        previousReadyRef.current = false
        pendingCollapseRef.current = false
        manuallyReopenedRef.current = false
        writeManuallyReopened(false)
        cycleKeyRef.current = null
      } else {
        if (cycleKeyRef.current !== cycleKey) {
          cycleKeyRef.current = cycleKey
          manuallyReopenedRef.current = false
          writeManuallyReopened(false)
        writeManuallyReopened(false)
          previousReadyRef.current = false
        }
        if (!previousReadyRef.current && !manuallyReopenedRef.current) {
          previousReadyRef.current = true
          const active = typeof document === "undefined" ? null : document.activeElement
          const focusedInside = Boolean(active && cardRef.current?.contains(active))
          const decision = setupCollapseDecision({
            wasReady: false,
            isReady: true,
            focusedInside,
            manuallyReopened: manuallyReopenedRef.current,
          })
          if (decision === "wait-until-blur") pendingCollapseRef.current = true
          if (decision === "collapse") setCollapsed(true)
        }
      }
      setState(next)
    })
  }, [phase])

  useEffect(() => {
    let active = true
    queueMicrotask(() => {
      if (!active) return
      if (phase.kind === "AUTHENTICATED" || phase.kind === "AUTH_DISABLED") {
        refresh()
        return
      }
      ++sequenceRef.current
      setState(createInitialSetupState())
      setCollapsed(false)
      previousReadyRef.current = false
      pendingCollapseRef.current = false
      manuallyReopenedRef.current = false
      writeManuallyReopened(false)
      cycleKeyRef.current = null
    })
    return () => {
      active = false
    }
  }, [phase.kind, phase.session_epoch, refresh])

  const toggleDisclosure = useCallback(() => {
    setCollapsed((previous) => {
      const next = !previous
      if (!next) {
        manuallyReopenedRef.current = true
        writeManuallyReopened(true)
        pendingCollapseRef.current = false
      }
      return next
    })
  }, [])

  const handleBlurCapture = useCallback((event: FocusEvent<HTMLDivElement>) => {
    if (!pendingCollapseRef.current) return
    const nextTarget = event.relatedTarget
    if (nextTarget instanceof Node && event.currentTarget.contains(nextTarget)) return
    pendingCollapseRef.current = false
    setCollapsed(true)
  }, [])

  // Collapsible regardless of readiness: an operator who already knows the
  // instance is half-configured should still be able to get the banner out of
  // the way. The five facts stay reachable by expanding it again.
  const effectiveCollapsed = collapsed && state.phase === "fresh"

  return {
    state,
    collapsed: effectiveCollapsed,
    loading: state.phase === "loading",
    cardRef,
    refresh,
    toggleDisclosure,
    handleBlurCapture,
  }
}
