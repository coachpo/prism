import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ProxyApiKey } from "@/lib/types";
import { ProxyKeysListCard } from "../ProxyKeysListCard";

function buildProxyKey(overrides: Partial<ProxyApiKey> = {}): ProxyApiKey {
  return {
    id: 1,
    name: "Primary runtime key",
    key_prefix: "prism",
    key_preview: "prism_****************",
    is_active: true,
    expires_at: null,
    last_used_at: null,
    last_used_ip: null,
    notes: "Used by the primary ingress.",
    rotated_from_id: null,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-21T10:00:00Z",
    ...overrides,
  };
}

describe("ProxyKeysListCard", () => {
  it("renders the shared empty-state copy when no proxy keys exist", () => {
    render(
      <LocaleProvider>
        <ProxyKeysListCard
          authEnabled={true}
          deletingProxyKeyId={null}
          displayedProxyKeys={[]}
          onDelete={vi.fn()}
          onEdit={vi.fn()}
          onRotate={vi.fn()}
          rotatingProxyKeyId={null}
        />
      </LocaleProvider>
    );

    expect(screen.getByText("Issued keys")).toBeInTheDocument();
    expect(screen.getByText("No proxy keys created yet.")).toBeInTheDocument();
    expect(screen.getByText("0 keys")).toBeInTheDocument();
  });

  it("keeps rendering proxy key rows when keys are present", () => {
    render(
      <LocaleProvider>
        <ProxyKeysListCard
          authEnabled={true}
          deletingProxyKeyId={null}
          displayedProxyKeys={[
            buildProxyKey(),
            buildProxyKey({
              id: 2,
              name: "Secondary runtime key",
              key_preview: "prism_alt_************",
              notes: "Fallback ingress credential.",
            }),
          ]}
          onDelete={vi.fn()}
          onEdit={vi.fn()}
          onRotate={vi.fn()}
          rotatingProxyKeyId={null}
        />
      </LocaleProvider>
    );

    expect(screen.getByRole("columnheader", { name: "Name / note" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Preview" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Created" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Updated" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Last used" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Last IP" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Operation" })).toBeInTheDocument();
    expect(screen.getByText("Primary runtime key")).toBeInTheDocument();
    expect(screen.getByText("Used by the primary ingress.")).toBeInTheDocument();
    expect(screen.getByText("Secondary runtime key")).toBeInTheDocument();
    expect(screen.getByText("prism_****************")).toBeInTheDocument();
    expect(screen.getByText("prism_alt_************")).toBeInTheDocument();
    expect(screen.queryByText("No proxy keys created yet.")).not.toBeInTheDocument();
  });

  it("renders localized proxy-key list copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    render(
      <LocaleProvider>
        <ProxyKeysListCard
          authEnabled={true}
          deletingProxyKeyId={null}
          displayedProxyKeys={[]}
          onDelete={vi.fn()}
          onEdit={vi.fn()}
          onRotate={vi.fn()}
          rotatingProxyKeyId={null}
        />
      </LocaleProvider>
    );

    expect(screen.getByText("已发放密钥")).toBeInTheDocument();
    expect(screen.getByText("还没有创建代理密钥。")) .toBeInTheDocument();
    expect(screen.getByText("0 个密钥")).toBeInTheDocument();
  });
});
