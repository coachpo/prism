import { useCallback, useState } from "react";
import { api } from "@/lib/api";
import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin";
import type { OpenCodeExportSourceResponse } from "@/lib/types";
import type { KeyDecision } from "./ExportKeyDialog";
import { normalizeGatewayOrigin } from "./exportDestination";
import { useExportRenderSession } from "./useExportRenderSession";

export function useOpenCodeExportRender({
  source,
  selectedIds,
  sourceActionsBlocked,
  refetchSource,
  renderFailedMessage,
}: {
  source?: OpenCodeExportSourceResponse;
  selectedIds: ReadonlySet<number>;
  sourceActionsBlocked: boolean;
  refetchSource: () => unknown;
  renderFailedMessage: string;
}) {
  const [gatewayOrigin, setGatewayOrigin] = useState(
    () => getEffectiveBackendOrigin().origin,
  );
  const [providerId, setProviderId] = useState("prism");
  const normalizedOrigin = normalizeGatewayOrigin(gatewayOrigin);
  const providerIdInvalid = !/^prism(?:-[a-z0-9][a-z0-9_-]*)?$/.test(
    providerId.trim(),
  );
  const gatewayOriginInvalid = normalizedOrigin === null;
  const selectionInvalid =
    selectedIds.size === 0 ||
    [...selectedIds].some(
      (id) =>
        !source?.models.some(
          (model) =>
            model.model_config_id === id &&
            model.selectable &&
            model.direct_request_enabled &&
            model.is_enabled,
        ),
    );
  const executeRender = useCallback(
    (decision: KeyDecision, signal: AbortSignal) => {
      if (
        !source ||
        sourceActionsBlocked ||
        selectionInvalid ||
        !normalizedOrigin ||
        providerIdInvalid ||
        (decision.mode === "manual" && !decision.manualKey.trim())
      ) {
        throw new Error(renderFailedMessage);
      }
      return api.opencodeExport.renderOpenCodeExport(
        {
          expected_source_digest: source.source_digest,
          model_config_ids: [...selectedIds].sort((a, b) => a - b),
          base_url: normalizedOrigin,
          provider_id: providerId.trim(),
          credential:
            decision.mode === "manual"
              ? { include: true, api_key: decision.manualKey.trim() }
              : { include: false },
        },
        signal,
      );
    },
    [
      source,
      sourceActionsBlocked,
      selectionInvalid,
      normalizedOrigin,
      providerIdInvalid,
      renderFailedMessage,
      selectedIds,
      providerId,
    ],
  );
  const session = useExportRenderSession({
    render: executeRender,
    refetchSource,
    renderFailedMessage,
  });

  return {
    ...session,
    gatewayOrigin,
    setGatewayOrigin,
    gatewayOriginInvalid,
    providerId,
    setProviderId,
    providerIdInvalid,
    openKeyDialogDisabled:
      sourceActionsBlocked ||
      selectionInvalid ||
      gatewayOriginInvalid ||
      providerIdInvalid,
  };
}
