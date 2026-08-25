import { useCallback, useEffect, useState } from "react";
import { models as modelsApi } from "@/lib/api/management";
import type { ModelCatalogResponse } from "@/lib/types";

interface CatalogSettled {
  token: number;
  modelConfigId: number;
  catalog: ModelCatalogResponse | null;
  failed: boolean;
}

/**
 * Loads the models.dev binding for one model. The read is management-only:
 * it never feeds routing. Mutations (bind/refresh/override) call refresh to
 * bump the reload token and re-read.
 *
 * Follows the settled-record pattern from useRoutingDiagnosticsView: pending
 * and loading are derived by comparing tokens, so no effect body ever calls
 * setState synchronously.
 */
export function useModelCatalog(modelConfigId: number | undefined, revision: number) {
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
          setSettled({ token: reloadToken, modelConfigId, catalog: response, failed: false });
        }
      } catch {
        if (!cancelled) {
          setSettled({ token: reloadToken, modelConfigId, catalog: null, failed: true });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [modelConfigId, revision, reloadToken]);

  // Pending is derived: a settled record only counts when both its token and
  // its model match the current request.
  const loading =
    !modelConfigId || Number.isNaN(modelConfigId)
      ? false
      : !(settled && settled.token === reloadToken && settled.modelConfigId === modelConfigId);
  const catalog = loading ? null : (settled?.catalog ?? null);
  const failed = !loading && Boolean(settled?.failed);

  return { catalog, loading, failed, refresh };
}
