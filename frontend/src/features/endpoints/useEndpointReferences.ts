import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { api } from "@/lib/api"
import {
  isReferenceIntegrityError,
} from "@/lib/api/endpointErrors"
import { ApiError } from "@/lib/api/core"
import type { EndpointReferenceDetail, EndpointReferenceItem, EndpointReferenceSummary } from "@/lib/types"

// Discriminated reference state per Endpoint (§8.2). Unknown/stale never
// equals zero: callers must not coerce non-ready state to an empty summary.

export type EndpointReferenceSummaryState =
  | { status: "loading"; generation: number }
  | { status: "ready"; value: EndpointReferenceSummary; generation: number; receivedAt: number }
  | { status: "stale"; value: EndpointReferenceSummary; error: ApiError; generation: number; receivedAt: number }
  | { status: "error"; error: ApiError; generation: number }

export type EndpointReferenceDetailState =
  | { status: "idle" }
  | { status: "loading"; generation: number }
  | { status: "ready"; value: EndpointReferencePagedSnapshot; generation: number; receivedAt: number }
  | { status: "stale"; value: EndpointReferencePagedSnapshot; error: ApiError; generation: number; receivedAt: number }
  | { status: "error"; error: ApiError; generation: number }

export interface EndpointReferencePagedSnapshot {
  summary: EndpointReferenceSummary
  loaded_items: EndpointReferenceItem[]
  total_count: number
  next_cursor: string | null
  reference_snapshot_hash: string
}

const BATCH_CHUNK_SIZE = 100
const MAX_CONCURRENT_BATCHES = 3

interface ReferencesState {
  summaries: Record<number, EndpointReferenceSummaryState>
  details: Record<number, EndpointReferenceDetailState>
}

function initialSummaryState(): EndpointReferenceSummaryState {
  return { status: "loading", generation: 0 }
}

function initialDetailState(): EndpointReferenceDetailState {
  return { status: "idle" }
}

function nextGeneration(current: number): number {
  return current + 1
}

function pageToSnapshot(detail: EndpointReferenceDetail): EndpointReferencePagedSnapshot {
  return {
    summary: detail.summary,
    loaded_items: detail.reference_page.items,
    total_count: detail.reference_page.total_count,
    next_cursor: detail.reference_page.next_cursor,
    reference_snapshot_hash: detail.reference_page.reference_snapshot_hash,
  }
}

export function useEndpointReferences(endpointIds: number[]) {
  const [state, setState] = useState<ReferencesState>({ summaries: {}, details: {} })
  const generationByEndpoint = useRef<Record<number, number>>({})
  const [inFlight, setInFlight] = useState(false)
  const [retryNonce, setRetryNonce] = useState(0)
  const lastRequested = useRef<number[]>([])

  const uniqueIds = useMemo(() => Array.from(new Set(endpointIds)), [endpointIds])

  // Issue one monotonic generation per Endpoint for every batch membership.
  const issueGenerations = useCallback((ids: number[]) => {
    const next = { ...generationByEndpoint.current }
    for (const id of ids) {
      next[id] = nextGeneration(next[id] ?? 0)
    }
    generationByEndpoint.current = next
    return next
  }, [])

  const commitIfCurrent = useCallback((
    id: number,
    generation: number,
    updater: (current: ReferencesState) => ReferencesState,
  ) => {
    setState((current) => {
      if (generationByEndpoint.current[id] !== generation) {
        return current
      }
      return updater(current)
    })
  }, [])

  // Fetch summaries for a chunk of Endpoint IDs. Each item commits only when
  // its captured generation is still current; a missing item is a protocol
  // error (unknown), never zero.
  const fetchChunk = useCallback(async (ids: number[], generations: Record<number, number>) => {
    try {
      const response = await api.endpoints.referencesBatch(ids)
      const byId = new Map(response.items.map((item) => [item.endpoint_id, item.summary]))
      const now = Date.now()
      for (const id of ids) {
        const generation = generations[id]
        const summary = byId.get(id)
        if (!summary) {
          const error = new ApiError("Missing reference summary item", 500, { code: "reference_missing_item" })
          commitIfCurrent(id, generation, (current) => ({
            ...current,
            summaries: { ...current.summaries, [id]: { status: "error", error, generation } },
          }))
          continue
        }
        commitIfCurrent(id, generation, (current) => ({
          ...current,
          summaries: {
            ...current.summaries,
            [id]: { status: "ready", value: summary, generation, receivedAt: now },
          },
        }))
      }
    } catch (error) {
      const apiError = error instanceof ApiError ? error : new ApiError(error instanceof Error ? error.message : "Failed to load references", 0, null)
      for (const id of ids) {
        const generation = generations[id]
        commitIfCurrent(id, generation, (current) => {
          const previous = current.summaries[id]
          if (previous && previous.status !== "loading" && (previous.status === "ready" || previous.status === "stale")) {
            return {
              ...current,
              summaries: {
                ...current.summaries,
                [id]: {
                  status: "stale",
                  value: previous.value,
                  error: apiError,
                  generation,
                  receivedAt: previous.receivedAt,
                },
              },
            }
          }
          return {
            ...current,
            summaries: { ...current.summaries, [id]: { status: "error", error: apiError, generation } },
          }
        })
      }
    }
  }, [commitIfCurrent])

  // Reconcile all visible Endpoint IDs: add new entries, remove stale ones,
  // and fetch missing/outdated summaries in bounded chunks.
  useEffect(() => {
    const previous = lastRequested.current
    lastRequested.current = uniqueIds
    const removed = previous.filter((id) => !uniqueIds.includes(id))
    if (removed.length > 0) {
      setState((current) => {
        const summaries = { ...current.summaries }
        const details = { ...current.details }
        for (const id of removed) {
          delete summaries[id]
          delete details[id]
        }
        return { summaries, details }
      })
    }

    setState((current) => {
      const summaries = { ...current.summaries }
      const details = { ...current.details }
      let changed = false
      for (const id of uniqueIds) {
        if (!summaries[id]) {
          summaries[id] = initialSummaryState()
          changed = true
        }
        if (!details[id]) {
          details[id] = initialDetailState()
          changed = true
        }
      }
      return changed ? { summaries, details } : current
    })

    const generations = issueGenerations(uniqueIds)
    const chunks: number[][] = []
    for (let index = 0; index < uniqueIds.length; index += BATCH_CHUNK_SIZE) {
      chunks.push(uniqueIds.slice(index, index + BATCH_CHUNK_SIZE))
    }
    if (chunks.length === 0) {
      setInFlight(false)
      return
    }
    setInFlight(true)
    let active = 0
    let cursor = 0
    const startNext = () => {
      while (active < MAX_CONCURRENT_BATCHES && cursor < chunks.length) {
        const chunk = chunks[cursor]
        cursor += 1
        active += 1
        void fetchChunk(chunk, generations).finally(() => {
          active -= 1
          if (cursor < chunks.length) {
            startNext()
          } else if (active === 0) {
            setInFlight(false)
          }
        })
      }
    }
    startNext()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [uniqueIds.join(","), issueGenerations, fetchChunk, retryNonce])

  // Single-detail read: opening a disclosure or a delete preflight. On
  // success the detail commits atomically with its own summary replacing the
  // batch summary for that Endpoint.
  const loadDetail = useCallback(async (endpointId: number) => {
    const generation = nextGeneration(generationByEndpoint.current[endpointId] ?? 0)
    generationByEndpoint.current[endpointId] = generation
    setState((current) => ({
      ...current,
      details: { ...current.details, [endpointId]: { status: "loading", generation } },
    }))
    try {
      const detail = await api.endpoints.referencesDetail(endpointId)
      commitIfCurrent(endpointId, generation, (current) => ({
        ...current,
        summaries: {
          ...current.summaries,
          [endpointId]: { status: "ready", value: detail.summary, generation, receivedAt: Date.now() },
        },
        details: {
          ...current.details,
          [endpointId]: { status: "ready", value: pageToSnapshot(detail), generation, receivedAt: Date.now() },
        },
      }))
    } catch (error) {
      const apiError = error instanceof ApiError ? error : new ApiError(error instanceof Error ? error.message : "Failed to load references", 0, null)
      commitIfCurrent(endpointId, generation, (current) => {
        const previousDetail = current.details[endpointId]
        if (previousDetail && (previousDetail.status === "ready" || previousDetail.status === "stale")) {
          return {
            ...current,
            details: {
              ...current.details,
              [endpointId]: { status: "stale", value: previousDetail.value, error: apiError, generation, receivedAt: previousDetail.receivedAt },
            },
          }
        }
        return {
          ...current,
          details: { ...current.details, [endpointId]: { status: "error", error: apiError, generation } },
        }
      })
    }
  }, [commitIfCurrent])

  // Adopt a detail response that was already fetched by a delete preflight.
  // Keeping this write in the same coordinator as batch/detail reads prevents
  // the dialog's fresh counts and the table's cached counts from diverging.
  const adoptDetail = useCallback((endpointId: number, detail: EndpointReferenceDetail) => {
    const generation = nextGeneration(generationByEndpoint.current[endpointId] ?? 0)
    generationByEndpoint.current[endpointId] = generation
    const receivedAt = Date.now()
    const snapshot = pageToSnapshot(detail)
    setState((current) => ({
      ...current,
      summaries: {
        ...current.summaries,
        [endpointId]: { status: "ready", value: detail.summary, generation, receivedAt },
      },
      details: {
        ...current.details,
        [endpointId]: { status: "ready", value: snapshot, generation, receivedAt },
      },
    }))
  }, [])

  // Load more along the same snapshot cursor. Any cursor mismatch, stale
  // snapshot or new generation discards accumulated pages and restarts.
  const loadMore = useCallback(async (endpointId: number): Promise<EndpointReferenceDetail | null> => {
    const current = state.details[endpointId]
    if (!current || current.status !== "ready" || !current.value.next_cursor) {
      return null
    }
    const generation = nextGeneration(generationByEndpoint.current[endpointId] ?? 0)
    generationByEndpoint.current[endpointId] = generation
    const snapshot = current.value
    setState((prev) => ({
      ...prev,
      details: { ...prev.details, [endpointId]: { status: "loading", generation } },
    }))
    try {
      const detail = await api.endpoints.referencesDetail(endpointId, snapshot.next_cursor ? { cursor: snapshot.next_cursor } : undefined)
      if (detail.reference_page.reference_snapshot_hash !== snapshot.reference_snapshot_hash || detail.reference_page.total_count !== snapshot.total_count) {
        // Snapshot changed under us: discard accumulated pages and restart
        // from page one instead of leaving a permanent loading state.
        commitIfCurrent(endpointId, generation, (prev) => ({
          ...prev,
          details: {
            ...prev.details,
            [endpointId]: { status: "loading", generation },
          },
        }))
        void loadDetail(endpointId)
        return null
      }
      const merged: EndpointReferencePagedSnapshot = {
        summary: detail.summary,
        loaded_items: [...snapshot.loaded_items, ...detail.reference_page.items],
        total_count: detail.reference_page.total_count,
        next_cursor: detail.reference_page.next_cursor,
        reference_snapshot_hash: detail.reference_page.reference_snapshot_hash,
      }
      commitIfCurrent(endpointId, generation, (prev) => {
        return {
          ...prev,
          summaries: {
            ...prev.summaries,
            [endpointId]: {
              status: "ready",
              value: detail.summary,
              generation,
              receivedAt: Date.now(),
            },
          },
          details: {
            ...prev.details,
            [endpointId]: { status: "ready", value: merged, generation, receivedAt: Date.now() },
          },
        }
      })
      if (generationByEndpoint.current[endpointId] !== generation) {
        return null
      }
      return {
        endpoint_id: endpointId,
        summary: detail.summary,
        reference_page: {
          items: merged.loaded_items,
          total_count: merged.total_count,
          next_cursor: merged.next_cursor,
          reference_snapshot_hash: merged.reference_snapshot_hash,
        },
      }
    } catch (error) {
      const apiError = error instanceof ApiError ? error : new ApiError(error instanceof Error ? error.message : "Failed to load references", 0, null)
      if (isReferenceIntegrityError(error) || (error instanceof ApiError && error.status === 409)) {
        // Stale/cursor errors discard accumulated pages and restart.
        void loadDetail(endpointId)
        return null
      }
      commitIfCurrent(endpointId, generation, (prev) => {
        const previousDetail = prev.details[endpointId]
        if (previousDetail && (previousDetail.status === "ready" || previousDetail.status === "stale")) {
          return {
            ...prev,
            details: {
              ...prev.details,
              [endpointId]: { status: "stale", value: previousDetail.value, error: apiError, generation, receivedAt: previousDetail.receivedAt },
            },
          }
        }
        return { ...prev, details: { ...prev.details, [endpointId]: { status: "error", error: apiError, generation } } }
      })
      void apiError
      return null
    }
  }, [commitIfCurrent, loadDetail, state.details])

  // Delete removes both states; create/duplicate adds loading summary + idle detail.
  const removeEndpoint = useCallback((endpointId: number) => {
    delete generationByEndpoint.current[endpointId]
    setState((current) => {
      const summaries = { ...current.summaries }
      const details = { ...current.details }
      delete summaries[endpointId]
      delete details[endpointId]
      return { summaries, details }
    })
  }, [])

  const addEndpoint = useCallback((endpointId: number) => {
    generationByEndpoint.current[endpointId] = nextGeneration(generationByEndpoint.current[endpointId] ?? 0)
    setState((current) => ({
      summaries: { ...current.summaries, [endpointId]: { status: "loading", generation: generationByEndpoint.current[endpointId] } },
      details: { ...current.details, [endpointId]: { status: "idle" } },
    }))
  }, [])

  const invalidateEndpoint = useCallback((endpointId: number) => {
    void loadDetail(endpointId)
  }, [loadDetail])

  const retry = useCallback(() => {
    setState((current) => {
      const summaries = { ...current.summaries }
      for (const id of uniqueIds) {
        summaries[id] = { status: "loading", generation: generationByEndpoint.current[id] ?? 0 }
      }
      return { ...current, summaries }
    })
    setRetryNonce((current) => current + 1)
  }, [uniqueIds])

  // A 409/integrity error on any item disables reference-derived filter/sort.
  const hasUnknownOrStale = useMemo(() => {
    return uniqueIds.some((id) => {
      const summary = state.summaries[id]
      return !summary || summary.status !== "ready"
    })
  }, [state.summaries, uniqueIds])

  const hasIntegrityError = useMemo(() => {
    return uniqueIds.some((id) => {
      const summary = state.summaries[id]
      return summary?.status === "error" && isReferenceIntegrityError(summary.error)
    })
  }, [state.summaries, uniqueIds])

  const hasReferenceError = useMemo(() => {
    return uniqueIds.some((id) => {
      const summary = state.summaries[id]
      return summary?.status === "error" || summary?.status === "stale"
    })
  }, [state.summaries, uniqueIds])

  return {
    adoptDetail,
    addEndpoint,
    details: state.details,
    hasIntegrityError,
    hasUnknownOrStale,
    inFlight,
    invalidateEndpoint,
    loadDetail,
    loadMore,
    removeEndpoint,
    retry,
    summaries: state.summaries,
    hasReferenceError,
  }
}

export function isReferenceSummaryFreshReady(summary: EndpointReferenceSummaryState | undefined): summary is { status: "ready"; value: EndpointReferenceSummary; generation: number; receivedAt: number } {
  return Boolean(summary && summary.status === "ready")
}

export function isReferenceDetailReady(detail: EndpointReferenceDetailState | undefined): detail is { status: "ready"; value: EndpointReferencePagedSnapshot; generation: number; receivedAt: number } {
  return Boolean(detail && detail.status === "ready")
}
