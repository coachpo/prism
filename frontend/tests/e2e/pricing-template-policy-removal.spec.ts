import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-13T00:00:00Z";

function createProfile() {
  return {
    id: 1,
    name: "Default",
    description: null,
    is_active: true,
    is_default: true,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createPricingTemplate(overrides: Record<string, unknown> = {}) {
  return {
    id: 11,
    profile_id: 1,
    name: "Baseline USD",
    description: "Shared baseline pricing",
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    input_price: "0.10",
    output_price: "0.20",
    cached_input_price: null,
    cache_creation_price: null,
    reasoning_price: null,
    version: 1,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  };
}

type PricingTemplateRouteOptions = {
  connectionUsageItems?: unknown[];
  deleteResponse?: {
    body: unknown;
    status: number;
  };
};

async function stubPricingTemplateRoutes(page: Page, options: PricingTemplateRouteOptions = {}) {
  const profile = createProfile();
  const createPayloads: unknown[] = [];
  const deleteRequests: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    const method = request.method();

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({
        profiles: [profile],
        active_profile: profile,
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/pricing-templates" && method === "GET") {
      return fulfillJson([createPricingTemplate({ cached_input_price: "0.05" })]);
    }

    if (pathname === "/api/pricing-templates/11" && method === "GET") {
      return fulfillJson(createPricingTemplate({ cached_input_price: "0.05" }));
    }

    if (pathname === "/api/pricing-templates/11/connections" && method === "GET") {
      return fulfillJson({ items: options.connectionUsageItems ?? [] });
    }

    if (pathname === "/api/pricing-templates/11" && method === "DELETE") {
      deleteRequests.push(pathname);
      if (options.deleteResponse) {
        return fulfillJson(options.deleteResponse.body, options.deleteResponse.status);
      }
      return fulfillJson({});
    }

    if (pathname === "/api/pricing-templates" && method === "POST") {
      const payload = request.postDataJSON();
      createPayloads.push(payload);
      return fulfillJson(
        createPricingTemplate({
          id: 12,
          name: payload.name,
          description: payload.description,
          pricing_currency_code: payload.pricing_currency_code,
          input_price: payload.input_price,
          output_price: payload.output_price,
          cached_input_price: payload.cached_input_price,
          cache_creation_price: payload.cache_creation_price,
          reasoning_price: payload.reasoning_price,
        }),
        201,
      );
    }

    return fulfillJson({ error: `Unhandled ${method} ${pathname}` }, 500);
  });

  return { createPayloads, deleteRequests };
}

test("pricing template dialog normalizes all prices and removes optional/default pricing copy", async ({ page }) => {
  const { createPayloads } = await stubPricingTemplateRoutes(page);

  await page.goto("/route/pricing");
  await expect(page.getByText("Cached Input Price (per 1M tokens): 0.05")).toBeVisible();
  await expect(page.getByText("Cache Creation Price (per 1M tokens): 0")).toBeVisible();
  await expect(page.getByText("Reasoning Price (per 1M tokens): 0")).toBeVisible();
  await expect(page.getByText(/0 \(default\)|Primary rate|Optional rate/)).toHaveCount(0);

  await page.getByRole("button", { name: "Edit" }).click();
  let dialog = page.getByRole("dialog");
  const priceInput = (label: string) => dialog.getByRole("textbox", { name: label, exact: true });
  await expect(dialog.getByRole("heading", { name: "Edit Pricing Template" })).toBeVisible();
  await expect(priceInput("Cached Input Price (per 1M tokens)")).toHaveValue("0.05");
  await expect(priceInput("Cache Creation Price (per 1M tokens)")).toHaveValue("0");
  await expect(priceInput("Reasoning Price (per 1M tokens)")).toHaveValue("0");
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();

  await page.getByRole("button", { name: "Add Template" }).click();

  dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Add Pricing Template" })).toBeVisible();
  await expect(dialog.getByText("Base token rates")).toBeVisible();
  await expect(dialog.getByText("Specialized token rates")).toBeVisible();
  await expect(
    dialog.getByText(
      "Set explicit rates for cached input, cache creation, and reasoning tokens. Use 0 when a token class should not add cost.",
    ),
  ).toBeVisible();
  await expect(dialog.getByLabel("Name")).toBeVisible();
  await expect(dialog.getByLabel("Currency Code")).toHaveValue("USD");
  await expect(priceInput("Input Price (per 1M tokens)")).toHaveValue("0");
  await expect(priceInput("Output Price (per 1M tokens)")).toHaveValue("0");
  await expect(priceInput("Cached Input Price (per 1M tokens)")).toHaveValue("0");
  await expect(priceInput("Cache Creation Price (per 1M tokens)")).toHaveValue("0");
  await expect(priceInput("Reasoning Price (per 1M tokens)")).toHaveValue("0");
  await expect(dialog.getByText(/missing special token|fallback policy|Primary rate|Optional rate|0 \(default\)/i)).toHaveCount(0);

  await dialog.getByLabel("Name").fill("Special token cleanup template");
  await priceInput("Input Price (per 1M tokens)").fill("   ");
  await priceInput("Output Price (per 1M tokens)").fill("");
  await priceInput("Cached Input Price (per 1M tokens)").fill(" ");
  await priceInput("Cache Creation Price (per 1M tokens)").fill("  ");
  await priceInput("Reasoning Price (per 1M tokens)").fill("");
  await dialog.getByRole("button", { name: "Save Template" }).click();

  await expect.poll(() => createPayloads.length).toBe(1);
  expect(createPayloads[0]).toEqual({
    name: "Special token cleanup template",
    description: null,
    pricing_currency_code: "USD",
    input_price: "0",
    output_price: "0",
    cached_input_price: "0",
    cache_creation_price: "0",
    reasoning_price: "0",
  });
});


test("pricing template usage and preflight delete dependencies use terminal target labels", async ({ page }) => {
  const usageRow = {
    connection_id: 501,
    connection_name: "Primary Target",
    model_config_id: 101,
    model_id: "gpt-4.1",
    endpoint_id: 201,
    endpoint_name: "OpenAI Primary",
  };
  const { deleteRequests } = await stubPricingTemplateRoutes(page, {
    connectionUsageItems: [usageRow],
  });

  await page.goto("/route/pricing");
  const templateRow = page.getByRole("row").filter({ hasText: "Baseline USD" });
  await templateRow.getByRole("button", { name: "View Usage Baseline USD" }).click();

  let dialog = page.getByRole("dialog", { name: "Template Usage" });
  await expect(dialog.getByText('Terminal targets currently using the "Baseline USD" template.')).toBeVisible();
  await expect(dialog.getByRole("columnheader", { name: "Terminal Target" })).toBeVisible();
  const usageTableRow = dialog.getByRole("row").filter({ hasText: "gpt-4.1" });
  await expect(usageTableRow).toContainText("OpenAI Primary");
  await expect(usageTableRow).toContainText("Primary Target");
  await dialog.getByRole("button", { name: "Close" }).first().click();
  await expect(page.getByRole("dialog")).toBeHidden();

  await templateRow.getByRole("button", { name: "Delete" }).click();

  dialog = page.getByRole("dialog", { name: "Delete Pricing Template" });
  await expect(dialog.getByRole("heading", { name: "Delete Pricing Template" })).toBeVisible();
  await expect(dialog.getByText("Cannot delete this template because it is currently used by 1 terminal target.")).toBeVisible();
  await expect(dialog.getByRole("columnheader", { name: "Terminal Target" })).toBeVisible();
  const dependencyRow = dialog.getByRole("row").filter({ hasText: "gpt-4.1" });
  await expect(dependencyRow).toContainText("OpenAI Primary");
  await expect(dependencyRow).toContainText("Primary Target");
  await expect(dialog.getByRole("button", { name: "Delete" })).toBeDisabled();
  expect(deleteRequests).toEqual([]);
});


test("pricing template delete conflict relabels backend 409 connection rows", async ({ page }) => {
  const rawBackendMessage = "Cannot delete pricing template that is referenced by connections";
  await stubPricingTemplateRoutes(page, {
    connectionUsageItems: [],
    deleteResponse: {
      status: 409,
      body: {
        detail: {
          message: rawBackendMessage,
          connections: [
            {
              connection_id: 502,
              connection_name: "Backup Target",
              model_config_id: 102,
              model_id: "model-a",
              endpoint_id: 202,
              endpoint_name: "Endpoint A",
            },
          ],
        },
      },
    },
  });

  await page.goto("/route/pricing");
  const templateRow = page.getByRole("row").filter({ hasText: "Baseline USD" });
  await templateRow.getByRole("button", { name: "Delete" }).click();

  const dialog = page.getByRole("dialog", { name: "Delete Pricing Template" });
  const deleteButton = dialog.getByRole("button", { name: "Delete" });
  await expect(deleteButton).toBeEnabled();
  await deleteButton.click();

  await expect(dialog.getByText("Cannot delete this template because it is currently used by 1 terminal target.")).toBeVisible();
  await expect(dialog.getByText(rawBackendMessage)).toHaveCount(0);
  await expect(dialog.getByRole("columnheader", { name: "Terminal Target" })).toBeVisible();
  const conflictRow = dialog.getByRole("row").filter({ hasText: "model-a" });
  await expect(conflictRow).toContainText("Endpoint A");
  await expect(conflictRow).toContainText("Backup Target");
  await expect(deleteButton).toBeDisabled();
});
