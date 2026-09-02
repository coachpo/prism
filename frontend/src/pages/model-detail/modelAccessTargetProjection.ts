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

/**
 * Read-only projection of the upstream identities held by this entry model's
 * DIRECT Terminal Targets. Model Target rows are logical edges and never
 * contribute identities, and the projection never follows them recursively —
 * the detail summary answers "how many distinct exits", not "the full exit
 * graph".
 *
 * Identity comparison against the entry `model_id` is exact and case-sensitive:
 * provider-facing strings, so `Entry-A` and `entry-a` are different upstream
 * identities. A Terminal Target without a readable identity is unknown
 * evidence — it counts as neither consistent nor decoupled.
 */
export interface UpstreamIdentitySummary {
  hasDirectTerminalTargets: boolean;
  /** Known identities only: missing/blank upstream values are excluded. */
  distinctUpstreamModelIdCount: number;
  /** Known identities that differ from the entry `model_id` exactly. */
  decoupledUpstreamModelIdCount: number;
  /** Direct Terminal Targets carrying no readable upstream identity. */
  unknownUpstreamModelIdCount: number;
}

function knownUpstreamModelId(target: ModelAccessTarget): string | null {
  const upstreamModelId = getTerminalTarget(target)?.upstream_model_id;
  return upstreamModelId?.trim() ? upstreamModelId : null;
}

export function buildUpstreamIdentitySummary(
  model: Pick<ModelConfig, "model_id" | "access_targets"> | null,
): UpstreamIdentitySummary {
  const entryModelId = model?.model_id ?? null;
  const terminalTargets = (model?.access_targets ?? []).filter((target) =>
    isTerminalTargetAccessTargetType(target.target_type),
  );

  if (!entryModelId || terminalTargets.length === 0) {
    return {
      hasDirectTerminalTargets: false,
      distinctUpstreamModelIdCount: 0,
      decoupledUpstreamModelIdCount: 0,
      unknownUpstreamModelIdCount: 0,
    };
  }

  const knownIdentities = new Set<string>();
  let decoupledCount = 0;
  let unknownCount = 0;
  for (const target of terminalTargets) {
    const upstreamModelId = knownUpstreamModelId(target);
    if (upstreamModelId === null) {
      unknownCount += 1;
      continue;
    }
    knownIdentities.add(upstreamModelId);
    if (upstreamModelId !== entryModelId) decoupledCount += 1;
  }

  return {
    hasDirectTerminalTargets: true,
    distinctUpstreamModelIdCount: knownIdentities.size,
    decoupledUpstreamModelIdCount: decoupledCount,
    unknownUpstreamModelIdCount: unknownCount,
  };
}
