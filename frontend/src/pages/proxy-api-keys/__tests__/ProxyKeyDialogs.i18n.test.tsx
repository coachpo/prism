import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DeleteProxyKeyDialog } from "../DeleteProxyKeyDialog";
import { EditProxyKeyDialog } from "../EditProxyKeyDialog";

describe("proxy key dialogs i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("renders localized edit dialog copy", () => {
    render(
      <LocaleProvider>
        <EditProxyKeyDialog
          open={true}
          proxyKeyActive={true}
          proxyKeyName="Primary runtime key"
          proxyKeyNotes="notes"
          saving={false}
          onOpenChange={vi.fn()}
          onSubmit={vi.fn()}
          setProxyKeyActive={vi.fn()}
          setProxyKeyName={vi.fn()}
          setProxyKeyNotes={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("编辑代理 API 密钥")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存" })).toBeInTheDocument();
  });

  it("exposes stable form names for proxy key edit fields", () => {
    render(
      <LocaleProvider>
        <EditProxyKeyDialog
          open={true}
          proxyKeyActive={true}
          proxyKeyName="Primary runtime key"
          proxyKeyNotes="notes"
          saving={false}
          onOpenChange={vi.fn()}
          onSubmit={vi.fn()}
          setProxyKeyActive={vi.fn()}
          setProxyKeyName={vi.fn()}
          setProxyKeyNotes={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByDisplayValue("Primary runtime key")).toHaveAttribute("name", "proxy-key-name");
    expect(screen.getByDisplayValue("notes")).toHaveAttribute("name", "proxy-key-notes");
  });

  it("renders localized delete dialog copy", () => {
    render(
      <LocaleProvider>
        <DeleteProxyKeyDialog
          deleteConfirm={{
            id: 1,
            name: "Primary runtime key",
            key_prefix: "prism",
            key_preview: "prism_****",
            is_active: true,
            expires_at: null,
            last_used_at: null,
            last_used_ip: null,
            notes: null,
            rotated_from_id: null,
            created_at: "",
            updated_at: "",
          }}
          deletingProxyKeyId={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          onOpenChange={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除代理 API 密钥")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除密钥" })).toBeInTheDocument();
  });
});
