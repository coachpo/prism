import { describe, expect, it } from "vitest";

import { parsePricingTemplateUsageRows } from "./pricingUsage";

describe("pricing usage projection", () => {
  it("accepts direct and nested connection response envelopes", () => {
    const connection = {
      connection_id: 11,
      model_config_id: 22,
      endpoint_id: 33,
      model_id: "router-model",
      endpoint_name: "OpenAI",
      connection_name: "primary",
    };

    expect(parsePricingTemplateUsageRows({ connections: [connection] })).toEqual([
      {
        connection_id: 11,
        model_config_id: 22,
        endpoint_id: 33,
        model_id: "router-model",
        endpoint_name: "OpenAI",
        connection_name: "primary",
      },
    ]);
    expect(parsePricingTemplateUsageRows({ detail: { connections: [connection] } })).toHaveLength(1);
  });

  it("drops incomplete rows and preserves explicit missing-value projections", () => {
    const rows = parsePricingTemplateUsageRows({
      connections: [
        { connection_id: 1, model_config_id: 2, endpoint_id: 3 },
        { connection_id: "bad", model_config_id: 2, endpoint_id: 3 },
      ],
    });

    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      connection_id: 1,
      model_config_id: 2,
      endpoint_id: 3,
      connection_name: null,
    });
  });
});
