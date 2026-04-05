import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DeleteEndpointDialog } from "../DeleteEndpointDialog";

vi.mock("@/components/ui/dialog", () => ({
  Dialog: ({ children, open }: { children: ReactNode; open: boolean }) => (
    <div data-testid="dialog-root" data-open={String(open)}>
      {children}
    </div>
  ),
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

describe("DeleteEndpointDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized delete dialog copy", () => {
    render(
      <LocaleProvider>
        <DeleteEndpointDialog
          deleteTarget={{
            id: 1,
            profile_id: 1,
            name: "Primary OpenAI",
            base_url: "https://api.openai.com",
            has_api_key: true,
            masked_api_key: "sk-***",
            position: 0,
            created_at: "",
            updated_at: "",
          }}
          isDeletingEndpoint={false}
          onConfirm={vi.fn()}
          onOpenChange={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除端点")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();
  });

  it("keeps the endpoint subject copy stable while the dialog is closing", () => {
    const deleteTarget = {
      id: 1,
      profile_id: 1,
      name: "Primary OpenAI",
      base_url: "https://api.openai.com",
      has_api_key: true,
      masked_api_key: "sk-***",
      position: 0,
      created_at: "",
      updated_at: "",
    };

    const { rerender } = render(
      <LocaleProvider>
        <DeleteEndpointDialog
          deleteTarget={deleteTarget}
          isDeletingEndpoint={false}
          onConfirm={vi.fn()}
          onOpenChange={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("确定要删除“Primary OpenAI”吗？此操作无法撤销。")).toBeInTheDocument();

    rerender(
      <LocaleProvider>
        <DeleteEndpointDialog
          deleteTarget={null}
          isDeletingEndpoint={false}
          onConfirm={vi.fn()}
          onOpenChange={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByTestId("dialog-root")).toHaveAttribute("data-open", "false");
    expect(screen.getByText("确定要删除“Primary OpenAI”吗？此操作无法撤销。")).toBeInTheDocument();
    expect(screen.queryByText("确定要删除“”吗？此操作无法撤销。")).not.toBeInTheDocument();
  });
});
