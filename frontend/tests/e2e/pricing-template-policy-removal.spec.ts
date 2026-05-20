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

async function stubPricingTemplateRoutes(page: Page) {
  const profile = createProfile();
  const createPayloads: unknown[] = [];

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

  return { createPayloads };
}

test("pricing template dialog keeps blank optional prices nullable and shows default-zero table values", async ({ page }) => {
  const { createPayloads } = await stubPricingTemplateRoutes(page);

  await page.goto("/pricing-templates");
  await expect(page.getByText("Cached Input Price (Optional): 0.05")).toBeVisible();
  await expect(page.getByText("Cache Creation Price (Optional): 0 (default)")).toBeVisible();
  await expect(page.getByText("Reasoning Price (Optional): 0 (default)")).toBeVisible();

  await page.getByRole("button", { name: "Edit" }).click();
  let dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Edit Pricing Template" })).toBeVisible();
  await expect(dialog.getByLabel("Cached Input Price (Optional)")).toHaveValue("0.05");
  await expect(dialog.getByLabel("Cache Creation Price (Optional)")).toHaveValue("");
  await expect(dialog.getByLabel("Reasoning Price (Optional)")).toHaveValue("");
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();

  await page.getByRole("button", { name: "Add Template" }).click();

  dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Add Pricing Template" })).toBeVisible();
  await expect(
    dialog.getByText(
      "Leave optional component prices blank to bill cached, cache-creation, or reasoning tokens at 0 per 1M tokens by default.",
    ),
  ).toBeVisible();
  await expect(dialog.getByLabel("Name")).toBeVisible();
  await expect(dialog.getByLabel("Currency Code")).toHaveValue("USD");
  await expect(dialog.getByLabel("Input Price (per 1M tokens)")).toBeVisible();
  await expect(dialog.getByLabel("Output Price (per 1M tokens)")).toBeVisible();
  await expect(dialog.getByLabel("Cached Input Price (Optional)")).toBeVisible();
  await expect(dialog.getByLabel("Cache Creation Price (Optional)")).toBeVisible();
  await expect(dialog.getByLabel("Reasoning Price (Optional)")).toBeVisible();
  await expect(dialog.getByText(/missing special token|fallback policy/i)).toHaveCount(0);

  await dialog.getByLabel("Name").fill("Special token cleanup template");
  await dialog.getByLabel("Input Price (per 1M tokens)").fill("0.30");
  await dialog.getByLabel("Output Price (per 1M tokens)").fill("0.60");
  await dialog.getByRole("button", { name: "Save Template" }).click();

  await expect.poll(() => createPayloads.length).toBe(1);
  expect(createPayloads[0]).toEqual({
    name: "Special token cleanup template",
    description: null,
    pricing_currency_code: "USD",
    input_price: "0.30",
    output_price: "0.60",
    cached_input_price: null,
    cache_creation_price: null,
    reasoning_price: null,
  });
});
