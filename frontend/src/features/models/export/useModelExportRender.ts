import { useCallback, useMemo, useState } from "react";
import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin";
import { api } from "@/lib/api";
import type { ExportRenderResponse, ExportSourceResponse } from "@/lib/types";
import type { KeyDecision } from "./ExportKeyDialog";

const DEFAULT_PROVIDER_ID = "prism";

/**
 * 主操作为什么不可用。禁用理由必须有名字：页面据此直接说出原因，
 * 而不是回头去反推一条或链里究竟是哪一项为真。`null` 表示可以生成。
 */
export type ModelExportBlockReason =
  | "source_unavailable"
  | "source_actions_blocked"
  | "no_selection"
  | "destination_invalid"
  | "unbound_selection"
  | null;

function defaultGatewayOrigin(): string {
  return getEffectiveBackendOrigin().origin;
}

function normalizeGatewayOrigin(value: string): string | null {
  try {
    const parsed = new URL(value.trim());
    if (
      (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
      parsed.username !== "" ||
      parsed.password !== "" ||
      parsed.search !== "" ||
      parsed.hash !== "" ||
      (parsed.pathname !== "" && parsed.pathname !== "/")
    ) {
      return null;
    }
    return parsed.origin;
  } catch {
    return null;
  }
}

interface UseModelExportRenderInput {
  refetchSource: () => unknown;
  renderFailedMessage: string;
  selectedIds: ReadonlySet<number>;
  source: ExportSourceResponse | undefined;
  sourceActionsBlocked: boolean;
}

export function useModelExportRender({
  refetchSource,
  renderFailedMessage,
  selectedIds,
  source,
  sourceActionsBlocked,
}: UseModelExportRenderInput) {
  const [gatewayOrigin, setGatewayOrigin] = useState(defaultGatewayOrigin);
  const [providerId, setProviderId] = useState(DEFAULT_PROVIDER_ID);
  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const [renderResult, setRenderResult] = useState<ExportRenderResponse | null>(
    null,
  );
  const [renderError, setRenderError] = useState<string | null>(null);
  const [renderStale, setRenderStale] = useState(false);

  const normalizedOrigin = useMemo(
    () => normalizeGatewayOrigin(gatewayOrigin),
    [gatewayOrigin],
  );
  const normalizedProviderId = providerId.trim();
  const gatewayOriginInvalid = normalizedOrigin === null;
  const providerIdInvalid =
    normalizedProviderId === "" || normalizedProviderId.includes("/");

  // Render only trusts a persisted binding whose frozen model id and final
  // Pi API still match current Prism truth. Live catalog drift alone does
  // not invalidate those frozen render bytes.
  // 选中项里哪些真的能渲染。页面要说出还差几个，也要能一键把选择收敛到
  // 可渲染的那些，两者必须用同一个判据。
  const renderableSelectedIds = useMemo(() => {
    const renderable = new Set<number>();
    if (!source) return renderable;
    for (const id of selectedIds) {
      const model = source.models.find((m) => m.model_config_id === id);
      if (model?.pi_selected && model.pi_binding_renderable) renderable.add(id);
    }
    return renderable;
  }, [selectedIds, source]);

  const unboundSelectedCount = source
    ? selectedIds.size - renderableSelectedIds.size
    : 0;

  const blockReason: ModelExportBlockReason = !source
    ? "source_unavailable"
    : sourceActionsBlocked
      ? "source_actions_blocked"
      : selectedIds.size === 0
        ? "no_selection"
        : gatewayOriginInvalid || providerIdInvalid
          ? "destination_invalid"
          : unboundSelectedCount > 0
            ? "unbound_selection"
            : null;

  const openKeyDialogDisabled = blockReason !== null;

  const buildRenderRequest = useCallback(
    (decision: KeyDecision) => {
      const ids = [...selectedIds].sort((a, b) => a - b);
      if (!normalizedOrigin || providerIdInvalid)
        throw new Error(renderFailedMessage);
      if (decision.mode === "manual" && decision.manualKey.trim() === "")
        throw new Error(renderFailedMessage);
      if (sourceActionsBlocked) throw new Error(renderFailedMessage);
      const selections: Record<
        number,
        { provider_id: string; model_id: string; api: string }
      > = {};
      for (const id of ids) {
        const model = source?.models.find((m) => m.model_config_id === id);
        const bound = model?.pi_selected ?? null;
        if (!bound || !model?.pi_binding_renderable) {
          throw new Error(renderFailedMessage);
        }
        selections[id] = {
          provider_id: bound.provider_id,
          model_id: bound.model_id,
          api: bound.api,
        };
      }
      return {
        expected_source_digest: source?.source_digest ?? "",
        model_config_ids: ids,
        base_url: normalizedOrigin,
        provider_id: normalizedProviderId,
        credential:
          decision.mode === "manual"
            ? { include: true, api_key: decision.manualKey.trim() }
            : { include: false },
        selections,
      };
    },
    [
      normalizedOrigin,
      normalizedProviderId,
      providerIdInvalid,
      renderFailedMessage,
      selectedIds,
      source,
      sourceActionsBlocked,
    ],
  );

  const handleGenerate = useCallback(
    async (decision: KeyDecision) => {
      setRenderError(null);
      setRenderStale(false);
      try {
        const response = await api.modelExport.renderModelExport(
          buildRenderRequest(decision),
        );
        setRenderResult(response);
      } catch (error) {
        const detail = error as {
          status?: number;
          message?: string;
        };
        if (detail.status === 409) {
          setRenderStale(true);
          void refetchSource();
        }
        setRenderError(detail.message ?? renderFailedMessage);
        throw error;
      }
    },
    [buildRenderRequest, refetchSource, renderFailedMessage],
  );

  const clearResult = useCallback(() => setRenderResult(null), []);
  const closeKeyDialog = useCallback(() => setKeyDialogOpen(false), []);
  const openKeyDialog = useCallback(() => {
    setRenderError(null);
    setRenderStale(false);
    setKeyDialogOpen(true);
  }, []);

  return {
    blockReason,
    clearResult,
    closeKeyDialog,
    gatewayOrigin,
    gatewayOriginInvalid,
    handleGenerate,
    keyDialogOpen,
    openKeyDialogDisabled,
    openKeyDialog,
    providerId,
    providerIdInvalid,
    renderableSelectedIds,
    renderError,
    renderResult,
    renderStale,
    setGatewayOrigin,
    setProviderId,
    unboundSelectedCount,
  };
}
