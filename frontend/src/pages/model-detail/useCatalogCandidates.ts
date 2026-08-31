import { useCallback, useEffect, useState } from "react";

import { api } from "@/lib/api";
import type { CatalogCandidate } from "@/lib/types";
import {
  useAppendCandidatePager,
  type CandidatePage,
} from "@/shared/table/useAppendCandidatePager";

/**
 * Bounded catalog candidate search uses the same page size as the backend
 * default (`backend/internal/httpapi/management/models/catalog_read_routes.go`)
 * so the first read never has to re-request a smaller window.
 */
export const CATALOG_CANDIDATE_PAGE_SIZE = 20;

/** Keystroke debounce for the replace read, unchanged from the first cut. */
const CANDIDATE_QUERY_DEBOUNCE_MS = 250;

export type { CandidatePhase } from "@/shared/table/useAppendCandidatePager";

function candidateKey(candidate: CatalogCandidate): string {
  return `${candidate.provider_id}/${candidate.model_id}`;
}

/**
 * models.dev adapter over the shared append pager. This hook owns only what
 * is models.dev-specific: the 250ms query debounce, the `family|all` scope
 * policy, the wire mapping from the candidates endpoint (including the
 * source-qualified `models.dev:<etag>` snapshot revision), and the public
 * pager view. Replace/append lifecycle, generation ownership, abort,
 * single-flight, dedupe, retry, and revision rollover all live in the shared
 * controller — there is no second reducer here. The Playwright journeys
 * (`model-catalog-pricing.spec.ts`) keep covering the user-visible paging
 * behavior end to end on top of this adapter.
 */
export function useCatalogCandidates(
  modelConfigId: number | null,
  query: string,
) {
  // `settled=false` until the first debounce window closes: the pager stays
  // disabled so the first read fires exactly once, after the same 250ms
  // window that gates later query edits (StrictMode double-mount safe).
  // `settleToken` increments on every debounce settle — even when the settled
  // text is unchanged — so an A→B→A edit where B never settles still commits
  // a fresh generation instead of leaving the gated A read as current.
  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [settleToken, setSettleToken] = useState(0);
  const [settled, setSettled] = useState(false);

  // Debounce is a models.dev dialog concern: the shared pager only sees the
  // committed query through conditionKey.
  useEffect(() => {
    const handle = setTimeout(() => {
      setDebouncedQuery(query);
      setSettleToken((token) => token + 1);
      setSettled(true);
    }, CANDIDATE_QUERY_DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [query]);

  const conditionKey = `${modelConfigId ?? ""}\u0000${debouncedQuery}\u0000${settleToken}`;

  const fetchPage = useCallback(
    async (
      offset: number,
      signal: AbortSignal,
    ): Promise<CandidatePage<CatalogCandidate>> => {
      if (modelConfigId === null) {
        return { items: [], total: 0, offset: 0, revision: "" };
      }
      const response = await api.models.catalog.candidates(modelConfigId, {
        // Empty query stays scoped to the api_family; a search reaches every
        // provider so manual binding can find aggregator offerings.
        q: debouncedQuery ? debouncedQuery : undefined,
        scope: debouncedQuery ? "all" : "family",
        limit: CATALOG_CANDIDATE_PAGE_SIZE,
        offset,
        signal,
      });
      const items = Array.isArray(response.items) ? response.items : [];
      const total = Number.isFinite(response.total)
        ? response.total
        : items.length;
      return {
        items,
        total,
        offset: Number.isFinite(response.offset) ? response.offset : offset,
        // Source-qualified snapshot revision: the bare ETag must never be
        // compared against another source's revision space.
        revision: response.catalog_revision
          ? `models.dev:${response.catalog_revision}`
          : "",
      };
    },
    [modelConfigId, debouncedQuery],
  );

  const pager = useAppendCandidatePager<CatalogCandidate>({
    sourceKey: "models.dev",
    enabled: modelConfigId !== null && settled,
    conditionKey,
    fetchPage,
    itemKey: candidateKey,
  });

  return {
    items: pager.items,
    total: pager.total,
    phase: pager.phase,
    replacing: pager.replacing,
    appending: pager.appending,
    error: pager.error,
    appendError: pager.appendError,
    hasMore: pager.hasMore,
    revision: pager.revision,
    revisionRolledOver: pager.revisionRolledOver,
    onLoadMore: pager.onLoadMore,
    onRetry: pager.onRetry,
    onRolloverAcknowledged: pager.onRolloverAcknowledged,
  };
}

export type CatalogCandidatePager = ReturnType<typeof useCatalogCandidates>;
