import type {
  ModelCatalogResponse,
  PiModelReadResponse,
} from "../../src/lib/types";

/** Honest models.dev read for journeys that do not exercise catalog actions. */
export function createUnboundModelsDevCatalog(): ModelCatalogResponse {
  return {
    bound: false,
    source: null,
    override: null,
    effective: null,
    auto_match: null,
  };
}

/**
 * Honest independent pi.dev read for model-detail journeys whose concern is
 * unrelated to catalog availability. The unavailable live directory never
 * fabricates a binding and keeps all Pi actions inert.
 */
export function createUnavailablePiModelRead({
  modelConfigId,
  modelId,
  apiFamily = "openai",
  piApi = "openai-responses",
}: {
  modelConfigId: number;
  modelId: string;
  apiFamily?: string;
  piApi?: string;
}): PiModelReadResponse {
  return {
    model: {
      model_config_id: modelConfigId,
      model_id: modelId,
      api_family: apiFamily,
      pi_api: piApi,
    },
    catalog: { status: "unavailable" },
    candidate_status: "catalog_unavailable",
    candidates: [],
    binding_status: "unbound",
    binding_renderable: false,
    binding: {
      bound: false,
      source: null,
      override: null,
      effective: null,
    },
  };
}
