import { useCallback } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/request";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type { ApiFamily, Connection, ModelConfig } from "@/lib/types";
import {
  isOwnedConnectionTarget,
} from "./modelAccessTargetProjection";
import { buildConnectionUpdatePayload } from "./connectionDataSupport";
import type { ConnectionSubmitPreparationInput } from "./connectionSubmitPreparation";
import {
  prepareConnectionSubmit,
} from "./connectionSubmitPreparation";
import type { CommitModelDetailConnection } from "./useModelDetailConnectionReconciliation";

const TERMINAL_TARGET_OWNER_MISMATCH =
  "Terminal Target owner does not match the current model";

interface UseModelDetailConnectionSubmitInput
  extends Omit<
    ConnectionSubmitPreparationInput,
    "apiFamily" | "editingConnection"
  > {
  id: string | undefined;
  revision: number;
  model: ModelConfig | null;
  modelApiFamily: ApiFamily | null;
  editingConnection: Connection | null;
  setRoutingScheduleError: (
    error: import("./routingScheduleDraft").RoutingScheduleDraftError | null,
  ) => void;
  setCustomRequestParametersError: (
    error:
      | import("./customRequestParameters").CustomRequestParametersParseError
      | null,
  ) => void;
  setIsConnectionDialogOpen: (open: boolean) => void;
  applyTargets: (targets: import("@/lib/types").ModelAccessTarget[]) => void;
  commitConnection: CommitModelDetailConnection;
}

function isCustomRequestParametersValidationError(
  error: unknown,
): error is ApiError {
  if (!(error instanceof ApiError) || error.status !== 422) return false;
  const detail = error.detail;
  return Boolean(
    detail &&
      typeof detail === "object" &&
      (detail as { detail?: unknown }).detail ===
        "Invalid custom request parameters",
  );
}

function customRequestParametersErrorFromServerBody(
  body: Record<string, unknown>,
) {
  const reason =
    typeof body.reason === "string"
      ? (body.reason as import("./customRequestParameters").CustomRequestParametersParseError["reason"])
      : "not_object";
  return {
    reason,
    path:
      typeof body.path === "string"
        ? body.path
        : "custom_request_parameters",
    limit: typeof body.limit === "number" ? body.limit : undefined,
  };
}

export function useModelDetailConnectionSubmit({
  id,
  revision,
  model,
  modelApiFamily,
  createMode,
  selectedEndpointId,
  newEndpointForm,
  connectionForm,
  headerRows,
  customRequestParametersDraft,
  setCustomRequestParametersError,
  editingConnection,
  endpointSourceDefaultName,
  routingScheduleDraft,
  setRoutingScheduleError,
  setIsConnectionDialogOpen,
  applyTargets,
  commitConnection,
}: UseModelDetailConnectionSubmitInput) {
  const modelConfigId = id ? Number.parseInt(id, 10) : NaN;

  const handleConnectionSubmit = useCallback(
    async (event: Pick<Event, "preventDefault">) => {
      event.preventDefault();
      if (!id || !Number.isFinite(modelConfigId)) return;

      const preparation = prepareConnectionSubmit({
        apiFamily: modelApiFamily,
        createMode,
        selectedEndpointId,
        newEndpointForm,
        connectionForm,
        headerRows,
        customRequestParametersDraft,
        routingScheduleDraft,
        editingConnection,
        endpointSourceDefaultName,
      });
      if (preparation.kind === "custom_request_parameters_error") {
        setCustomRequestParametersError(preparation.error);
        return;
      }
      setCustomRequestParametersError(null);
      if (preparation.kind === "routing_schedule_error") {
        setRoutingScheduleError(preparation.error);
        return;
      }
      setRoutingScheduleError(null);
      if (preparation.kind === "payload_error") {
        if (preparation.errorMessage) toast.error(preparation.errorMessage);
        return;
      }

      try {
        if (editingConnection) {
          if (
            !isOwnedConnectionTarget(
              model,
              modelConfigId,
              editingConnection.id,
            )
          ) {
            toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
            return;
          }
          const updatedResponse = await api.models.connections.update(
            modelConfigId,
            editingConnection.id,
            buildConnectionUpdatePayload(
              preparation.payload,
              editingConnection,
            ),
          );
          if (!commitConnection(updatedResponse.connection)) return;
          toast.success(getStaticMessages().modelDetailData.connectionUpdated);
        } else {
          const createdResponse = await api.models.connections.create(
            modelConfigId,
            preparation.payload,
          );
          if (!commitConnection(createdResponse.connection)) return;
          const targets = await api.models.targets.list(modelConfigId);
          applyTargets(targets);
          toast.success(getStaticMessages().modelDetailData.connectionCreated);
        }
        clearSharedReferenceData(undefined, revision);
        setIsConnectionDialogOpen(false);
      } catch (error) {
        if (isRoutingScheduleValidationError(error)) {
          const body = error.detail as Record<string, unknown>;
          setRoutingScheduleError({
            reason: String(body.reason ?? "") as import("./routingScheduleDraft").RoutingScheduleDraftError["reason"],
            windowIndex:
              typeof body.index === "number" ? body.index : undefined,
          });
          return;
        }
        if (isCustomRequestParametersValidationError(error)) {
          setCustomRequestParametersError(
            customRequestParametersErrorFromServerBody(
              error.detail as Record<string, unknown>,
            ),
          );
          return;
        }
        toast.error(
          error instanceof Error
            ? error.message
            : getStaticMessages().modelDetailData.saveConnectionFailed,
        );
      }
    },
    [
      applyTargets,
      commitConnection,
      connectionForm,
      createMode,
      customRequestParametersDraft,
      editingConnection,
      endpointSourceDefaultName,
      headerRows,
      id,
      model,
      modelApiFamily,
      modelConfigId,
      newEndpointForm,
      routingScheduleDraft,
      revision,
      selectedEndpointId,
      setCustomRequestParametersError,
      setIsConnectionDialogOpen,
      setRoutingScheduleError,
    ],
  );

  return { handleConnectionSubmit };
}

function isRoutingScheduleValidationError(
  error: unknown,
): error is ApiError {
  if (!(error instanceof ApiError)) return false;
  const detail = error.detail as Record<string, unknown> | undefined;
  return Boolean(detail && detail.field === "routing_schedule");
}
