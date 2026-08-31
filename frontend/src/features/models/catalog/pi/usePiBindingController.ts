import { useCallback, useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { api } from "@/lib/api";
import type {
  PiCandidateWire,
  PiOverrideFieldValue,
  PiRefreshPreviewResponse,
} from "@/lib/types";
import {
  useAppendCandidatePager,
  type AppendCandidatePager,
} from "@/shared/table/useAppendCandidatePager";

/** Shared page size with the backend search default. */
export const PI_DIRECTORY_SEARCH_PAGE_SIZE = 20;

/**
 * The narrow, host-independent Pi binding state a dialog needs. Both hosts
 * build it with their own authoritative read: the export host maps an
 * aggregate `ExportSourceModelRow`, the model-detail host maps
 * `GET /models/{id}/pi`. The controller never reads either host's data
 * directly, and mutations always end with the host's own authoritative
 * re-read — no optimistic patching.
 */
export interface PiCatalogModelView {
  modelConfigId: number;
  /** Prism model_id — never the directory id. */
  modelId: string;
  /** Final Pi API Prism maps this model to, or "" when undeterminable. */
  piApi: string;
  /** Live exact-candidate discovery evidence; never render authority. */
  liveCandidates: PiCandidateWire[];
  /** Persisted binding coordinate, when bound. */
  selected?: { provider_id: string; model_id: string; api: string } | null;
  /** Frozen Prism identity snapshot at bind time. */
  bindingPrismModelId?: string;
  bindingSource?: PiBindingMetadataView | null;
  bindingOverride?: PiBindingMetadataView | null;
  bindingEffective?: PiBindingMetadataView | null;
  bindingStatus: "unbound" | "bound" | "bound_drifted";
  bindingRenderable: boolean;
  /** Bound rows with an incomplete coordinate or frozen identity fail closed. */
  bindingIntegrityError: boolean;
  /** Source-qualified revision + freshness of the host's catalog evidence. */
  catalogRevision: string;
  catalogFresh: boolean;
}

export interface PiDirectorySearchEvidence {
  modelConfigId: number;
  modelId: string;
  piApi: string;
  query: string;
  nonce: number;
  catalogStatus: "fresh" | "stale" | "unavailable";
  catalogRevision: string;
  fetchedAt: string;
  checkedAt: string;
}

export interface PiBindingMetadataView {
  name: string | null;
  reasoning: boolean | null;
  input: string[] | null;
  context_window: number | null;
  max_tokens: number | null;
  thinking_level_map: Record<string, string | null> | null;
  compat: Record<string, unknown> | null;
}

interface BindInput {
  modelConfigId: number;
  providerId?: string;
  catalogModelId?: string;
  expectedCatalogRevision: string;
  expectedPrismModelId: string;
  expectedPiApi: string;
}

interface RefreshCommitInput {
  modelConfigId: number;
  expected: {
    provider_id: string;
    catalog_model_id: string;
    api: string;
    binding_updated_at: string;
    catalog_revision: string;
  };
}

interface OverrideInput {
  modelConfigId: number;
  fields: Record<string, PiOverrideFieldValue>;
}

function piCandidateKey(candidate: PiCandidateWire): string {
  return `${candidate.provider_id}/${candidate.model_id}`;
}

/**
 * The narrow Pi binding controller shared by the export page and the model
 * detail panel. It owns the paged pi.dev directory search (over the shared
 * append pager, `sourceKey: "pi.dev"`), the bind mutation, refresh
 * preview/commit, sparse override/clear, unbind, and mutation-pending state.
 * After every successful mutation it calls the host's authoritative
 * `reconcile()` — the controller never patches host state optimistically.
 *
 * It does not own export batch selection/filtering, targets/pricing risk,
 * source digest, gateway origin/credentials, render/blob lifecycle, or the
 * model-detail route state.
 */
export function usePiBindingController(options: {
  /** Host-authoritative re-read invoked after every successful mutation. */
  reconcile: () => Promise<void>;
  /**
   * True while the host's authoritative read is in flight or failed: the
   * dialogs stay inert rather than acting on unverified state.
   */
  actionsBlocked?: boolean;
}) {
  const { reconcile, actionsBlocked = false } = options;

  // Directory search state: the submitted query is a controller-owned
  // condition so the pager can own generation/rollover semantics. A query
  // edit clears field/read errors; only the explicit search action commits
  // the condition.
  const [searchCondition, setSearchCondition] = useState<{
    modelConfigId: number;
    modelId: string;
    piApi: string;
    query: string;
    nonce: number;
  } | null>(null);
  const [searchNonce, setSearchNonce] = useState(0);
  const [searchFieldError, setSearchFieldError] = useState<string | null>(null);

  const fetchSearchPage = useCallback(
    async (
      offset: number,
      signal: AbortSignal,
    ): Promise<{
      items: PiCandidateWire[];
      total: number;
      offset: number;
      revision: string;
      evidence?: PiDirectorySearchEvidence;
    }> => {
      if (!searchCondition) {
        return { items: [], total: 0, offset: 0, revision: "" };
      }
      const response = await api.modelExport.searchModelPiCatalog(
        searchCondition.modelConfigId,
        {
          model_id_query: searchCondition.query,
          limit: PI_DIRECTORY_SEARCH_PAGE_SIZE,
          offset,
        },
        signal,
      );
      if (
        response.export_identity.model_config_id !==
          searchCondition.modelConfigId ||
        response.export_identity.model_id !== searchCondition.modelId ||
        response.export_identity.api !== searchCondition.piApi ||
        response.api !== searchCondition.piApi
      ) {
        throw new Error(
          "pi_catalog_search_identity_changed: the Prism model identity changed; search again",
        );
      }
      const catalogRevision = response.catalog.revision ?? "";
      return {
        items: Array.isArray(response.results) ? response.results : [],
        total: Number.isFinite(response.total) ? response.total : 0,
        offset: Number.isFinite(response.offset) ? response.offset : offset,
        revision: catalogRevision ? `pi.dev:${catalogRevision}` : "",
        evidence: {
          modelConfigId: searchCondition.modelConfigId,
          modelId: searchCondition.modelId,
          piApi: searchCondition.piApi,
          query: searchCondition.query,
          nonce: searchCondition.nonce,
          catalogStatus: response.catalog.status,
          catalogRevision,
          fetchedAt: response.fetched_at,
          checkedAt: response.checked_at,
        },
      };
    },
    [searchCondition],
  );

  const directorySearchPager = useAppendCandidatePager<
    PiCandidateWire,
    PiDirectorySearchEvidence
  >({
    sourceKey: "pi.dev",
    enabled: searchCondition !== null,
    conditionKey: searchCondition
      ? `${searchCondition.modelConfigId}\u0000${searchCondition.modelId}\u0000${searchCondition.piApi}\u0000${searchCondition.query}\u0000${searchCondition.nonce}`
      : "",
    fetchPage: fetchSearchPage,
    itemKey: piCandidateKey,
  });

  const startDirectorySearch = useCallback(
    (
      model: { modelConfigId: number; modelId: string; piApi: string },
      rawQuery: string,
    ) => {
      const query = rawQuery.trim();
      if (query === "") {
        setSearchFieldError("model_id_query");
        return;
      }
      setSearchFieldError(null);
      // Every explicit click re-commits the condition as a new generation, so
      // re-running the same query re-issues the read (old dialog behavior).
      const nonce = searchNonce + 1;
      setSearchNonce(nonce);
      setSearchCondition({ ...model, query, nonce });
    },
    [searchNonce],
  );

  const clearSearchErrors = useCallback(() => {
    setSearchFieldError(null);
  }, []);

  const resetDirectorySearch = useCallback(() => {
    setSearchCondition(null);
    setSearchFieldError(null);
  }, []);

  const bindMutation = useMutation({
    mutationFn: (input: BindInput) =>
      api.modelExport.bindModelPi(input.modelConfigId, {
        provider_id: input.providerId,
        catalog_model_id: input.catalogModelId,
        expected_catalog_revision: input.expectedCatalogRevision,
        expected_prism_model_id: input.expectedPrismModelId,
        expected_pi_api: input.expectedPiApi,
      }),
    onSuccess: reconcile,
  });

  const refreshPreviewMutation = useMutation({
    mutationFn: (modelConfigId: number) =>
      api.modelExport.refreshModelPiPreview(modelConfigId),
  });

  const refreshCommitMutation = useMutation({
    mutationFn: (input: RefreshCommitInput) =>
      api.modelExport.refreshModelPiCommit(input.modelConfigId, {
        expected_provider_id: input.expected.provider_id,
        expected_catalog_model_id: input.expected.catalog_model_id,
        expected_api: input.expected.api,
        expected_binding_updated_at: input.expected.binding_updated_at,
        expected_catalog_revision: input.expected.catalog_revision,
      }),
    onSuccess: reconcile,
  });

  const overrideMutation = useMutation({
    mutationFn: (input: OverrideInput) =>
      api.modelExport.putModelPiOverride(input.modelConfigId, input.fields),
    onSuccess: reconcile,
  });

  const clearOverrideMutation = useMutation({
    mutationFn: (modelConfigId: number) =>
      api.modelExport.clearModelPiOverride(modelConfigId),
    onSuccess: reconcile,
  });

  const unbindMutation = useMutation({
    mutationFn: (modelConfigId: number) =>
      api.modelExport.unbindModelPi(modelConfigId),
    onSuccess: reconcile,
  });

  const mutationPending =
    bindMutation.isPending ||
    refreshCommitMutation.isPending ||
    overrideMutation.isPending ||
    clearOverrideMutation.isPending ||
    unbindMutation.isPending;

  const openRefreshPreview = useCallback(
    (modelConfigId: number): Promise<PiRefreshPreviewResponse> =>
      refreshPreviewMutation.mutateAsync(modelConfigId),
    [refreshPreviewMutation],
  );

  return {
    // directory search (paged, pi.dev-qualified, never auto-selected)
    directorySearch: {
      pager: directorySearchPager,
      pending:
        directorySearchPager.replacing || directorySearchPager.appending,
      error: directorySearchPager.error,
      fieldError: searchFieldError,
      status: directorySearchPager.evidence?.catalogStatus ?? null,
      evidence: directorySearchPager.evidence,
      start: startDirectorySearch,
      reset: resetDirectorySearch,
      clearErrors: clearSearchErrors,
      activeQuery: searchCondition?.query ?? "",
      activeModelConfigId: searchCondition?.modelConfigId ?? null,
    } as {
      pager: AppendCandidatePager<
        PiCandidateWire,
        PiDirectorySearchEvidence
      >;
      pending: boolean;
      error: string | null;
      fieldError: string | null;
      status: "fresh" | "stale" | "unavailable" | null;
      evidence: PiDirectorySearchEvidence | null;
      start: (
        model: { modelConfigId: number; modelId: string; piApi: string },
        rawQuery: string,
      ) => void;
      reset: () => void;
      clearErrors: () => void;
      activeQuery: string;
      activeModelConfigId: number | null;
    },
    // mutations
    bind: bindMutation.mutateAsync,
    openRefreshPreview,
    refreshCommit: refreshCommitMutation.mutateAsync,
    putOverride: overrideMutation.mutateAsync,
    clearOverride: clearOverrideMutation.mutateAsync,
    unbind: unbindMutation.mutateAsync,
    mutationPending,
    bindPending: bindMutation.isPending,
    // Host gate: inert while the host's authoritative read is unavailable.
    actionsBlocked: actionsBlocked || mutationPending,
  };
}

export type PiBindingController = ReturnType<typeof usePiBindingController>;

/** Maps the export source row into the narrow controller view. */
export function piViewFromExportRow(row: {
  model_config_id: number;
  model_id: string;
  pi_api?: string;
  pi_candidates: PiCandidateWire[];
  pi_selected?: { provider_id: string; model_id: string; api: string } | null;
  pi_binding_prism_model_id?: string;
  pi_binding_source?: PiBindingMetadataView | null;
  pi_binding_override?: PiBindingMetadataView | null;
  pi_binding_effective?: PiBindingMetadataView | null;
  pi_binding_status: PiCatalogModelView["bindingStatus"];
  pi_binding_renderable: boolean;
  sourceCatalogRevision: string;
  sourceCatalogFresh: boolean;
}): PiCatalogModelView {
  return {
    modelConfigId: row.model_config_id,
    modelId: row.model_id,
    piApi: row.pi_api ?? "",
    liveCandidates: row.pi_candidates,
    selected: row.pi_selected ?? null,
    bindingPrismModelId: row.pi_binding_prism_model_id,
    bindingSource: row.pi_binding_source ?? null,
    bindingOverride: row.pi_binding_override ?? null,
    bindingEffective: row.pi_binding_effective ?? null,
    bindingStatus: row.pi_binding_status,
    bindingRenderable: row.pi_binding_renderable,
    bindingIntegrityError:
      Boolean(row.pi_selected) && !row.pi_binding_prism_model_id,
    catalogRevision: row.sourceCatalogRevision,
    catalogFresh: row.sourceCatalogFresh,
  };
}

/** Maps the single-model Pi read into the same narrow controller view. */
export function piViewFromModelRead(read: {
  model: { model_config_id: number; model_id: string; pi_api?: string };
  catalog: { status: string; revision?: string };
  candidates: PiCandidateWire[];
  binding: {
    bound: boolean;
    provider_id?: string;
    catalog_model_id?: string;
    api?: string;
    prism_model_id_at_bind?: string;
    source: PiBindingMetadataView | null;
    override: PiBindingMetadataView | null;
    effective: PiBindingMetadataView | null;
  };
  binding_status: PiCatalogModelView["bindingStatus"];
  binding_renderable: boolean;
}): PiCatalogModelView {
  return {
    modelConfigId: read.model.model_config_id,
    modelId: read.model.model_id,
    piApi: read.model.pi_api ?? "",
    liveCandidates: read.candidates,
    selected:
      read.binding.bound &&
      Boolean(read.binding.provider_id) &&
      Boolean(read.binding.catalog_model_id) &&
      Boolean(read.binding.api)
      ? {
          provider_id: read.binding.provider_id ?? "",
          model_id: read.binding.catalog_model_id ?? "",
          api: read.binding.api ?? "",
        }
      : null,
    bindingPrismModelId: read.binding.prism_model_id_at_bind,
    bindingSource: read.binding.source ?? null,
    bindingOverride: read.binding.override ?? null,
    bindingEffective: read.binding.effective ?? null,
    bindingStatus: read.binding_status,
    bindingRenderable: read.binding_renderable,
    bindingIntegrityError:
      read.binding.bound &&
      (!read.binding.provider_id ||
        !read.binding.catalog_model_id ||
        !read.binding.api ||
        !read.binding.prism_model_id_at_bind),
    catalogRevision: read.catalog.revision ?? "",
    catalogFresh: read.catalog.status === "fresh",
  };
}

/** Narrow host gate shared by both dialogs' confirm logic. */
export function usePiSearchSelection(
  pager: AppendCandidatePager<PiCandidateWire>,
) {
  return useMemo(
    () => ({
      find(key: string | null): PiCandidateWire | null {
        if (!key) return null;
        return (
          pager.items.find((candidate) => piCandidateKey(candidate) === key) ??
          null
        );
      },
      keyOf: piCandidateKey,
    }),
    [pager.items],
  );
}
