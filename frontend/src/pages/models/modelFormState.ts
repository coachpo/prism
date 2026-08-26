import type {
  ApiFamily,
  ModelConfig,
  ModelConfigListItem,
  OpenAIAcceptedFormat,
  OpenAIImageOperations,
} from "@/lib/types";
import type {
  ManagedModelConfigCreate,
  ManagedModelConfigUpdate,
} from "@/lib/api/models";

export type SubmitEventLike = Pick<Event, "preventDefault">;

export interface ModelFormData {
  api_family: ApiFamily;
  model_id: string;
  display_name: string;
  openai_accepted_format: OpenAIAcceptedFormat | "";
  openai_image_operations: OpenAIImageOperations | "";
  loadbalance_strategy_id: number | null;
  is_enabled: boolean;
  last_auto_display_name?: string | null;
}

export type ModelFormValidationError =
  | "api_family_required"
  | "model_id_required"
  | "openai_accepted_format_invalid"
  | "openai_image_operations_invalid"
  | "openai_capability_required"
  | "loadbalance_strategy_required";

const DEFAULT_API_FAMILY: ApiFamily = "openai";
export const DEFAULT_OPENAI_ACCEPTED_FORMAT: OpenAIAcceptedFormat =
  "dual_native";
export const OPENAI_ACCEPTED_FORMAT_OPTIONS: readonly OpenAIAcceptedFormat[] = [
  "dual_native",
  "responses_only",
  "chat_completions_only",
];
export const OPENAI_IMAGE_OPERATIONS_OPTIONS: readonly OpenAIImageOperations[] = [
  "generations",
  "edits",
  "generations_and_edits",
];

export const DEFAULT_MODEL_FORM_DATA: ModelFormData = {
  api_family: DEFAULT_API_FAMILY,
  model_id: "",
  display_name: "",
  openai_accepted_format: DEFAULT_OPENAI_ACCEPTED_FORMAT,
  openai_image_operations: "",
  loadbalance_strategy_id: null,
  is_enabled: false,
  last_auto_display_name: "",
};

export function resolveModelApiFamily(
  model:
    | Pick<ModelConfigListItem, "api_family">
    | Pick<ModelConfig, "api_family">,
): ApiFamily {
  return model.api_family;
}

function isOpenAIAcceptedFormat(
  value: unknown,
): value is OpenAIAcceptedFormat {
  return (
    value === "responses_only" ||
    value === "chat_completions_only" ||
    value === "dual_native"
  );
}

function isOpenAIImageOperations(
  value: unknown,
): value is OpenAIImageOperations {
  return (
    value === "generations" ||
    value === "edits" ||
    value === "generations_and_edits"
  );
}

/**
 * The image dimension has no default. A model that never declared image
 * support must stay without it, so an unrecognized value normalizes to "".
 */
export function normalizeOpenAIImageOperationsForForm(
  apiFamily: ApiFamily,
  value: unknown,
): OpenAIImageOperations | "" {
  if (apiFamily !== "openai") return "";
  return isOpenAIImageOperations(value) ? value : "";
}

/**
 * Landing on OpenAI preselects the canonical text default; leaving OpenAI
 * clears the dimension.
 */
export function normalizeOpenAIAcceptedFormatForForm(
  apiFamily: ApiFamily,
  value: unknown,
): OpenAIAcceptedFormat | "" {
  if (apiFamily !== "openai") return "";
  return isOpenAIAcceptedFormat(value)
    ? value
    : DEFAULT_OPENAI_ACCEPTED_FORMAT;
}

/** Persisted hydration preserves absence for pure image models. */
export function readOpenAIAcceptedFormatForForm(
  apiFamily: ApiFamily,
  value: unknown,
): OpenAIAcceptedFormat | "" {
  if (apiFamily !== "openai") return "";
  return isOpenAIAcceptedFormat(value) ? value : "";
}

function shouldAutoSyncDisplayName(formData: ModelFormData): boolean {
  const displayName = formData.display_name ?? "";
  return (
    displayName.trim() === "" ||
    displayName === (formData.last_auto_display_name ?? "")
  );
}

type EditableModelFormSource =
  | Pick<
      ModelConfig,
      | "api_family"
      | "model_id"
      | "display_name"
      | "openai_accepted_format"
      | "openai_image_operations"
      | "loadbalance_strategy_id"
      | "is_enabled"
    >
  | Pick<
      ModelConfigListItem,
      | "api_family"
      | "model_id"
      | "display_name"
      | "openai_accepted_format"
      | "openai_image_operations"
      | "loadbalance_strategy_id"
      | "is_enabled"
    >;

export function createEditModelFormData(
  model: EditableModelFormSource,
): ModelFormData {
  const displayName = model.display_name || "";
  const apiFamily = resolveModelApiFamily(model);
  return {
    api_family: apiFamily,
    model_id: model.model_id,
    display_name: displayName,
    openai_accepted_format: readOpenAIAcceptedFormatForForm(
      apiFamily,
      model.openai_accepted_format,
    ),
    openai_image_operations: normalizeOpenAIImageOperationsForForm(
      apiFamily,
      model.openai_image_operations,
    ),
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    is_enabled: model.is_enabled,
    last_auto_display_name:
      displayName === model.model_id ? model.model_id : displayName,
  };
}

export function createNewModelFormData(
  loadbalanceStrategyId: number | null,
): ModelFormData {
  return {
    api_family: DEFAULT_MODEL_FORM_DATA.api_family,
    model_id: DEFAULT_MODEL_FORM_DATA.model_id,
    display_name: DEFAULT_MODEL_FORM_DATA.display_name,
    openai_accepted_format: DEFAULT_MODEL_FORM_DATA.openai_accepted_format,
    openai_image_operations: DEFAULT_MODEL_FORM_DATA.openai_image_operations,
    loadbalance_strategy_id: loadbalanceStrategyId,
    is_enabled: DEFAULT_MODEL_FORM_DATA.is_enabled,
    last_auto_display_name: DEFAULT_MODEL_FORM_DATA.last_auto_display_name,
  };
}

export function validateModelFormData(
  formData: ModelFormData,
): ModelFormValidationError | null {
  if (!formData.api_family) return "api_family_required";
  if (formData.model_id.trim() === "") return "model_id_required";
  if (
    formData.api_family === "openai" &&
    formData.openai_accepted_format !== "" &&
    formData.openai_accepted_format !== undefined &&
    !isOpenAIAcceptedFormat(formData.openai_accepted_format)
  ) {
    return "openai_accepted_format_invalid";
  }
  if (
    formData.api_family === "openai" &&
    formData.openai_image_operations !== "" &&
    formData.openai_image_operations !== undefined &&
    !isOpenAIImageOperations(formData.openai_image_operations)
  ) {
    return "openai_image_operations_invalid";
  }
  if (
    formData.api_family === "openai" &&
    !isOpenAIAcceptedFormat(formData.openai_accepted_format) &&
    !isOpenAIImageOperations(formData.openai_image_operations)
  ) {
    return "openai_capability_required";
  }
  if (formData.loadbalance_strategy_id === null) {
    return "loadbalance_strategy_required";
  }
  return null;
}

function getRequiredLoadbalanceStrategyId(formData: ModelFormData): number {
  if (formData.loadbalance_strategy_id === null) {
    throw new Error("loadbalance_strategy_id is required");
  }
  return formData.loadbalance_strategy_id;
}

function getNormalizedRoutingState(formData: ModelFormData) {
  return {
    loadbalance_strategy_id: getRequiredLoadbalanceStrategyId(formData),
  };
}

function getNormalizedOpenAIState(formData: ModelFormData) {
  if (formData.api_family !== "openai") return {};
  const openAIAcceptedFormat = isOpenAIAcceptedFormat(
    formData.openai_accepted_format,
  )
    ? formData.openai_accepted_format
    : null;
  const openAIImageOperations = normalizeOpenAIImageOperationsForForm(
    formData.api_family,
    formData.openai_image_operations,
  );
  if (openAIAcceptedFormat === null && openAIImageOperations === "") {
    throw new Error("at least one OpenAI capability dimension is required");
  }
  return {
    openai_accepted_format: openAIAcceptedFormat,
    openai_image_operations:
      openAIImageOperations === "" ? null : openAIImageOperations,
  };
}

export function toModelCreatePayload(
  formData: ModelFormData,
): ManagedModelConfigCreate {
  const normalizedDisplayName =
    formData.display_name?.trim() || formData.model_id.trim();
  return {
    api_family: formData.api_family,
    model_id: formData.model_id,
    display_name: normalizedDisplayName,
    is_enabled: formData.is_enabled,
    ...getNormalizedOpenAIState(formData),
    ...getNormalizedRoutingState(formData),
  };
}

export function toModelUpdatePayload(
  formData: ModelFormData,
): ManagedModelConfigUpdate {
  return {
    api_family: formData.api_family,
    display_name: formData.display_name || null,
    model_id: formData.model_id,
    is_enabled: formData.is_enabled,
    ...getNormalizedOpenAIState(formData),
    ...getNormalizedRoutingState(formData),
  };
}

export function setLoadbalanceStrategyIdOnForm(
  formData: ModelFormData,
  strategyId: number | null,
): ModelFormData {
  return { ...formData, loadbalance_strategy_id: strategyId };
}

export function setApiFamilyOnForm(
  formData: ModelFormData,
  apiFamily: ApiFamily,
): ModelFormData {
  // Re-selecting the current family must not re-add a text mode deliberately
  // cleared on an image-only model.
  const enteringOpenAI =
    apiFamily === "openai" && formData.api_family !== "openai";
  const openAIAcceptedFormat = enteringOpenAI
    ? normalizeOpenAIAcceptedFormatForForm(
        apiFamily,
        formData.openai_accepted_format,
      )
    : readOpenAIAcceptedFormatForForm(
        apiFamily,
        formData.openai_accepted_format,
      );
  const openAIImageOperations = normalizeOpenAIImageOperationsForForm(
    apiFamily,
    formData.openai_image_operations,
  );
  if (
    formData.api_family === apiFamily &&
    formData.openai_accepted_format === openAIAcceptedFormat &&
    formData.openai_image_operations === openAIImageOperations
  ) {
    return formData;
  }
  return {
    ...formData,
    api_family: apiFamily,
    openai_accepted_format: openAIAcceptedFormat,
    openai_image_operations: openAIImageOperations,
  };
}

export function setOpenAIImageOperationsOnForm(
  formData: ModelFormData,
  imageOperations: OpenAIImageOperations | "",
): ModelFormData {
  return {
    ...formData,
    openai_image_operations: normalizeOpenAIImageOperationsForForm(
      formData.api_family,
      imageOperations,
    ),
  };
}

export function setOpenAIAcceptedFormatOnForm(
  formData: ModelFormData,
  acceptedFormat: OpenAIAcceptedFormat | "",
): ModelFormData {
  return {
    ...formData,
    openai_accepted_format: readOpenAIAcceptedFormatForForm(
      formData.api_family,
      acceptedFormat,
    ),
  };
}

export function setModelIdOnForm(
  formData: ModelFormData,
  modelId: string,
): ModelFormData {
  const autoDisplayName = modelId;
  return {
    ...formData,
    model_id: modelId,
    display_name: shouldAutoSyncDisplayName(formData)
      ? autoDisplayName
      : formData.display_name,
    last_auto_display_name: autoDisplayName,
  };
}

export function setDisplayNameOnForm(
  formData: ModelFormData,
  displayName: string,
): ModelFormData {
  return { ...formData, display_name: displayName };
}
