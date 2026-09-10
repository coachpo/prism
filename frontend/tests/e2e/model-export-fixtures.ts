import type { Page } from "@playwright/test";
import { openCodeRenderPayload, openCodeSource } from "./opencode-export-fixtures";

function sourceModel(overrides: Record<string, unknown> = {}) {
  const row = {
    model_config_id: 3,
    model_id: "gpt-x",
    api_family: "openai",
    display_name: "gpt-x",
    is_enabled: true,
    direct_request_enabled: true,
    selectable: true,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    prism_metadata: {},
    merged_metadata: { name: "gpt-x" },
    metadata_provenance: {},
    missing_metadata: [],
    completeness: { metadata_fields: { name: true }, cost_exportable: true },
    targets: [
      {
        terminal_target_id: 11,
        position: 0,
        endpoint_id: 21,
        endpoint_name: "primary",
        openai_text_capability: "dual_native",
        pricing: {
          terminal_target_id: 11,
          template_kind: "standard",
          currency_code: "USD",
          pricing_unit: "PER_1M",
          card: {
            input_price: "3",
            output_price: "15",
            cached_input_price: "0.3",
            cache_creation_price: "3.75",
            reasoning_price: "15",
          },
        },
      },
    ],
    price_risk: { exportable: true },
    pi_api: "openai-responses",
    pi_candidates: [
      {
        provider_id: "openai",
        model_id: "gpt-x",
        api: "openai-responses",
        name: "GPT X",
      },
    ],
    candidate_status: "single",
    pi_selected: null,
    pi_binding_status: "unbound",
    pi_binding_renderable: false,
    ...overrides,
  };
  return { ...row, readiness: { status: row.selectable && row.pi_binding_renderable ? "ready" : "blocked", blocking_reasons: row.pi_binding_renderable ? [] : ["pi_binding_required"], repair_path: `/route/models/${row.model_config_id}` } };
}

const catalogWire = {
  status: "fresh" as const,
  revision: "rev-1",
  minimum_version: "0.80.0",
};

const searchCatalogWire = {
  ...catalogWire,
  revision: "rev-2",
};

const internalSourceModel = sourceModel({
  model_config_id: 9,
  model_id: "deepseek/deepseek-v4-flash-0731",
  direct_request_enabled: false,
});

const unboundSource = {
  target_version: "0.84.3",
  catalog: catalogWire,
  source_digest: "a".repeat(64),
  models: [
    sourceModel({
      model_id: "codex/gpt-x",
      display_name: "Codex GPT X",
      pi_candidates: [],
      candidate_status: "not_in_catalog",
    }),
    internalSourceModel,
  ],
};

const boundSource = {
  target_version: "0.84.3",
  catalog: searchCatalogWire,
  source_digest: "b".repeat(64),
  models: [
    sourceModel({
      model_id: "codex/gpt-x",
      display_name: "Codex GPT X",
      pi_candidates: [],
      candidate_status: "not_in_catalog",
      pi_selected: {
        provider_id: "alias-provider",
        model_id: "gpt-x-alias",
        api: "openai-responses",
      },
      pi_binding_status: "bound",
      pi_binding_renderable: true,
      pi_bind_source: "manual",
      pi_binding_prism_model_id: "codex/gpt-x",
      pi_binding_catalog_revision: "rev-2",
    }),
    internalSourceModel,
  ],
};

export const renderPayload = {
  target_version: "0.84.3",
  content: `{"providers":{"prism":{"name":"Prism"}}}\n`,
  content_sha256: "c".repeat(64),
  file_name: "prism-pi-models.json",
  mime_type: "application/json;charset=utf-8",
  model_results: [
    { model_config_id: 3, model_id: "codex/gpt-x", cost_exported: true },
  ],
  warnings: [],
};

export async function installExportRoutes(page: Page, options: { initiallyBound?: boolean } = {}) {
  let bound = options.initiallyBound ?? false;
  const outbound: string[] = [];
  const unexpectedApi: string[] = [];
  const pageErrors: string[] = [];
  const openCodeRequests: Record<string, unknown>[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.route("**/*", async (route) => {
    const request = route.request();
    outbound.push(request.url());
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) return route.continue();
    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (pathname === "/api/auth/status") {
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/models/exports/pi/source") {
      return fulfillJson(bound ? boundSource : unboundSource);
    }
    if (pathname === "/api/models/exports/opencode/source") {
      return fulfillJson(openCodeSource);
    }
    if (pathname === "/api/models/exports/opencode/render" && request.method() === "POST") {
      const body = request.postDataJSON();
      openCodeRequests.push(body);
      return fulfillJson(openCodeRenderPayload(body.credential?.include ? body.credential.api_key : undefined));
    }
    if (pathname === "/api/models/3/pi/search" && request.method() === "POST") {
      return fulfillJson({
        query: "gpt-x",
        api: "openai-responses",
        limit: 20,
        total: 1,
        returned: 1,
        truncated: false,
        selected: false,
        catalog: searchCatalogWire,
        fetched_at: "2026-08-30T00:00:00Z",
        export_identity: {
          model_config_id: 3,
          model_id: "codex/gpt-x",
          api: "openai-responses",
          provider_id_source: "operator_input",
        },
        results: [
          {
            provider_id: "alias-provider",
            model_id: "gpt-x-alias",
            api: "openai-responses",
            name: "GPT X Alias",
            context_window: 200000,
            dropped_fields: ["headers"],
          },
        ],
      });
    }
    if (pathname === "/api/models/3/pi/bind" && request.method() === "POST") {
      bound = true;
      return fulfillJson({
        bound: true,
        bind_source: "manual",
        provider_id: "alias-provider",
        catalog_model_id: "gpt-x-alias",
        api: "openai-responses",
        prism_model_id_at_bind: "codex/gpt-x",
        catalog_revision: "rev-2",
        source: {
          name: "GPT X",
          reasoning: null,
          input: null,
          context_window: null,
          max_tokens: null,
          thinking_level_map: null,
          compat: null,
        },
        override: null,
        effective: {
          name: "GPT X",
          reasoning: null,
          input: null,
          context_window: null,
          max_tokens: null,
          thinking_level_map: null,
          compat: null,
        },
      });
    }
    if (
      pathname === "/api/models/exports/pi/render" &&
      request.method() === "POST"
    ) {
      return fulfillJson(renderPayload);
    }
    unexpectedApi.push(`${request.method()} ${pathname}`);
    return fulfillJson({ detail: "unexpected mocked API request" }, 500);
  });
  return { outbound, pageErrors, unexpectedApi, openCodeRequests };
}
