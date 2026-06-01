import { expect, test, type Page } from "@playwright/test";

const fixedTimestamp = "2026-04-28T12:00:00Z";
const liveAuthoringCapabilityDefaults = {
  context_window_tokens: null,
  default_output_token_reserve: 4_096,
  max_context_utilization: 0.9,
};

type ProfileImportBundle = ReturnType<typeof buildProfileImportBundle>;

type PreviewRequestCapture = {
  payload: unknown;
  previewToken: string;
  profileHeader: string | null;
};

function createProfile() {
  return {
    id: 1,
    name: "Default",
    description: null,
    is_active: true,
    is_default: true,
    is_editable: true,
    version: 1,
    created_at: fixedTimestamp,
    deleted_at: null,
    updated_at: fixedTimestamp,
  };
}

function createSecondaryProfile() {
  return {
    ...createProfile(),
    id: 2,
    name: "Disaster recovery",
    description: "Restores imported profile bundles",
    is_active: false,
    is_default: false,
    version: 2,
  };
}

function createAuthSettings() {
  return {
    auth_enabled: false,
    username: null,
    has_password: false,
    email: null,
    pending_email: null,
    email_bound_at: null,
    email_verification_required: false,
  };
}

function createCostingSettings() {
  return {
    report_currency_code: "EUR",
    report_currency_symbol: "€",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

function createRetentionSettings() {
  return {
    request_logs_retention_days: 30,
    statistics_retention_days: 30,
    audit_logs_retention_days: 30,
    loadbalance_events_retention_days: 30,
  };
}

function createModelListItem() {
  return {
    id: 1,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    ...liveAuthoringCapabilityDefaults,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: fixedTimestamp,
    updated_at: fixedTimestamp,
  };
}

function buildProfileImportBundle(variant: "alpha" | "beta") {
  if (variant === "alpha") {
    return {
      version: 3 as const,
      bundle_kind: "profile_config" as const,
      vendor_refs: [
        {
          key: "openai",
          name_hint: "OpenAI",
          description_hint: "Primary vendor",
          icon_key_hint: "openai",
        },
      ],
      endpoints: [
        {
          name: "Alpha endpoint",
          base_url: "https://alpha.example.invalid",
          api_key_secret_ref: "alpha-endpoint-secret",
          position: 0,
        },
      ],
      pricing_templates: [],
      connections: [
        {
          ref: "alpha-connection",
          endpoint_name: "Alpha endpoint",
          api_family: "openai" as const,
          ...liveAuthoringCapabilityDefaults,
          pricing_template_name: null,
          is_active: true,
          name: "Alpha connection",
          auth_type: "openai" as const,
          custom_headers: null,
          openai_probe_endpoint_variant: null,
          qps_limit: null,
          max_in_flight_non_stream: null,
          max_in_flight_stream: null,
        },
      ],
      loadbalance_strategies: [
        {
          name: "Alpha legacy routing",
          legacy_strategy_type: "round-robin" as const,
          failure_status_codes: [429, 500],
          ban_mode: "off" as const,
          retry_base_delay_ms: 1000,
          retry_backoff_multiplier: 2,
          retry_jitter_ratio: 0.2,
          retry_max_delay_ms: 8000,
          cycle_retry_attempt_limit: 3,
          ban_cumulative_retry_attempt_threshold: 0,
          ban_duration_seconds: 0,
        },
      ],
      models: [
        {
          vendor_key: "openai",
          api_family: "openai" as const,
          model_id: "alpha-model",
          display_name: "Alpha model",
          loadbalance_strategy_name: "Alpha legacy routing",
          ...liveAuthoringCapabilityDefaults,
          is_enabled: true,
          access_targets: [
            { position: 0, is_enabled: true, target_type: "connection" as const, connection_ref: "alpha-connection" },
          ],
        },
      ],
      profile_settings: {
        report_currency_code: "EUR",
        report_currency_symbol: "€",
        timezone_preference: null,
        endpoint_fx_mappings: [],
      },
      header_blocklist_rules: [
        {
          name: "Hide request IDs",
          match_type: "prefix" as const,
          pattern: "x-request-",
          enabled: true,
        },
      ],
      user_agent_client_rules: [
        {
          name: "Codex CLI",
          pattern: "Codex",
          enabled: true,
        },
      ],
      secret_payload: {
        kind: "encrypted" as const,
        cipher: "fernet-v1" as const,
        key_id: "alpha-key",
        entries: [{ ref: "alpha-endpoint-secret", ciphertext: "cipher-alpha" }],
      },
    };
  }

  return {
    version: 3 as const,
    bundle_kind: "profile_config" as const,
    vendor_refs: [
      {
        key: "anthropic",
        name_hint: "Anthropic",
        description_hint: "Fallback vendor",
        icon_key_hint: "anthropic",
      },
    ],
    endpoints: [
      {
        name: "Beta endpoint A",
        base_url: "https://beta-a.example.invalid",
        api_key_secret_ref: "beta-endpoint-secret-a",
        position: 0,
      },
      {
        name: "Beta endpoint B",
        base_url: "https://beta-b.example.invalid",
        api_key_secret_ref: "beta-endpoint-secret-b",
        position: 1,
      },
    ],
    pricing_templates: [],
    connections: [
      {
        ref: "beta-connection-a",
        endpoint_name: "Beta endpoint A",
        api_family: "anthropic" as const,
        context_window_tokens: 200_000,
        default_output_token_reserve: 8_192,
        max_context_utilization: 0.92,
        pricing_template_name: null,
        is_active: true,
        name: "Beta connection A",
        auth_type: "anthropic" as const,
        custom_headers: null,
        openai_probe_endpoint_variant: null,
        qps_limit: null,
        max_in_flight_non_stream: null,
        max_in_flight_stream: null,
      },
      {
        ref: "beta-connection-b",
        endpoint_name: "Beta endpoint B",
        api_family: "anthropic" as const,
        ...liveAuthoringCapabilityDefaults,
        pricing_template_name: null,
        is_active: true,
        name: "Beta connection B",
        auth_type: "anthropic" as const,
        custom_headers: null,
        openai_probe_endpoint_variant: null,
        qps_limit: null,
        max_in_flight_non_stream: null,
        max_in_flight_stream: null,
      },
    ],
    loadbalance_strategies: [
      {
        name: "Beta legacy routing",
        legacy_strategy_type: "fill-first" as const,
        failure_status_codes: [429, 500],
        ban_mode: "off" as const,
        retry_base_delay_ms: 1000,
        retry_backoff_multiplier: 2,
        retry_jitter_ratio: 0.2,
        retry_max_delay_ms: 8000,
        cycle_retry_attempt_limit: 3,
        ban_cumulative_retry_attempt_threshold: 0,
        ban_duration_seconds: 0,
      },
    ],
    models: [
      {
        vendor_key: "anthropic",
        api_family: "anthropic" as const,
        model_id: "beta-model",
        display_name: "Beta model",
        loadbalance_strategy_name: "Beta legacy routing",
        context_window_tokens: 262_144,
        default_output_token_reserve: 12_288,
        max_context_utilization: 0.95,
        is_enabled: true,
        access_targets: [
          { position: 0, is_enabled: true, target_type: "connection" as const, connection_ref: "beta-connection-a" },
          { position: 1, is_enabled: true, target_type: "connection" as const, connection_ref: "beta-connection-b" },
        ],
      },
    ],
    profile_settings: {
      report_currency_code: "EUR",
      report_currency_symbol: "€",
      timezone_preference: null,
      endpoint_fx_mappings: [],
    },
    header_blocklist_rules: [
      {
        name: "Hide trace headers",
        match_type: "prefix" as const,
        pattern: "x-trace-",
        enabled: true,
      },
    ],
    user_agent_client_rules: [
      {
        name: "Claude Code",
        pattern: "Claude\\sCode",
        enabled: true,
      },
    ],
    secret_payload: {
      kind: "encrypted" as const,
      cipher: "fernet-v1" as const,
      key_id: "beta-key",
      entries: [
        { ref: "beta-endpoint-secret-a", ciphertext: "cipher-beta-a" },
        { ref: "beta-endpoint-secret-b", ciphertext: "cipher-beta-b" },
      ],
    },
  };
}

function countConnections(bundle: ProfileImportBundle) {
  return bundle.connections.length;
}

function buildPreviewResponse(bundle: ProfileImportBundle, previewToken: string) {
  return {
    ready: true,
    version: 3 as const,
    bundle_kind: "profile_config" as const,
    preview_token: previewToken,
    bundle_fingerprint: `profile-fingerprint-${previewToken}`,
    replacement_scope: {
      target: "selected_profile" as const,
      endpoints: bundle.endpoints.length,
      pricing_templates: bundle.pricing_templates.length,
      loadbalance_strategies: bundle.loadbalance_strategies.length,
      models: bundle.models.length,
      connections: countConnections(bundle),
      header_blocklist_rules: bundle.header_blocklist_rules.length,
      user_agent_client_rules: bundle.user_agent_client_rules.length,
      profile_settings: true,
    },
    untouched_scope: {
      other_profiles: true,
      existing_global_vendor_metadata: true,
      request_logs: true,
    },
    vendor_summary: {
      create_count: bundle.vendor_refs.length,
      reuse_count: 0,
      warning_count: 1,
    },
    secret_summary: {
      endpoint_secret_refs: bundle.endpoints.filter((endpoint) => endpoint.api_key_secret_ref).length,
      secret_payload_entries: bundle.secret_payload.entries.length,
      decryptable_secret_refs: bundle.secret_payload.entries.length,
    },
    endpoints_imported: bundle.endpoints.length,
    pricing_templates_imported: bundle.pricing_templates.length,
    strategies_imported: bundle.loadbalance_strategies.length,
    models_imported: bundle.models.length,
    connections_imported: countConnections(bundle),
    vendor_resolutions: bundle.vendor_refs.map((vendor) => ({
      vendor_key: vendor.key,
      resolution: "create" as const,
      warning: "Vendor metadata will be created during import.",
    })),
    secret_key_id: bundle.secret_payload.key_id,
    decryptable_secret_refs: bundle.secret_payload.entries.map((entry) => entry.ref),
    blocking_errors: [],
    warnings: ["Secrets are imported only when the referenced key is available."],
  };
}

type ProfileFixture = ReturnType<typeof createProfile> | ReturnType<typeof createSecondaryProfile>;

type ProfilePreviewResponse = Omit<ReturnType<typeof buildPreviewResponse>, "blocking_errors" | "warnings"> & {
  blocking_errors: string[];
  warnings: string[];
};

type PreviewResponseFactory = (
  bundle: ProfileImportBundle,
  previewToken: string,
) => ProfilePreviewResponse;

interface MockSettingsRoutesOptions {
  profiles?: ProfileFixture[];
  previewResponseFactory?: PreviewResponseFactory;
}

async function mockSettingsRoutes(page: Page, options: MockSettingsRoutesOptions = {}) {
  const defaultProfile = createProfile();
  const profiles = options.profiles ?? [defaultProfile];
  const activeProfile = profiles.find((profile) => profile.is_active) ?? profiles[0] ?? defaultProfile;
  const previewTokenBindings = new Map<string, { payloadText: string; profileHeader: string | null }>();
  const previewRequests: PreviewRequestCapture[] = [];
  const importedPayloads: unknown[] = [];
  const appliedPreviewTokens: string[] = [];
  const appliedProfileHeaders: Array<string | null> = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

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
      return fulfillJson({ profiles, active_profile: activeProfile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson(createCostingSettings());
    }
    if (pathname === "/api/settings/auth") {
      return fulfillJson(createAuthSettings());
    }
    if (pathname === "/api/settings/log-retention") {
      return fulfillJson(createRetentionSettings());
    }
    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem()]);
    }
    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/header-blocklist-rules") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/user-agent-client-rules") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/profile/import/preview" && request.method() === "POST") {
      const payload = request.postDataJSON() as ProfileImportBundle;
      const profileHeader = (await request.allHeaders())["x-profile-id"] ?? null;
      const previewToken = `profile-preview-token-${previewRequests.length + 1}`;
      previewRequests.push({ payload, previewToken, profileHeader });
      previewTokenBindings.set(previewToken, { payloadText: JSON.stringify(payload), profileHeader });
      const previewResponse = options.previewResponseFactory?.(payload, previewToken)
        ?? buildPreviewResponse(payload, previewToken);
      return fulfillJson(previewResponse);
    }
    if (pathname === "/api/config/profile/import" && request.method() === "POST") {
      const headers = await request.allHeaders();
      const previewToken = headers["x-prism-preview-token"] ?? "";
      const profileHeader = headers["x-profile-id"] ?? null;
      const payload = request.postDataJSON();
      const previewBinding = previewTokenBindings.get(previewToken);
      if (!previewToken) {
        return fulfillJson({ error: "missing preview token" }, 400);
      }
      if (
        !previewBinding ||
        previewBinding.payloadText !== JSON.stringify(payload) ||
        previewBinding.profileHeader !== profileHeader
      ) {
        return fulfillJson({ error: "preview token is stale or mismatched" }, 409);
      }
      importedPayloads.push(payload);
      appliedPreviewTokens.push(previewToken);
      appliedProfileHeaders.push(profileHeader);
      const bundle = payload as ProfileImportBundle;
      return fulfillJson({
        endpoints_imported: bundle.endpoints.length,
        pricing_templates_imported: bundle.pricing_templates.length,
        strategies_imported: bundle.loadbalance_strategies.length,
        models_imported: bundle.models.length,
        connections_imported: countConnections(bundle),
      });
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getAppliedPreviewTokens: () => appliedPreviewTokens,
    getAppliedProfileHeaders: () => appliedProfileHeaders,
    getImportedPayloads: () => importedPayloads,
    getPreviewRequests: () => previewRequests,
  };
}

test("context-capability-authoring: config import requires an explicit preview before apply", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);
  const importBundle = buildProfileImportBundle("alpha");

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  const applyButton = backupSection.getByTestId("profile-import-apply");

  await expect(backupSection).toBeVisible();
  await backupSection.getByTestId("profile-import-file").setInputFiles({
    name: "profile-import-alpha.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(importBundle)),
  });

  await expect(backupSection.getByText("Loaded profile-import-alpha.json: 1 endpoints, 1 strategies, 1 models, 1 top-level connections.")).toBeVisible();
  await expect(backupSection.getByText("Run preview to bind a fresh token for the currently loaded bundle before applying it.")).toBeVisible();
  await expect(applyButton).toBeDisabled();
  expect(routes.getPreviewRequests()).toEqual([]);
  expect(routes.getImportedPayloads()).toEqual([]);

  await backupSection.getByTestId("profile-import-preview").click();

  await expect(backupSection.getByText("Preview status")).toBeVisible();
  await expect(backupSection.getByText("This preview token is bound to Default (#1). Changing the file or selected profile requires a fresh preview before apply.")).toBeVisible();
  await expect(backupSection.getByText("Replacement scope", { exact: true })).toBeVisible();
  await expect(backupSection.getByText("Untouched scope", { exact: true })).toBeVisible();
  await expect(backupSection.getByText("Vendor summary", { exact: true })).toBeVisible();
  await expect(backupSection.getByText("Secret summary", { exact: true })).toBeVisible();
  await expect(backupSection.getByText("Vendor resolutions", { exact: true })).toBeVisible();
  await expect(backupSection.getByText("Vendor metadata will be created during import.")).toBeVisible();
  await expect(backupSection.getByText("Secrets are imported only when the referenced key is available.")).toBeVisible();
  await expect(applyButton).toBeEnabled();

  await applyButton.click();

  await expect(page.getByText("Imported 1 endpoints, 1 strategies, 1 models, 1 top-level connections")).toBeVisible();
  const previewRequests = routes.getPreviewRequests();
  expect(previewRequests).toHaveLength(1);
  expect(previewRequests[0]).toEqual({
    payload: importBundle,
    previewToken: "profile-preview-token-1",
    profileHeader: "1",
  });
  expect(routes.getImportedPayloads()).toEqual([importBundle]);
  expect(routes.getAppliedPreviewTokens()).toEqual(["profile-preview-token-1"]);
  expect(routes.getAppliedProfileHeaders()).toEqual(["1"]);
});

test("context-capability-authoring: config import keeps apply disabled and surfaces the first blocking preview error", async ({ page }) => {
  const routes = await mockSettingsRoutes(page, {
    previewResponseFactory: (bundle, previewToken) => ({
      ...buildPreviewResponse(bundle, previewToken),
      ready: false,
      blocking_errors: [
        "Bundle key mismatch blocks this import.",
        "Second blocking error stays in the preview details.",
      ],
    }),
  });
  const importBundle = buildProfileImportBundle("alpha");

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  const applyButton = backupSection.getByTestId("profile-import-apply");

  await expect(backupSection).toBeVisible();
  await backupSection.getByTestId("profile-import-file").setInputFiles({
    name: "profile-import-alpha.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(importBundle)),
  });
  await expect(applyButton).toBeDisabled();

  await backupSection.getByTestId("profile-import-preview").click();

  await expect(backupSection.getByText("Blocking errors", { exact: true })).toBeVisible();
  await expect(backupSection.getByText("Bundle key mismatch blocks this import.")).toBeVisible();
  await expect(backupSection.getByText("Second blocking error stays in the preview details.")).toBeVisible();
  await expect(applyButton).toBeDisabled();
  expect(routes.getPreviewRequests()).toHaveLength(1);
  expect(routes.getImportedPayloads()).toEqual([]);
  expect(routes.getAppliedPreviewTokens()).toEqual([]);
});

test("context-capability-authoring: config import invalidates a stale preview when the bundle changes", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);
  const firstBundle = buildProfileImportBundle("alpha");
  const secondBundle = buildProfileImportBundle("beta");

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  const applyButton = backupSection.getByTestId("profile-import-apply");

  await expect(backupSection).toBeVisible();
  await backupSection.getByTestId("profile-import-file").setInputFiles({
    name: "profile-import-alpha.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(firstBundle)),
  });
  await backupSection.getByTestId("profile-import-preview").click();
  await expect(applyButton).toBeEnabled();

  await backupSection.getByTestId("profile-import-file").setInputFiles({
    name: "profile-import-beta.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(secondBundle)),
  });

  await expect(backupSection.getByText("Loaded profile-import-beta.json: 2 endpoints, 1 strategies, 1 models, 2 top-level connections.")).toBeVisible();
  await expect(backupSection.getByText("Run preview to bind a fresh token for the currently loaded bundle before applying it.")).toBeVisible();
  await expect(applyButton).toBeDisabled();
  expect(routes.getImportedPayloads()).toEqual([]);

  await backupSection.getByTestId("profile-import-preview").click();
  await expect(backupSection.getByText("This preview token is bound to Default (#1). Changing the file or selected profile requires a fresh preview before apply.")).toBeVisible();
  await expect(applyButton).toBeEnabled();

  await applyButton.click();

  await expect(page.getByText("Imported 2 endpoints, 1 strategies, 1 models, 2 top-level connections")).toBeVisible();
  const previewRequests = routes.getPreviewRequests();
  expect(previewRequests).toHaveLength(2);
  expect(previewRequests[0]).toEqual({
    payload: firstBundle,
    previewToken: "profile-preview-token-1",
    profileHeader: "1",
  });
  expect(previewRequests[1]).toEqual({
    payload: secondBundle,
    previewToken: "profile-preview-token-2",
    profileHeader: "1",
  });
  expect(routes.getImportedPayloads()).toEqual([secondBundle]);
  expect(routes.getAppliedPreviewTokens()).toEqual(["profile-preview-token-2"]);
  expect(routes.getAppliedProfileHeaders()).toEqual(["1"]);
});

test("context-capability-authoring: config import invalidates a stale preview when the selected profile changes", async ({ page }) => {
  const routes = await mockSettingsRoutes(page, {
    profiles: [createProfile(), createSecondaryProfile()],
  });
  const importBundle = buildProfileImportBundle("alpha");

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  const applyButton = backupSection.getByTestId("profile-import-apply");

  await expect(backupSection).toBeVisible();
  await backupSection.getByTestId("profile-import-file").setInputFiles({
    name: "profile-import-alpha.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(importBundle)),
  });
  await backupSection.getByTestId("profile-import-preview").click();
  await expect(backupSection.getByText("This preview token is bound to Default (#1). Changing the file or selected profile requires a fresh preview before apply.")).toBeVisible();
  await expect(applyButton).toBeEnabled();

  await page.getByTestId("shell-profile-switcher").getByRole("button").click();
  await page.getByRole("menuitem", { name: /Disaster recovery/ }).click();

  await expect(page.getByText("Changes here manage Disaster recovery (#2).")).toBeVisible();
  await expect(backupSection.getByText("Loaded profile-import-alpha.json")).toHaveCount(0);
  await expect(applyButton).toHaveCount(0);
  expect(routes.getImportedPayloads()).toEqual([]);

  await backupSection.getByTestId("profile-import-file").setInputFiles({
    name: "profile-import-alpha.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(importBundle)),
  });
  const reboundApplyButton = backupSection.getByTestId("profile-import-apply");
  await expect(reboundApplyButton).toBeDisabled();

  await backupSection.getByTestId("profile-import-preview").click();
  await expect(backupSection.getByText("This preview token is bound to Disaster recovery (#2). Changing the file or selected profile requires a fresh preview before apply.")).toBeVisible();
  await expect(reboundApplyButton).toBeEnabled();

  await reboundApplyButton.click();

  await expect(page.getByText("Imported 1 endpoints, 1 strategies, 1 models, 1 top-level connections")).toBeVisible();
  const previewRequests = routes.getPreviewRequests();
  expect(previewRequests).toHaveLength(2);
  expect(previewRequests[0]).toEqual({
    payload: importBundle,
    previewToken: "profile-preview-token-1",
    profileHeader: "1",
  });
  expect(previewRequests[1]).toEqual({
    payload: importBundle,
    previewToken: "profile-preview-token-2",
    profileHeader: "2",
  });
  expect(routes.getImportedPayloads()).toEqual([importBundle]);
  expect(routes.getAppliedPreviewTokens()).toEqual(["profile-preview-token-2"]);
  expect(routes.getAppliedProfileHeaders()).toEqual(["2"]);
});
