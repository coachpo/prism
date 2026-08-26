import type {
  ModelAccessTarget,
  ModelAccessTargetModelMutation,
  ModelAccessTargetMutation,
} from "@/lib/types";

/** The editor's draft is one mixed Model Target/Terminal Target sequence. */
export function sortAccessTargetsByPositionThenId(
  targets: readonly ModelAccessTarget[] | null | undefined,
): ModelAccessTarget[] {
  return [...(targets ?? [])].sort(
    (left, right) => left.position - right.position || left.id - right.id,
  );
}

export interface IndexedModelAccessTargetMutation {
  sourceIndex: number;
  target: ModelAccessTargetModelMutation;
}

export interface IndexedConnectionAccessTargetMutation {
  sourceIndex: number;
  target: Extract<ModelAccessTargetMutation, { target_type: "connection" }>;
}

export function isModelAccessTargetMutation(
  target: ModelAccessTargetMutation,
): target is ModelAccessTargetModelMutation {
  return target.target_type === "model";
}

export function getIndexedModelAccessTargets(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): IndexedModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations(targets).flatMap(
    (target, sourceIndex) => {
      if (!isModelAccessTargetMutation(target)) return [];
      return [{ sourceIndex, target }];
    },
  );
}

export function getIndexedConnectionAccessTargets(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): IndexedConnectionAccessTargetMutation[] {
  return normalizeAccessTargetMutations(targets).flatMap(
    (target, sourceIndex) => {
      if (target.target_type !== "connection") return [];
      return [{ sourceIndex, target }];
    },
  );
}

export function accessTargetKey(
  target: Pick<
    ModelAccessTargetMutation,
    "target_type" | "target_model_id" | "connection_id"
  >,
): string | null {
  if (target.target_type === "model" && target.target_model_id?.trim()) {
    return `model:${target.target_model_id.trim()}`;
  }
  if (
    target.target_type === "connection" &&
    typeof target.connection_id === "number"
  ) {
    return `connection:${target.connection_id}`;
  }
  return null;
}

export function accessTargetToMutation(
  target: ModelAccessTarget,
): ModelAccessTargetMutation | null {
  if (target.target_type === "model" && target.target_model_id) {
    return {
      target_type: "model",
      target_model_id: target.target_model_id,
      position: target.position,
      is_enabled: target.is_enabled,
    };
  }
  if (target.target_type === "connection" && target.connection_id !== null) {
    return {
      target_type: "connection",
      connection_id: target.connection_id,
      position: target.position,
      is_enabled: target.is_enabled,
    };
  }
  return null;
}

export function normalizeAccessTargetMutations(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): ModelAccessTargetMutation[] {
  const seen = new Set<string>();
  const normalized: ModelAccessTargetMutation[] = [];
  for (const target of targets ?? []) {
    const key = accessTargetKey(target);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    if (target.target_type === "model") {
      normalized.push({
        target_type: "model",
        target_model_id: target.target_model_id.trim(),
        position: normalized.length,
        is_enabled: target.is_enabled ?? true,
      });
    } else {
      normalized.push({
        target_type: "connection",
        connection_id: target.connection_id,
        position: normalized.length,
        is_enabled: target.is_enabled ?? true,
      });
    }
  }
  return normalized;
}

export function moveAccessTarget(
  targets: ModelAccessTargetMutation[],
  fromIndex: number,
  toIndex: number,
): ModelAccessTargetMutation[] {
  const normalized = normalizeAccessTargetMutations(targets);
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= normalized.length ||
    toIndex >= normalized.length ||
    fromIndex === toIndex
  ) {
    return normalized;
  }
  const nextTargets = [...normalized];
  const [movedTarget] = nextTargets.splice(fromIndex, 1);
  if (!movedTarget) return normalized;
  nextTargets.splice(toIndex, 0, movedTarget);
  return normalizeAccessTargetMutations(nextTargets);
}

export function appendAccessTarget(
  targets: ModelAccessTargetMutation[],
  target: Omit<ModelAccessTargetMutation, "position">,
): ModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations([
    ...normalizeAccessTargetMutations(targets),
    { ...target, position: targets.length } as ModelAccessTargetMutation,
  ]);
}

export function removeAccessTarget(
  targets: ModelAccessTargetMutation[],
  index: number,
): ModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations(
    normalizeAccessTargetMutations(targets).filter(
      (_, currentIndex) => currentIndex !== index,
    ),
  );
}

export function setAccessTargetEnabled(
  targets: ModelAccessTargetMutation[],
  index: number,
  isEnabled: boolean,
): ModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations(
    normalizeAccessTargetMutations(targets).map((target, currentIndex) =>
      currentIndex === index ? { ...target, is_enabled: isEnabled } : target,
    ),
  );
}

export function normalizeModelAccessTargetMutations(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): ModelAccessTargetModelMutation[] {
  return normalizeAccessTargetMutations(targets)
    .filter(
      (target): target is ModelAccessTargetModelMutation =>
        target.target_type === "model",
    )
    .map((target, position) => ({ ...target, position }));
}
