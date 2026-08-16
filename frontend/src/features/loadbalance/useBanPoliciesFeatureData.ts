import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { api, ApiError } from "@/lib/api"
import { getSharedLoadbalanceStrategies, setSharedLoadbalanceStrategies } from "@/lib/referenceData"
import type { LoadbalanceStrategy, StrategyImpactListResponse } from "@/lib/types"
import { banPolicyFormValuesFromStrategy, buildBanPolicyPayload, buildBanPolicyUpdatePayload, DEFAULT_BAN_POLICY_FORM_VALUES, getAttachedModelCountFromDeleteDetail, type BanPolicyFormValues } from "./banPolicySchemas"

// Trusted fragment state (SPEC §8.1): every resource owns its own
// idle|loading|ready|empty|error phase with last-good stale support.
export type FragmentPhase = "idle" | "loading" | "ready" | "empty" | "error"

export interface FragmentState<T> {
  phase: FragmentPhase
  data: T | null
  stale: boolean
  lastSuccessfulAt: string | null
  error: string | null
  semanticQueryKey: string
}

const STRATEGIES_QUERY_KEY = "loadbalance:strategies:v1"

function initialFragment<T>(key: string): FragmentState<T> {
  return { phase: "idle", data: null, stale: false, lastSuccessfulAt: null, error: null, semanticQueryKey: key }
}

export interface StrategyImpactState {
  fragment: FragmentState<StrategyImpactListResponse>
  expanded: boolean
}

export interface SetDefaultState {
  pending: boolean
  error: string | null
  conflictCurrentDefaultId: number | null
}

export interface StrategyMutationError {
  message: string
  attachedModelCount: number | null
  defaultStrategyId: number | null
}

export function useBanPoliciesFeatureData(revision: number) {
  const [strategiesFragment, setStrategiesFragment] = useState<FragmentState<LoadbalanceStrategy[]>>(() => initialFragment<LoadbalanceStrategy[]>(STRATEGIES_QUERY_KEY))
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingStrategy, setEditingStrategy] = useState<LoadbalanceStrategy | null>(null)
  const [formValues, setFormValues] = useState<BanPolicyFormValues>(DEFAULT_BAN_POLICY_FORM_VALUES)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [preparingEditId, setPreparingEditId] = useState<number | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<LoadbalanceStrategy | null>(null)
  const [displayDelete, setDisplayDelete] = useState<LoadbalanceStrategy | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<StrategyMutationError | null>(null)
  const [defaultsCreating, setDefaultsCreating] = useState(false)
  const [defaultsError, setDefaultsError] = useState<string | null>(null)
  const [setDefaultState, setSetDefaultState] = useState<Record<number, SetDefaultState>>({})
  const [impactStates, setImpactStates] = useState<Record<number, StrategyImpactState>>({})
  const requestGeneration = useRef(0)

  const commitStrategies = useCallback((updater: (current: LoadbalanceStrategy[]) => LoadbalanceStrategy[]) => {
    setStrategiesFragment((current) => {
      const next = sortStrategies(updater(current.data ?? []))
      setSharedLoadbalanceStrategies(revision, next)
      return { ...current, phase: next.length === 0 ? "empty" : "ready", data: next, stale: false, error: null }
    })
  }, [revision])

  const refreshStrategies = useCallback(async () => {
    const generation = ++requestGeneration.current
    setStrategiesFragment((current) => ({ ...current, phase: "loading", stale: current.data !== null, error: null }))
    try {
      const next = sortStrategies(await getSharedLoadbalanceStrategies(revision))
      if (generation !== requestGeneration.current) return
      setStrategiesFragment({ phase: next.length === 0 ? "empty" : "ready", data: next, stale: false, lastSuccessfulAt: new Date().toISOString(), error: null, semanticQueryKey: STRATEGIES_QUERY_KEY })
    } catch (error) {
      if (generation !== requestGeneration.current) return
      setStrategiesFragment((current) => ({
        ...current,
        phase: "error",
        stale: current.data !== null,
        error: error instanceof Error ? error.message : "Failed to load routing strategies",
        lastSuccessfulAt: current.lastSuccessfulAt,
      }))
    }
  }, [revision])

  useEffect(() => { void refreshStrategies() }, [refreshStrategies])

  const openCreate = () => {
    setEditingStrategy(null)
    setFormValues(DEFAULT_BAN_POLICY_FORM_VALUES)
    setSaveError(null)
    setDialogOpen(true)
  }

  const openEdit = async (strategy: LoadbalanceStrategy) => {
    setPreparingEditId(strategy.id)
    try {
      const loaded = await api.loadbalanceStrategies.get(strategy.id)
      setEditingStrategy(loaded)
      setFormValues(banPolicyFormValuesFromStrategy(loaded))
      setSaveError(null)
      setDialogOpen(true)
    } catch (error) {
      setStrategiesFragment((current) => ({
        ...current,
        phase: "error",
        stale: current.data !== null,
        error: error instanceof Error ? error.message : "Failed to load routing strategy",
      }))
    } finally {
      setPreparingEditId(null)
    }
  }

  const save = async (values: BanPolicyFormValues) => {
    setSaving(true)
    setSaveError(null)
    try {
      if (editingStrategy) {
        const updated = await api.loadbalanceStrategies.update(editingStrategy.id, buildBanPolicyUpdatePayload(values))
        commitStrategies((current) => current.map((strategy) => strategy.id === editingStrategy.id ? updated : strategy))
      } else {
        const created = await api.loadbalanceStrategies.create(buildBanPolicyPayload(values))
        commitStrategies((current) => [created, ...current])
      }
      setDialogOpen(false)
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Failed to save routing strategy")
    } finally {
      setSaving(false)
    }
  }

  const createDefaults = async () => {
    setDefaultsCreating(true)
    setDefaultsError(null)
    try {
      const response = await api.loadbalanceStrategies.createDefaults()
      const refreshed = await getSharedLoadbalanceStrategies(revision)
      setStrategiesFragment({ phase: refreshed.length === 0 ? "empty" : "ready", data: sortStrategies(refreshed), stale: false, lastSuccessfulAt: new Date().toISOString(), error: null, semanticQueryKey: STRATEGIES_QUERY_KEY })
      setSharedLoadbalanceStrategies(revision, refreshed)
      if (response.default_changed) {
        setStrategiesFragment((current) => ({ ...current, error: null }))
      }
    } catch (error) {
      setDefaultsError(error instanceof Error ? error.message : "Failed to complete built-in strategies")
    } finally {
      setDefaultsCreating(false)
    }
  }

  const setDefault = async (strategyId: number) => {
    const currentDefault = strategiesFragment.data?.find((strategy) => strategy.is_default)?.id ?? null
    setSetDefaultState((current) => ({ ...current, [strategyId]: { pending: true, error: null, conflictCurrentDefaultId: null } }))
    try {
      const response = await api.loadbalanceStrategies.setDefault(strategyId, currentDefault)
      commitStrategies((current) => current.map((strategy) => ({
        ...strategy,
        is_default: strategy.id === response.default_strategy_id,
      })))
      setSetDefaultState((current) => ({ ...current, [strategyId]: { pending: false, error: null, conflictCurrentDefaultId: null } }))
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const detail = error.detail as { current_default_strategy_id?: number | null } | null
        const conflictCurrentDefaultId = detail?.current_default_strategy_id ?? null
        setSetDefaultState((current) => ({ ...current, [strategyId]: { pending: false, error: error instanceof Error ? error.message : "Default changed", conflictCurrentDefaultId } }))
        void refreshStrategies()
      } else {
        setSetDefaultState((current) => ({ ...current, [strategyId]: { pending: false, error: error instanceof Error ? error.message : "Failed to set default", conflictCurrentDefaultId: null } }))
      }
    }
  }

  const clearSetDefaultError = (strategyId: number) => {
    setSetDefaultState((current) => ({ ...current, [strategyId]: { pending: false, error: null, conflictCurrentDefaultId: null } }))
  }

  const openDelete = (strategy: LoadbalanceStrategy) => {
    setDeleteConfirm(strategy)
    setDisplayDelete(strategy)
    setDeleteError(null)
  }

  const closeDelete = () => {
    setDeleteConfirm(null)
    setDisplayDelete(null)
    setDeleteError(null)
  }

  const deleteStrategy = async () => {
    if (!deleteConfirm) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await api.loadbalanceStrategies.delete(deleteConfirm.id)
      commitStrategies((current) => current.filter((strategy) => strategy.id !== deleteConfirm.id))
      closeDelete()
    } catch (error) {
      const mutationError: StrategyMutationError = { message: error instanceof Error ? error.message : "Failed to delete routing strategy", attachedModelCount: null, defaultStrategyId: null }
      if (error instanceof ApiError && error.status === 409) {
        const attachedModelCount = getAttachedModelCountFromDeleteDetail(error.detail)
        mutationError.attachedModelCount = attachedModelCount
        const detail = error.detail as { default_strategy_id?: number | null } | null
        mutationError.defaultStrategyId = detail?.default_strategy_id ?? null
        if (attachedModelCount !== null) {
          const blocked = { ...deleteConfirm, attached_model_count: attachedModelCount }
          setDeleteConfirm(blocked)
          setDisplayDelete(blocked)
        }
      }
      setDeleteError(mutationError)
    } finally {
      setDeleting(false)
    }
  }

  const loadImpactPage = async (strategyId: number, cursor?: string) => {
    const queryKey = `loadbalance:impact:${strategyId}`
    setImpactStates((states) => {
      const previous = states[strategyId]?.fragment ?? initialFragment<StrategyImpactListResponse>(queryKey)
      return {
        ...states,
        [strategyId]: {
          expanded: true,
          fragment: {
            ...previous,
            phase: "loading",
            stale: previous.data !== null,
            error: null,
          },
        },
      }
    })
    try {
      const response = await api.loadbalanceStrategies.impact(strategyId, cursor ? { limit: 25, cursor } : { limit: 25 })
      setImpactStates((states) => {
        const existing = cursor ? states[strategyId]?.fragment.data : null
        const data = existing ? { ...response, items: [...existing.items, ...response.items] } : response
        return {
          ...states,
          [strategyId]: {
            expanded: true,
            fragment: {
              phase: data.items.length === 0 ? "empty" : "ready",
              data,
              stale: false,
              lastSuccessfulAt: new Date().toISOString(),
              error: null,
              semanticQueryKey: queryKey,
            },
          },
        }
      })
    } catch (error) {
      setImpactStates((states) => {
        const previous = states[strategyId]?.fragment ?? initialFragment<StrategyImpactListResponse>(queryKey)
        return {
          ...states,
          [strategyId]: {
            expanded: true,
            fragment: {
              ...previous,
              phase: "error",
              stale: previous.data !== null,
              error: error instanceof Error ? error.message : "Failed to load attached models",
            },
          },
        }
      })
    }
  }

  const toggleImpact = async (strategyId: number) => {
    const current = impactStates[strategyId]
    if (current?.expanded && current.fragment.phase !== "error") {
      setImpactStates((states) => ({ ...states, [strategyId]: { ...states[strategyId], expanded: false } }))
      return
    }
    const cursor = current?.fragment.phase === "error" ? current.fragment.data?.next_cursor ?? undefined : undefined
    await loadImpactPage(strategyId, cursor)
  }

  const retryImpact = async (strategyId: number) => {
    const current = impactStates[strategyId]
    await loadImpactPage(strategyId, current?.fragment.data?.next_cursor ?? undefined)
  }

  const loadMoreImpact = async (strategyId: number) => {
    const current = impactStates[strategyId]
    if (!current?.fragment.data?.next_cursor || (current.fragment.phase !== "ready" && current.fragment.phase !== "error")) return
    await loadImpactPage(strategyId, current.fragment.data.next_cursor)
  }

  const defaultsCompleteness = useMemo(() => {
    const strategies = strategiesFragment.data ?? []
    const canonicalNames = ["Default single routing", "Default fill-first routing", "Default round-robin routing"]
    const existing = canonicalNames.filter((name) => strategies.some((strategy) => strategy.name === name))
    const missing = canonicalNames.filter((name) => !existing.includes(name))
    return { complete: missing.length === 0, missing, existingCount: existing.length }
  }, [strategiesFragment.data])

  return {
    strategiesFragment,
    dialogOpen,
    editingStrategy,
    formValues,
    saving,
    saveError,
    defaultsCreating,
    defaultsError,
    defaultsCompleteness,
    preparingEditId,
    deleteConfirm,
    displayDelete,
    deleting,
    deleteError,
    setDefaultState,
    impactStates,
    refreshStrategies,
    openCreate,
    openEdit,
    save,
    createDefaults,
    setDefault,
    clearSetDefaultError,
    openDelete,
    closeDelete,
    deleteStrategy,
    toggleImpact,
    retryImpact,
    loadMoreImpact,
    setDialogOpen,
  }
}

function sortStrategies(strategies: LoadbalanceStrategy[]) {
  return [...strategies].sort((left, right) => {
    if (left.is_default !== right.is_default) return left.is_default ? -1 : 1
    return left.id - right.id
  })
}
