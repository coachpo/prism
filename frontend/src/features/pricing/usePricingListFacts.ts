import { useCallback, useEffect, useMemo, useState } from "react"

import { api } from "@/lib/api"
import type { PricingTemplateListPageItem } from "@/lib/types"

/**
 * Facts the list page endpoint already returns but the table never read:
 * configuration completeness, which specialty rates are missing, and how many
 * models / endpoints / terminal targets reference each template.
 *
 * They arrive from a separate read, so they carry their own failure. A failed
 * read leaves `failed` set and the map empty — the table renders that as a
 * failure, never as "zero references".
 */
export type PricingListFacts = {
  byId: Map<number, PricingTemplateListPageItem>
  failed: boolean
  loading: boolean
  refresh: () => void
}

const PAGE_LIMIT = 200

export function usePricingListFacts(revision: number): PricingListFacts {
  const [items, setItems] = useState<PricingTemplateListPageItem[]>([])
  const [failed, setFailed] = useState(false)
  const [loading, setLoading] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    void (async () => {
      try {
        const page = await api.pricingTemplates.listPage({ limit: PAGE_LIMIT })
        if (cancelled) return
        setItems(page.items)
        setFailed(false)
      } catch {
        if (cancelled) return
        setFailed(true)
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [attempt, revision])

  const byId = useMemo(() => {
    const next = new Map<number, PricingTemplateListPageItem>()
    for (const item of items) {
      const id = Number(item.id)
      if (Number.isFinite(id)) next.set(id, item)
    }
    return next
  }, [items])

  const refresh = useCallback(() => setAttempt((current) => current + 1), [])

  return { byId, failed, loading, refresh }
}

/** Total references across all three reference kinds. */
export function totalReferences(item: PricingTemplateListPageItem | undefined) {
  if (!item) return null
  return item.model_reference_count + item.endpoint_reference_count + item.terminal_target_reference_count
}

export function isRecentlyChanged(updatedAt: string, now = Date.now()) {
  const stamp = new Date(updatedAt).getTime()
  if (!Number.isFinite(stamp)) return false
  return now - stamp <= 7 * 24 * 60 * 60 * 1000
}
