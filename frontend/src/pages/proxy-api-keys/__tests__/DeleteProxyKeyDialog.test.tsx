import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ProxyApiKey } from "@/lib/types";
import { DeleteProxyKeyDialog } from "../DeleteProxyKeyDialog";

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

describe("DeleteProxyKeyDialog", () => {
  it("shows the selected key name and prefix in the confirmation dialog", () => {
    render(
      <LocaleProvider>
        <DeleteProxyKeyDialog
          deleteConfirm={buildProxyKey()}
          deletingProxyKeyId={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          onOpenChange={vi.fn()}
        />
      </LocaleProvider>
    );

    expect(screen.getByRole("heading", { name: "Delete Proxy API Key" })).toBeInTheDocument();
    expect(screen.getByText(/Primary runtime key/)).toBeInTheDocument();
    expect(screen.getByText(/Confirm the prefix prism before continuing/)).toBeInTheDocument();
  });
});
