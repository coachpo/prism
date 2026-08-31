import { useCallback, useEffect, useState } from "react";
import { models as modelsApi } from "@/lib/api/models";
import type { ModelCatalogResponse } from "@/lib/types";

interface CatalogSettled {
  token: number;
  modelConfigId: number;
  catalog: ModelCatalogResponse | null;
  failed: boolean;
  /** Failure message of the failed read; null on success. */
  error: string | null;
  /** When the last successful read completed; null until one succeeds. */
  lastSuccessfulAt: string | null;
}

export interface ModelCatalogView {
  catalog: ModelCatalogResponse | null;
  /** First read in flight: no data has ever arrived for this model. */
  loading: boolean;
  /** A same-model authoritative re-read is in flight while last-good stays visible. */
  refreshing: boolean;
  /** The current read failed. With last-good data this means stale, not unbound. */
  failed: boolean;
  /** The failure message of the current read, if any. */
  error: string | null;
  /** A previous successful read exists for this model and is being shown. */
  hasLastGood: boolean;
  /** ISO stamp of the last successful read for this model. */
  lastSuccessfulAt: string | null;
  refresh: () => void;
}

function failureMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

/**
 * Loads the models.dev binding for one model and keeps the honest read state.
 * The read is management-only: it never feeds routing.
 *
 * The three states a consumer must tell apart:
 * - `loading`    the first read is in flight (or the model changed and the
 *                old model's data has been withdrawn — data never survives a
 *                model id change);
 * - `failed`     the current read failed. With `hasLastGood` the panel shows
 *                the previous metadata labelled stale; without it the panel
 *                shows an error, never a fabricated "unbound";
 * - settled      a successful read: `catalog` is the authoritative response.
 *
 * Follows the settled-record pattern from useRoutingDiagnosticsView: pending
 * and loading are derived by comparing tokens, so no effect body ever calls
 * setState synchronously.
 */
export function useModelCatalog(
  modelConfigId: number | undefined,
  revision: number,
): ModelCatalogView {
  const [settled, setSettled] = useState<CatalogSettled | null>(null);
  const [reloadToken, setReloadToken] = useState(0);

  const refresh = useCallback(() => {
    setReloadToken((token) => token + 1);
  }, []);

  useEffect(() => {
    if (!modelConfigId || Number.isNaN(modelConfigId)) return;
    let cancelled = false;
    void (async () => {
      try {
        const response = await modelsApi.catalog.get(modelConfigId);
        if (!cancelled) {
          setSettled({
            token: reloadToken,
            modelConfigId,
            catalog: response,
            failed: false,
            error: null,
            lastSuccessfulAt: new Date().toISOString(),
          });
        }
      } catch (cause) {
        if (!cancelled) {
          setSettled((previous) => ({
            token: reloadToken,
            modelConfigId,
            // A failed re-read of the same model retains its last successful
            // truth. A different model never inherits this value.
            catalog:
              previous?.modelConfigId === modelConfigId
                ? previous.catalog
                : null,
            failed: true,
            error: failureMessage(cause),
            // A failed re-read keeps the previous read's stamp: the last-good
            // data is still what the panel shows, so "when is this from"
            // keeps pointing at the last successful read.
            lastSuccessfulAt:
              previous?.modelConfigId === modelConfigId
                ? previous.lastSuccessfulAt
                : null,
          }));
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [modelConfigId, revision, reloadToken]);

  // A same-model settled record stays available while a refresh is in flight;
  // only a model id change withdraws it. `loading` therefore means there is no
  // last-good value for the current model, matching the public hook contract.
  const current =
    settled && settled.modelConfigId === modelConfigId ? settled : null;
  const validModel = Boolean(modelConfigId) && !Number.isNaN(modelConfigId);
  const pending = current?.token !== reloadToken;
  const loading = validModel && (!current || (!current.catalog && pending));
  const refreshing =
    validModel && current !== null && Boolean(current.catalog) && pending;
  const failed = !loading && Boolean(current?.failed);
  const hasLastGood = !loading && Boolean(current?.catalog);

  return {
    catalog: current?.catalog ?? null,
    loading,
    refreshing,
    failed,
    error: failed ? (current?.error ?? null) : null,
    hasLastGood,
    lastSuccessfulAt: current?.lastSuccessfulAt ?? null,
    refresh,
  };
}
