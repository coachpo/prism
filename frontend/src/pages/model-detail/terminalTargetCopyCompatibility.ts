import type {
  ModelConfigListItem,
  OpenAIImageCapability,
  OpenAIImageOperations,
  OpenAITextCapability,
} from "@/lib/types";

function imageOperationSet(
  capability: OpenAIImageOperations | null,
): ReadonlySet<"generations" | "edits"> {
  if (capability === "generations_and_edits") {
    return new Set(["generations", "edits"]);
  }
  return capability ? new Set([capability]) : new Set();
}

/** Mirrors the backend's strict text equality plus image containment rule. */
export function canCopyTerminalTargetToModel(
  model: Pick<
    ModelConfigListItem,
    "api_family" | "openai_accepted_format" | "openai_image_operations"
  >,
  sourceTextCapability: OpenAITextCapability | null,
  sourceImageCapability: OpenAIImageCapability | null,
): boolean {
  if (model.api_family !== "openai") return true;
  if (model.openai_accepted_format !== sourceTextCapability) return false;
  const required = imageOperationSet(model.openai_image_operations);
  const supported = imageOperationSet(sourceImageCapability);
  return [...required].every((operation) => supported.has(operation));
}
