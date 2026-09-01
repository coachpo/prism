import { describe, expect, it } from "vitest";
import {
  buildConnectionDraftPayload,
  buildConnectionUpdatePayload,
} from "@/pages/model-detail/connectionDataSupport";
import type { Connection } from "@/lib/types";
import type { ConnectionDialogForm } from "@/pages/model-detail/useModelDetailDialogState";

function createPayload(upstream_model_id: string | undefined) {
  const connectionForm: ConnectionDialogForm = {
    api_family: "anthropic",
    name: "",
    is_active: true,
    upstream_model_id,
    custom_headers: null,
    openai_text_capability: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
  };
  return buildConnectionDraftPayload({
    apiFamily: "anthropic",
    createMode: "select",
    selectedEndpointId: "7",
    newEndpointForm: { name: "", base_url: "", api_key: "" },
    connectionForm,
    headerRows: [],
    customRequestParametersValue: null,
    routingScheduleValue: null,
    editingConnection: null,
    endpointSourceDefaultName: null,
  }).payload!;
}

describe("upstream_model_id create payload", () => {
  it.each([
    [undefined, undefined],
    ["", ""],
    ["  vendor/large-model  ", "vendor/large-model"],
    ["Vendor/ Model X v2", "Vendor/ Model X v2"],
  ])("projects %j without changing its identity semantics", (draft, expected) => {
    expect(createPayload(draft).upstream_model_id).toBe(expected);
  });
});

describe("upstream_model_id PATCH payload", () => {
  const stored = {
    upstream_model_id: "stored-upstream-id",
    pricing_template_id: null,
    updated_at: "2026-01-02T00:00:00Z",
  } as Connection;

  it.each([
    ["stored-upstream-id", undefined],
    ["", ""],
    ["new-upstream-id", "new-upstream-id"],
  ])("maps %j to the PATCH contract", (draft, expected) => {
    expect(
      buildConnectionUpdatePayload(createPayload(draft), stored)
        .upstream_model_id,
    ).toBe(expected);
  });
});
