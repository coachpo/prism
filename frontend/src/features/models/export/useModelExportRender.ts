import { useCallback, useMemo, useState } from "react";
import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin";
import { renderModelExport } from "@/lib/api/modelExport";
import type { ExportRenderResponse, ExportSourceResponse } from "./exportTypes";
import type { KeyDecision } from "./ExportKeyDialog";

const DEFAULT_PROVIDER_ID = "prism";

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
}

export function useModelExportRender({
  refetchSource,
  renderFailedMessage,
  selectedIds,
  source,
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

  // Render only ever trusts a persisted binding; a selected model whose
  // binding is not healthy (unbound or drifted) cannot be rendered yet.
  const hasUnboundSelection = useMemo(() => {
    if (!source) return false;
    for (const id of selectedIds) {
      const model = source.models.find((m) => m.model_config_id === id);
      if (!model) continue;
      if (!model.pi_selected || model.pi_binding_status !== "bound")
        return true;
    }
    return false;
  }, [selectedIds, source]);

  const openKeyDialogDisabled =
    selectedIds.size === 0 ||
    !source ||
    gatewayOriginInvalid ||
    providerIdInvalid ||
    hasUnboundSelection;

  const buildRenderRequest = useCallback(
    (decision: KeyDecision) => {
      const ids = [...selectedIds].sort((a, b) => a - b);
      if (!normalizedOrigin || providerIdInvalid)
        throw new Error("invalid export destination");
      const selections: Record<
        number,
        { provider_id: string; model_id: string; api: string } | null
      > = {};
      for (const id of ids) {
        const model = source?.models.find((m) => m.model_config_id === id);
        const bound = model?.pi_selected ?? null;
        if (bound)
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
    [normalizedOrigin, normalizedProviderId, providerIdInvalid, selectedIds, source],
  );

  const handleGenerate = useCallback(
    async (decision: KeyDecision) => {
      setRenderError(null);
      setRenderStale(false);
      try {
        const response = await renderModelExport(buildRenderRequest(decision));
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

  return {
    clearResult,
    gatewayOrigin,
    gatewayOriginInvalid,
    handleGenerate,
    hasUnboundSelection,
    keyDialogOpen,
    openKeyDialogDisabled,
    providerId,
    providerIdInvalid,
    renderError,
    renderResult,
    renderStale,
    setGatewayOrigin,
    setKeyDialogOpen,
    setProviderId,
  };
}
