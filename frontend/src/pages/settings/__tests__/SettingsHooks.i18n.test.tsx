import type { ChangeEvent } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { useConfigBackupData } from "../useConfigBackupData";
import { useRetentionDeletionData } from "../useRetentionDeletionData";
import { useAuthenticationSettingsData } from "../useAuthenticationSettingsData";

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({
    api: {
      config: {
        export: vi.fn(),
        previewImport: vi.fn(),
        import: vi.fn(),
      },
    stats: {
      delete: vi.fn(),
    },
    audit: {
      delete: vi.fn(),
    },
    loadbalance: {
      deleteEvents: vi.fn(),
    },
    settings: {
      auth: {
        get: vi.fn().mockRejectedValue(new Error("boom")),
        update: vi.fn(),
        requestEmailVerification: vi.fn(),
        confirmEmailVerification: vi.fn(),
      },
    },
  },
}));

function buildTimeoutPolicy() {
  return {
    attempt_open_timeout_ms: 2000,
    buffered_total_timeout_ms: 30000,
    stream_precommit_timeout_ms: 5000,
    stream_hard_cap_timeout_ms: 120000,
  };
}

function buildBackendShapedProfileBundle() {
  return {
    version: 2,
    bundle_kind: "profile_config" as const,
    exported_at: "2026-04-04T15:00:00Z",
    vendor_refs: [
      {
        key: "openai",
        name_hint: "OpenAI",
        description_hint: "OpenAI vendor",
        icon_key_hint: "openai",
      },
    ],
    endpoints: [
      {
        name: "openai-main",
        base_url: "https://api.openai.com",
        api_key_secret_ref: "endpoint:openai-main:api_key",
        pool_timeout: 5,
        connect_timeout: 10,
        write_timeout: 30,
        read_idle_timeout: 120,
        position: 0,
      },
    ],
    pricing_templates: [],
    loadbalance_strategies: [
      {
        name: "single-primary",
        strategy_type: "legacy" as const,
        timeout_policy: buildTimeoutPolicy(),
        legacy_strategy_type: "single" as const,
        auto_recovery: {
          mode: "disabled" as const,
        },
      },
    ],
    models: [
      {
        vendor_key: "openai",
        api_family: "openai" as const,
        model_id: "gpt-4o",
        display_name: "GPT-4o",
        model_type: "native" as const,
        proxy_targets: [],
        loadbalance_strategy_name: "single-primary",
        is_enabled: true,
        connections: [
          {
            endpoint_name: "openai-main",
            pricing_template_name: null,
            is_active: true,
            priority: 0,
            name: "Primary",
            auth_type: "openai" as const,
            custom_headers: { "X-Org": "my-org" },
            openai_probe_endpoint_variant: "responses_minimal" as const,
          },
        ],
      },
    ],
    profile_settings: {
      report_currency_code: "USD",
      report_currency_symbol: "$",
      timezone_preference: null,
      endpoint_fx_mappings: [],
    },
    header_blocklist_rules: [],
    secret_payload: {
      kind: "encrypted" as const,
      cipher: "fernet-v1" as const,
      key_id: "sha256:test",
      entries: [
        {
          ref: "endpoint:openai-main:api_key",
          ciphertext: "enc:test-ciphertext",
        },
      ],
    },
  };
}

describe("settings hooks i18n", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.documentElement.lang = "zh-CN";
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("exports without requiring a secret acknowledgement gate", async () => {
    const createObjectUrl = vi.fn(() => "blob:mock");
    const revokeObjectUrl = vi.fn();
    const originalClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = vi.fn();
    vi.stubGlobal("URL", {
      ...URL,
      createObjectURL: createObjectUrl,
      revokeObjectURL: revokeObjectUrl,
    });

    vi.mocked(api.config.export).mockResolvedValue({
      version: 2,
      bundle_kind: "profile_config",
      exported_at: "2026-04-04T15:00:00Z",
      vendor_refs: [],
      endpoints: [],
      pricing_templates: [],
      loadbalance_strategies: [],
      models: [],
      profile_settings: {
        report_currency_code: "USD",
        report_currency_symbol: "$",
        timezone_preference: null,
        endpoint_fx_mappings: [],
      },
      header_blocklist_rules: [],
      secret_payload: {
        kind: "encrypted",
        cipher: "fernet-v1",
        key_id: "sha256:test",
        entries: [],
      },
    } as never);

    const { result } = renderHook(() => useConfigBackupData({ bumpRevision: vi.fn() }));

    await act(async () => {
      await result.current.handleExport();
    });

    expect(api.config.export).toHaveBeenCalledTimes(1);
    expect(toast.error).not.toHaveBeenCalled();
    expect(createObjectUrl).toHaveBeenCalledTimes(1);
    expect(revokeObjectUrl).toHaveBeenCalledWith("blob:mock");

    HTMLAnchorElement.prototype.click = originalClick;
  });

  it("previews an imported v2 bundle on file select", async () => {
    document.documentElement.lang = "en";
    vi.mocked(api.config.previewImport).mockResolvedValue({
      ready: true,
      version: 2,
      bundle_kind: "profile_config",
      endpoints_imported: 1,
      pricing_templates_imported: 0,
      strategies_imported: 1,
      models_imported: 1,
      connections_imported: 1,
      vendor_resolutions: [
        {
          vendor_key: "openai",
          resolution: "reuse",
          warning: null,
        },
      ],
      secret_key_id: "sha256:test",
      decryptable_secret_refs: ["endpoint:openai-main:api_key"],
      blocking_errors: [],
      warnings: [],
    } as never);

    const { result } = renderHook(() => useConfigBackupData({ bumpRevision: vi.fn() }));
    const file = {
      name: "config.json",
      text: vi.fn().mockResolvedValue(JSON.stringify(buildBackendShapedProfileBundle())),
    } as unknown as File;

    await act(async () => {
      await result.current.handleFileSelect({
        target: { files: [file] },
      } as unknown as ChangeEvent<HTMLInputElement>);
    });

    await waitFor(() => {
      expect(result.current.parsedConfig?.models[0]?.model_type).toBe("native");
      expect(result.current.parsedConfig?.models[0]?.proxy_targets).toEqual([]);
      expect(result.current.parsedConfig?.models[0]?.connections?.[0]?.openai_probe_endpoint_variant).toBe("responses_minimal");
      expect(result.current.parsedConfig?.loadbalance_strategies[0]?.timeout_policy?.attempt_open_timeout_ms).toBe(2000);
    });
    expect(api.config.previewImport).toHaveBeenCalledTimes(1);
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("imports a previewed backend-shaped bundle and resets local state", async () => {
    document.documentElement.lang = "en";
    const bumpRevision = vi.fn();
    const bundle = buildBackendShapedProfileBundle();
    vi.mocked(api.config.previewImport).mockResolvedValue({
      ready: true,
      version: 2,
      bundle_kind: "profile_config",
      endpoints_imported: 1,
      pricing_templates_imported: 0,
      strategies_imported: 1,
      models_imported: 1,
      connections_imported: 1,
      vendor_resolutions: [{ vendor_key: "openai", resolution: "reuse", warning: null }],
      secret_key_id: "sha256:test",
      decryptable_secret_refs: ["endpoint:openai-main:api_key"],
      blocking_errors: [],
      warnings: [],
    } as never);
    vi.mocked(api.config.import).mockResolvedValue({
      endpoints_imported: 1,
      pricing_templates_imported: 0,
      strategies_imported: 1,
      models_imported: 1,
      connections_imported: 1,
    } as never);

    const { result } = renderHook(() => useConfigBackupData({ bumpRevision }));
    const file = {
      name: "config.json",
      text: vi.fn().mockResolvedValue(JSON.stringify(bundle)),
    } as unknown as File;

    await act(async () => {
      await result.current.handleFileSelect({ target: { files: [file] } } as unknown as ChangeEvent<HTMLInputElement>);
    });

    await waitFor(() => {
      expect(result.current.previewResult?.ready).toBe(true);
    });

    await act(async () => {
      await result.current.handleImport();
    });

    expect(api.config.import).toHaveBeenCalledWith(bundle);
    expect(bumpRevision).toHaveBeenCalledTimes(1);
    expect(result.current.selectedFile).toBeNull();
    expect(result.current.parsedConfig).toBeNull();
    expect(result.current.previewResult).toBeNull();
    expect(toast.error).not.toHaveBeenCalled();
    expect(toast.success).toHaveBeenCalled();
  });

  it("clears stale parsed config and preview state when a new file is selected", async () => {
    document.documentElement.lang = "en";
    vi.mocked(api.config.previewImport).mockResolvedValue({
      ready: true,
      version: 2,
      bundle_kind: "profile_config",
      endpoints_imported: 1,
      pricing_templates_imported: 0,
      strategies_imported: 0,
      models_imported: 1,
      connections_imported: 0,
      vendor_resolutions: [],
      secret_key_id: "sha256:test",
      decryptable_secret_refs: ["endpoint:openai-main:api_key"],
      blocking_errors: [],
      warnings: [],
    } as never);

    const { result } = renderHook(() => useConfigBackupData({ bumpRevision: vi.fn() }));
    const firstFile = {
      name: "config-v2.json",
      text: vi.fn().mockResolvedValue(
        JSON.stringify({
          version: 2,
          bundle_kind: "profile_config",
          vendor_refs: [{ key: "openai", name_hint: "OpenAI", description_hint: null, icon_key_hint: "openai" }],
          endpoints: [{ name: "openai-main", base_url: "https://api.openai.com", api_key_secret_ref: "endpoint:openai-main:api_key", position: 0 }],
          pricing_templates: [],
          loadbalance_strategies: [],
          models: [{ vendor_key: "openai", api_family: "openai", model_id: "gateway-proxy", display_name: "Gateway Proxy", model_type: "proxy", proxy_targets: [], loadbalance_strategy_name: null, is_enabled: true, connections: [] }],
          profile_settings: { report_currency_code: "USD", report_currency_symbol: "$", timezone_preference: null, endpoint_fx_mappings: [] },
          secret_payload: { kind: "encrypted", cipher: "fernet-v1", key_id: "sha256:test", entries: [{ ref: "endpoint:openai-main:api_key", ciphertext: "enc:test-ciphertext" }] },
          header_blocklist_rules: [],
        }),
      ),
    } as unknown as File;

    await act(async () => {
      await result.current.handleFileSelect({
        target: { files: [firstFile] },
      } as unknown as ChangeEvent<HTMLInputElement>);
    });

    await waitFor(() => {
      expect(result.current.parsedConfig).not.toBeNull();
      expect(result.current.previewResult?.ready).toBe(true);
    });

    const deferred = { resolveText: undefined as ((value: string) => void) | undefined };
    const delayedText = new Promise<string>((resolve) => {
      deferred.resolveText = resolve;
    });
    const secondFile = {
      name: "config-v2-second.json",
      text: vi.fn().mockReturnValue(delayedText),
    } as unknown as File;

    await act(async () => {
      void result.current.handleFileSelect({
        target: { files: [secondFile] },
      } as unknown as ChangeEvent<HTMLInputElement>);
    });

    expect(result.current.parsedConfig).toBeNull();
    expect(result.current.previewResult).toBeNull();

    if (!deferred.resolveText) {
      throw new Error("Expected second file resolver to be initialized");
    }

    deferred.resolveText(
      JSON.stringify({
        version: 2,
        bundle_kind: "profile_config",
        vendor_refs: [{ key: "openai", name_hint: "OpenAI", description_hint: null, icon_key_hint: "openai" }],
        endpoints: [{ name: "openai-main", base_url: "https://api.openai.com", api_key_secret_ref: "endpoint:openai-main:api_key", position: 0 }],
        pricing_templates: [],
        loadbalance_strategies: [],
        models: [{ vendor_key: "openai", api_family: "openai", model_id: "gateway-proxy-2", display_name: "Gateway Proxy 2", model_type: "proxy", proxy_targets: [], loadbalance_strategy_name: null, is_enabled: true, connections: [] }],
        profile_settings: { report_currency_code: "USD", report_currency_symbol: "$", timezone_preference: null, endpoint_fx_mappings: [] },
        secret_payload: { kind: "encrypted", cipher: "fernet-v1", key_id: "sha256:test", entries: [{ ref: "endpoint:openai-main:api_key", ciphertext: "enc:test-ciphertext" }] },
        header_blocklist_rules: [],
      }),
    );

    await waitFor(() => {
      expect(result.current.parsedConfig?.models[0]?.model_id).toBe("gateway-proxy-2");
      expect(result.current.previewResult?.ready).toBe(true);
    });
  });

  it("ignores a stale preview response from a previously selected file", async () => {
    document.documentElement.lang = "en";
    const firstPreview = { resolve: undefined as ((value: unknown) => void) | undefined };
    vi.mocked(api.config.previewImport)
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            firstPreview.resolve = resolve;
          }) as never,
      )
      .mockResolvedValueOnce({
        ready: false,
        version: 2,
        bundle_kind: "profile_config",
        endpoints_imported: 1,
        pricing_templates_imported: 0,
        strategies_imported: 0,
        models_imported: 1,
        connections_imported: 0,
        vendor_resolutions: [],
        secret_key_id: "sha256:test",
        decryptable_secret_refs: ["endpoint:openai-main:api_key"],
        blocking_errors: ["second-file-error"],
        warnings: [],
      } as never);

    const { result } = renderHook(() => useConfigBackupData({ bumpRevision: vi.fn() }));
    const firstFile = {
      name: "config-v2-first.json",
      text: vi.fn().mockResolvedValue(
        JSON.stringify({
          version: 2,
          bundle_kind: "profile_config",
          vendor_refs: [{ key: "openai", name_hint: "OpenAI", description_hint: null, icon_key_hint: "openai" }],
          endpoints: [{ name: "openai-main", base_url: "https://api.openai.com", api_key_secret_ref: "endpoint:openai-main:api_key", position: 0 }],
          pricing_templates: [],
          loadbalance_strategies: [],
          models: [{ vendor_key: "openai", api_family: "openai", model_id: "first-model", display_name: null, model_type: "proxy", proxy_targets: [], loadbalance_strategy_name: null, is_enabled: true, connections: [] }],
          profile_settings: null,
          secret_payload: { kind: "encrypted", cipher: "fernet-v1", key_id: "sha256:test", entries: [{ ref: "endpoint:openai-main:api_key", ciphertext: "enc:test-ciphertext" }] },
          header_blocklist_rules: [],
        }),
      ),
    } as unknown as File;

    const secondFile = {
      name: "config-v2-second.json",
      text: vi.fn().mockResolvedValue(
        JSON.stringify({
          version: 2,
          bundle_kind: "profile_config",
          vendor_refs: [{ key: "openai", name_hint: "OpenAI", description_hint: null, icon_key_hint: "openai" }],
          endpoints: [{ name: "openai-main", base_url: "https://api.openai.com", api_key_secret_ref: "endpoint:openai-main:api_key", position: 0 }],
          pricing_templates: [],
          loadbalance_strategies: [],
          models: [{ vendor_key: "openai", api_family: "openai", model_id: "second-model", display_name: null, model_type: "proxy", proxy_targets: [], loadbalance_strategy_name: null, is_enabled: true, connections: [] }],
          profile_settings: null,
          secret_payload: { kind: "encrypted", cipher: "fernet-v1", key_id: "sha256:test", entries: [{ ref: "endpoint:openai-main:api_key", ciphertext: "enc:test-ciphertext" }] },
          header_blocklist_rules: [],
        }),
      ),
    } as unknown as File;

    await act(async () => {
      void result.current.handleFileSelect({ target: { files: [firstFile] } } as unknown as ChangeEvent<HTMLInputElement>);
    });

    await act(async () => {
      void result.current.handleFileSelect({ target: { files: [secondFile] } } as unknown as ChangeEvent<HTMLInputElement>);
    });

    if (!firstPreview.resolve) {
      throw new Error("Expected first preview to be pending");
    }
    firstPreview.resolve({
      ready: true,
      version: 2,
      bundle_kind: "profile_config",
      endpoints_imported: 1,
      pricing_templates_imported: 0,
      strategies_imported: 0,
      models_imported: 1,
      connections_imported: 0,
      vendor_resolutions: [],
      secret_key_id: "sha256:test",
      decryptable_secret_refs: ["endpoint:openai-main:api_key"],
      blocking_errors: [],
      warnings: [],
    });

    await waitFor(() => {
      expect(result.current.parsedConfig?.models[0]?.model_id).toBe("second-model");
      expect(result.current.previewResult?.ready).toBe(false);
      expect(result.current.previewResult?.blocking_errors).toEqual(["second-file-error"]);
    });
  });

  it("emits a localized retention validation toast", () => {
    const { result } = renderHook(() => useRetentionDeletionData());

    act(() => {
      result.current.setCleanupType("requests");
      result.current.setRetentionPreset("invalid" as never);
    });

    act(() => {
      result.current.handleOpenDeleteConfirm();
    });

    expect(toast.error).toHaveBeenCalledWith("请选择有效的保留选项");
  });

  it("emits localized authentication setup toasts", async () => {
    const { result } = renderHook(() =>
      useAuthenticationSettingsData({ navigate: vi.fn(), refreshAuth: vi.fn(), revision: 1 }),
    );

    await act(async () => {
      await result.current.handleRequestEmailVerification();
    });

    expect(toast.error).toHaveBeenCalledWith("邮箱为必填项");
  });
});
