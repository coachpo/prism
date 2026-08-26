import type {
  ApiFamily,
  Connection,
  ModelAccessTarget,
  ModelConfig,
  ModelConfigListItem,
} from "@/lib/types";
import {
  getTerminalTarget,
  getTerminalTargetId,
  isTerminalTargetAccessTargetType,
} from "@/lib/types/target-compatibility";

type ApiFamilyModelOption = {
  api_family: ApiFamily;
  model_id: string;
  is_enabled?: boolean;
};

export function getAccessTargetModelsForApiFamily<
  T extends ApiFamilyModelOption,
>(models: T[], apiFamily: ApiFamily, excludedModelId?: string): T[] {
  const normalizedExcludedModelId = excludedModelId?.trim() ?? "";
  return models.filter(
    (model) =>
      model.api_family === apiFamily &&
      (normalizedExcludedModelId === "" ||
        model.model_id !== normalizedExcludedModelId) &&
      model.is_enabled !== false,
  );
}

export function getModelConnections(
  model:
    | Pick<ModelConfig, "access_targets">
    | Pick<ModelConfigListItem, "access_targets">,
): Connection[] {
  return model.access_targets
    .filter(
      (target) =>
        target.target_type === "connection" && target.connection,
    )
    .sort((left, right) => left.position - right.position)
    .map((target) => ({
      ...(target.connection as Connection),
      priority: target.position,
    }));
}

export function getEditModelConnectionOptions(
  model:
    | Pick<ModelConfig, "access_targets">
    | Pick<ModelConfigListItem, "access_targets">
    | null,
): Connection[] {
  return model ? getModelConnections(model) : [];
}

export function connectionBelongsToModel(
  connection: Pick<Connection, "model_config_id"> | null | undefined,
  modelConfigId: number | undefined,
): boolean {
  if (!connection || !Number.isFinite(modelConfigId)) return false;
  return (
    connection.model_config_id == null ||
    connection.model_config_id === modelConfigId
  );
}

export function getOwnedConnectionTarget(
  model: Pick<ModelConfig, "access_targets"> | null | undefined,
  modelConfigId: number | undefined,
  connectionId: number,
): ModelAccessTarget | null {
  if (!model || !Number.isFinite(modelConfigId)) return null;

  const target = model.access_targets.find(
    (candidate) =>
      isTerminalTargetAccessTargetType(candidate.target_type) &&
      getTerminalTargetId(candidate) === connectionId,
  );
  if (!target) return null;

  const terminalTarget = getTerminalTarget(target);
  return !terminalTarget ||
    connectionBelongsToModel(terminalTarget, modelConfigId)
    ? target
    : null;
}

export function isOwnedConnectionTarget(
  model: Pick<ModelConfig, "access_targets"> | null | undefined,
  modelConfigId: number | undefined,
  connectionId: number,
): boolean {
  return getOwnedConnectionTarget(model, modelConfigId, connectionId) !== null;
}

export function getSameFamilyConnections(
  connections: Connection[],
  apiFamily: ApiFamily,
  modelConfigId?: number,
): Connection[] {
  return connections.filter(
    (connection) =>
      connection.api_family === apiFamily &&
      connectionBelongsToModel(connection, modelConfigId),
  );
}

export function getOwnedModelConnections(
  model: Pick<ModelConfig, "access_targets">,
  modelConfigId: number | undefined,
): Connection[] {
  return getModelConnections(model).filter((connection) =>
    connectionBelongsToModel(connection, modelConfigId),
  );
}

export interface AccessTargetSummary {
  totalTargetCount: number;
  enabledTargetCount: number;
  totalModelTargetCount: number;
  totalTerminalTargetCount: number;
  enabledModelFallbackTargetCount: number;
  enabledTerminalTargetCount: number;
}

export function buildAccessTargetSummary(
  model: ModelConfig | null,
): AccessTargetSummary {
  const targets = model?.access_targets ?? [];
  const enabledTargets = targets.filter((target) => target.is_enabled);
  const modelTargets = targets.filter(
    (target) => target.target_type === "model",
  );
  const terminalTargets = targets.filter((target) =>
    isTerminalTargetAccessTargetType(target.target_type),
  );
  const enabledModelFallbackTargets = enabledTargets.filter(
    (target) => target.target_type === "model",
  );
  const enabledTerminalTargets = enabledTargets.filter((target) =>
    isTerminalTargetAccessTargetType(target.target_type),
  );

  return {
    totalTargetCount: targets.length,
    enabledTargetCount: enabledTargets.length,
    totalModelTargetCount: modelTargets.length,
    totalTerminalTargetCount: terminalTargets.length,
    enabledModelFallbackTargetCount: enabledModelFallbackTargets.length,
    enabledTerminalTargetCount: enabledTerminalTargets.length,
  };
}
