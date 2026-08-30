import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { pricingTemplates as pricingApi } from "@/lib/api/pricingTemplates";
import type {
  CatalogPricingPreviewRequest,
  CatalogPricingPreviewResponse,
} from "@/lib/types";

/**
 * How a catalog pricing import addresses its offering.
 *
 * - `bound_model` resolves the offering from the model's persisted models.dev
 *   binding. This is the model-detail path.
 * - `coordinates` addresses an offering explicitly, which is what the pricing
 *   page does after a unique auto-match or a human candidate pick. A
 *   `modelConfigId` may ride along as display evidence only; it never changes
 *   what the commit writes.
 */
export type CatalogPricingSource =
  | { kind: "bound_model"; modelConfigId: number }
  | {
      kind: "coordinates";
      providerId: string;
      catalogModelId: string;
      modelConfigId?: number;
    };

/** The request body a source produces for preview and commit replay. */
export function catalogSourceRequest(
  source: CatalogPricingSource,
): CatalogPricingPreviewRequest {
  if (source.kind === "bound_model") {
    return { model_config_id: source.modelConfigId };
  }
  const body: CatalogPricingPreviewRequest = {
    provider_id: source.providerId,
    catalog_model_id: source.catalogModelId,
  };
  if (source.modelConfigId !== undefined) {
    body.model_config_id = source.modelConfigId;
  }
  return body;
}

export function catalogSourceKey(source: CatalogPricingSource): string {
  const request = catalogSourceRequest(source);
  return [
    request.model_config_id ?? "",
    request.provider_id ?? "",
    request.catalog_model_id ?? "",
  ].join("|");
}
/** Sorted, de-duplicated, comma-joined target ids: the preview's CAS snapshot. */
export function catalogTargetsKey(connectionIds: number[]): string {
  return [...new Set(connectionIds)].sort((a, b) => a - b).join(",");
}

type SettledPreview = {
  /** Identifies the (source, targets) pair this result belongs to. */
  sourceKey: string;
  targetsKey: string;
  preview: CatalogPricingPreviewResponse | null;
  error: string | null;
};

export interface UseCatalogPricingImportOptions {
  source: CatalogPricingSource;
  /** Initial Terminal Target selection. The pricing page passes an empty list;
   *  the model-detail dialog passes the current target. */
  initialConnectionIds: number[];
  /** Open once the dialog is mounted. A closed dialog performs no reads. */
  enabled: boolean;
}

export interface UseCatalogPricingImportResult {
  connectionIds: number[];
  toggleConnection: (connectionId: number) => void;
  preview: CatalogPricingPreviewResponse | null;
  /** True while the settled record does not match the current selection. */
  loading: boolean;
  error: string | null;
  confirmDrift: boolean;
  setConfirmDrift: (value: boolean) => void;
  committing: boolean;
  /** Re-run the preview against current server state. */
  refresh: () => void;
  /** Commit the settled preview. Returns the response, or null on failure. */
  commit: () => Promise<CatalogPricingPreviewResponse | null>;
}

/**
 * The shared catalog pricing preview/commit protocol.
 *
 * Invariants this owner is responsible for:
 * - A preview is always fetched for the exact current target set. Changing the
 *   set invalidates the settled result immediately, so an operator can never
 *   commit a stale CAS snapshot by accident.
 * - Nothing is ever auto-selected or auto-committed: this hook only reads, and
 *   `commit` is called from an explicit action.
 * - A rejected commit clears the preview and re-reads, because every 409 family
 *   means server state moved.
 */
export function useCatalogPricingImport({
  source,
  initialConnectionIds,
  enabled,
}: UseCatalogPricingImportOptions): UseCatalogPricingImportResult {
  const [connectionIds, setConnectionIds] = useState<number[]>(() =>
    [...new Set(initialConnectionIds)].sort((a, b) => a - b),
  );
  const [settled, setSettled] = useState<SettledPreview | null>(null);
  // Acknowledgement belongs to one exact preview, not to the dialog lifetime.
  // A different offering or target set necessarily produces another hash and
  // must be confirmed independently before it may overwrite manual drift.
  const [confirmedPreviewHash, setConfirmedPreviewHash] = useState<
    string | null
  >(null);
  const [committing, setCommitting] = useState(false);
  const [reloadToken, setReloadToken] = useState(0);
  const requestGeneration = useRef(0);

  const sourceKey = useMemo(() => catalogSourceKey(source), [source]);
  const targetsKey = useMemo(
    () => catalogTargetsKey(connectionIds),
    [connectionIds],
  );

  const loading =
    enabled &&
    (settled === null ||
      settled.sourceKey !== sourceKey ||
      settled.targetsKey !== targetsKey);
  const preview = loading ? null : (settled?.preview ?? null);
  const error = loading ? null : (settled?.error ?? null);
  const confirmDrift = Boolean(
    preview?.preview_hash && confirmedPreviewHash === preview.preview_hash,
  );

  const setConfirmDrift = useCallback(
    (value: boolean) => {
      setConfirmedPreviewHash(
        value && preview?.preview_hash ? preview.preview_hash : null,
      );
    },
    [preview?.preview_hash],
  );

  // Fetch the preview for one exact (source, targets) pair. Results that no
  // longer match the current selection are dropped instead of rendered.
  useEffect(() => {
    if (!enabled) return;
    const generation = ++requestGeneration.current;
    const ids = targetsKey === "" ? [] : targetsKey.split(",").map(Number);
    void (async () => {
      try {
        const response = await pricingApi.catalogPreview({
          ...catalogSourceRequest(source),
          connection_ids: ids,
        });
        if (generation !== requestGeneration.current) return;
        setSettled({ sourceKey, targetsKey, preview: response, error: null });
      } catch (cause) {
        if (generation !== requestGeneration.current) return;
        setSettled({
          sourceKey,
          targetsKey,
          preview: null,
          error: cause instanceof Error ? cause.message : String(cause),
        });
      }
    })();
    // `source` is reconstructed by callers on every render, so the derived
    // sourceKey is the stable identity this effect actually depends on.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, sourceKey, targetsKey, reloadToken]);

  const toggleConnection = useCallback((connectionId: number) => {
    setConnectionIds((current) => {
      const next = new Set(current);
      if (next.has(connectionId)) next.delete(connectionId);
      else next.add(connectionId);
      return [...next].sort((a, b) => a - b);
    });
    // Any selection change invalidates the drift acknowledgement: the operator
    // is about to look at a different preview.
    setConfirmedPreviewHash(null);
  }, []);

  const refresh = useCallback(() => {
    setSettled(null);
    setConfirmedPreviewHash(null);
    setReloadToken((token) => token + 1);
  }, []);

  const commit = useCallback(async () => {
    const current = settled;
    if (
      !current ||
      current.sourceKey !== sourceKey ||
      current.targetsKey !== targetsKey ||
      !current.preview?.preview_hash ||
      !current.preview.committable ||
      (current.preview.drift && !confirmDrift)
    ) {
      return null;
    }
    const ids = targetsKey === "" ? [] : targetsKey.split(",").map(Number);
    setCommitting(true);
    try {
      await pricingApi.catalogCommit({
        schema_version: current.preview.schema_version,
        ...catalogSourceRequest(source),
        connection_ids: ids,
        preview_hash: current.preview.preview_hash,
        expected_catalog_revision: current.preview.catalog_revision,
        confirm_drift: confirmDrift,
      });
      return current.preview;
    } catch (cause) {
      // Every rejection here means authoritative state moved (stale revision,
      // stale preview, unconfirmed drift, or a target CAS conflict). Discard the
      // preview and re-read rather than guessing what is still valid.
      setSettled(null);
      setConfirmedPreviewHash(null);
      setReloadToken((token) => token + 1);
      throw cause instanceof Error ? cause : new Error(String(cause));
    } finally {
      setCommitting(false);
    }
  }, [confirmDrift, settled, source, sourceKey, targetsKey]);

  return {
    commit,
    committing,
    confirmDrift,
    connectionIds,
    error,
    loading,
    preview,
    refresh,
    setConfirmDrift,
    toggleConnection,
  };
}
