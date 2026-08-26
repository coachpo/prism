import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  AuthSettings,
  ProxyApiKey,
  ProxyApiKeyCreateResponse,
  ProxyApiKeyRotateResponse,
  ProxyApiKeyUpdateResponse,
  ProxyKeyCapacity,
} from "@/lib/types";
import { rewriteQueryKeys } from "@/shared/api/queryKeys";
import { useProxyKeyCreateMutation } from "./useProxyKeyCreateMutation";
import { useProxyKeyDeleteMutation } from "./useProxyKeyDeleteMutation";
import { useProxyKeyEditMutation } from "./useProxyKeyEditMutation";
import { useProxyKeyRotateMutation } from "./useProxyKeyRotateMutation";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  delete: vi.fn(),
  rotate: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    settings: {
      auth: {
        proxyKeys: {
          create: mocks.create,
          delete: mocks.delete,
          rotate: mocks.rotate,
          update: mocks.update,
        },
      },
    },
  },
}));

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    proxyApiKeysData: {
      createFailed: "Create failed",
      created: "Created",
      deleteFailed: "Delete failed",
      deleted: "Deleted",
      keyNameRequired: "Name required",
      maxKeysReached: (limit: string) => `Maximum ${limit}`,
      rotateFailed: "Rotate failed",
      rotated: "Rotated",
      settingsUnavailable: "Settings unavailable",
      updateFailed: "Update failed",
      updated: "Updated",
    },
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

const capacity: ProxyKeyCapacity = {
  limit: 10,
  used: 1,
  remaining: 9,
  counted_at: "2026-08-26T00:00:00Z",
};
const authSettings = {} as AuthSettings;

function key(overrides: Partial<ProxyApiKey> = {}): ProxyApiKey {
  return {
    id: 7,
    name: "production",
    key_prefix: "pm-1234",
    key_preview: "pm-1234••••••••5678",
    is_active: true,
    expires_at: null,
    last_used_at: null,
    last_used_ip: null,
    notes: null,
    rotated_at: null,
    rotation_count: 0,
    created_at: "2026-08-26T00:00:00Z",
    updated_at: "2026-08-26T00:00:00Z",
    ...overrides,
  };
}

function withClient() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  return {
    client,
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  };
}

describe("Proxy-Key mutation lifecycle owners", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps create validation/form reset with the issue lifecycle", async () => {
    const current = key();
    const created = key({ id: 8, name: "staging" });
    const response = {
      key: "pm-created-secret",
      item: created,
      capacity,
    } as ProxyApiKeyCreateResponse;
    mocks.create.mockResolvedValue(response);
    const { client, wrapper } = withClient();
    client.setQueryData(rewriteQueryKeys.global.proxyApiKeys(), {
      items: [current],
      capacity,
    });
    const showCreatedSecret = vi.fn();
    const { result } = renderHook(
      () =>
        useProxyKeyCreateMutation({
          authSettings,
          capacity,
          proxyKeyLimit: 10,
          remainingKeys: 9,
          showCreatedSecret,
        }),
      { wrapper },
    );

    act(() => {
      result.current.setProxyKeyName("  staging  ");
      result.current.setProxyKeyNotes("  notes  ");
    });
    await act(async () => {
      await result.current.handleCreateSubmit({
        preventDefault: vi.fn(),
      } as never);
    });

    expect(mocks.create).toHaveBeenCalledWith(
      {
        name: "staging",
        notes: "notes",
        expires_at: null,
      },
      expect.anything(),
    );
    expect(showCreatedSecret).toHaveBeenCalledWith(response);
    expect(result.current.proxyKeyName).toBe("");
    expect(result.current.issueSheetOpen).toBe(false);
    expect(
      client.getQueryData(rewriteQueryKeys.global.proxyApiKeys()),
    ).toMatchObject({ items: [created, current], capacity });
  });

  it("keeps edit payload shaping and server-item patch with the edit lifecycle", async () => {
    const original = key();
    const updated = key({ name: "production-updated" });
    const response = {
      item: updated,
      capacity,
    } as ProxyApiKeyUpdateResponse;
    mocks.update.mockResolvedValue(response);
    const { client, wrapper } = withClient();
    client.setQueryData(rewriteQueryKeys.global.proxyApiKeys(), {
      items: [original],
      capacity,
    });
    const { result } = renderHook(() => useProxyKeyEditMutation(), { wrapper });

    act(() => {
      result.current.startEditingProxyKey(original);
      result.current.setEditingProxyKeyName("  production-updated  ");
    });
    await act(async () => {
      await result.current.handleEditSubmit({
        preventDefault: vi.fn(),
      } as never);
    });

    expect(mocks.update).toHaveBeenCalledWith(original.id, {
      name: "production-updated",
      notes: null,
      is_active: true,
    });
    expect(result.current.editProxyKeySheetOpen).toBe(false);
    expect(
      client.getQueryData(rewriteQueryKeys.global.proxyApiKeys()),
    ).toMatchObject({ items: [updated], capacity });
  });

  it("keeps rotation confirmation and secret handoff with the rotate lifecycle", async () => {
    const original = key();
    const rotated = key({ rotation_count: 1 });
    const response = {
      key: "pm-rotated-secret",
      item: rotated,
      capacity,
    } as ProxyApiKeyRotateResponse;
    mocks.rotate.mockResolvedValue(response);
    const { client, wrapper } = withClient();
    client.setQueryData(rewriteQueryKeys.global.proxyApiKeys(), {
      items: [original],
      capacity,
    });
    const showRotatedSecret = vi.fn();
    const { result } = renderHook(
      () => useProxyKeyRotateMutation({ showRotatedSecret }),
      { wrapper },
    );

    act(() => {
      result.current.setRotateConfirm(original);
    });
    await act(async () => {
      await result.current.handleRotateProxyKey();
    });

    expect(mocks.rotate).toHaveBeenCalledWith(original.id);
    expect(showRotatedSecret).toHaveBeenCalledWith(response);
    expect(result.current.rotateConfirm).toBeNull();
    expect(result.current.rotateProxyKeyAlertOpen).toBe(false);
    expect(
      client.getQueryData(rewriteQueryKeys.global.proxyApiKeys()),
    ).toMatchObject({ items: [rotated], capacity });
  });

  it("keeps delete confirmation and ledger removal with the delete lifecycle", async () => {
    const deleting = key();
    const remaining = key({ id: 8, name: "staging" });
    mocks.delete.mockResolvedValue({ deleted_id: deleting.id, capacity });
    const { client, wrapper } = withClient();
    client.setQueryData(rewriteQueryKeys.global.proxyApiKeys(), {
      items: [deleting, remaining],
      capacity,
    });
    const { result } = renderHook(() => useProxyKeyDeleteMutation(), { wrapper });

    act(() => {
      result.current.setDeleteConfirm(deleting);
    });
    await act(async () => {
      await result.current.handleDeleteProxyKey();
    });

    expect(mocks.delete).toHaveBeenCalledWith(deleting.id);
    expect(result.current.deleteConfirm).toBeNull();
    expect(result.current.deleteProxyKeyAlertOpen).toBe(false);
    expect(
      client.getQueryData(rewriteQueryKeys.global.proxyApiKeys()),
    ).toMatchObject({ items: [remaining], capacity });
  });
});
