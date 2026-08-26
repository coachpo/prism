import { useCallback, useMemo, useState } from "react";

import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin";
import {
  renderModelExport,
  type ExportRenderRequestBody,
} from "@/lib/api/modelExport";
import type {
  ExportPlatform,
  ExportRenderResponse,
  ExportSourceResponse,
  ManualEnhancementWire,
} from "./exportTypes";
import type { KeyDecision } from "./PlatformKeyDialog";
import type { EnhancementDraft } from "./useModelExportUploadReview";

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
  defaultModelConfigId?: number;
  enhancements: Record<number, EnhancementDraft>;
  platform: ExportPlatform;
  refetchSource: () => unknown;
  renderFailedMessage: string;
  selectedIds: ReadonlySet<number>;
  selectedCount: number;
  source: ExportSourceResponse | undefined;
}

export function useModelExportRender({
  defaultModelConfigId,
  enhancements,
  platform,
  refetchSource,
  renderFailedMessage,
  selectedIds,
  selectedCount,
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

  const buildRenderRequest = useCallback(
    (decision: KeyDecision): ExportRenderRequestBody => {
      const ids = [...selectedIds].sort((a, b) => a - b);
      const enhancementWires: Record<number, ManualEnhancementWire | null> = {};
      for (const id of ids) {
        const draft = enhancements[id];
        enhancementWires[id] = draft
          ? { fields: draft.fields, override_fields: draft.overrideFields }
          : null;
      }
      if (!normalizedOrigin || providerIdInvalid) {
        throw new Error("invalid export destination");
      }
      return {
        expected_source_digest: source?.source_digest ?? "",
        model_config_ids: ids,
        base_url: normalizedOrigin,
        provider_id: normalizedProviderId,
        enhancements: enhancementWires,
        credential:
          decision.mode === "manual"
            ? { include: true, api_key: decision.manualKey.trim() }
            : { include: false },
        ...(platform === "opencode" && defaultModelConfigId !== undefined
          ? { default_model_config_id: defaultModelConfigId }
          : {}),
      };
    },
    [
      defaultModelConfigId,
      enhancements,
      normalizedOrigin,
      normalizedProviderId,
      platform,
      providerIdInvalid,
      selectedIds,
      source?.source_digest,
    ],
  );

  const handleGenerate = useCallback(
    async (decision: KeyDecision) => {
      setRenderError(null);
      setRenderStale(false);
      try {
        const response = await renderModelExport(
          buildRenderRequest(decision),
          platform,
        );
        setRenderResult(response);
      } catch (error) {
        const detail = error as {
          status?: number;
          code?: string;
          message?: string;
        };
        if (detail.code === "export_source_stale" || detail.status === 409) {
          setRenderStale(true);
          void refetchSource();
        }
        setRenderError(detail.message ?? renderFailedMessage);
        throw error;
      }
    }, [buildRenderRequest, platform, refetchSource, renderFailedMessage],
  );

  const resetForPlatform = useCallback(() => {
    setRenderResult(null);
    setRenderError(null);
    setRenderStale(false);
    setKeyDialogOpen(false);
  }, []);

  const clearResult = useCallback(() => {
    setRenderResult(null);
  }, []);

  return {
    clearResult,
    gatewayOrigin,
    gatewayOriginInvalid,
    handleGenerate,
    keyDialogOpen,
    normalizedProviderId,
    openKeyDialogDisabled:
      selectedCount === 0 ||
      !source ||
      gatewayOriginInvalid ||
      providerIdInvalid,
    providerId,
    providerIdInvalid,
    renderError,
    renderResult,
    renderStale,
    resetForPlatform,
    setGatewayOrigin,
    setKeyDialogOpen,
    setProviderId,
  };
}
