import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { PricingTemplate } from "@/lib/types";
import { DeletePricingTemplateDialog } from "../DeletePricingTemplateDialog";

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

describe("DeletePricingTemplateDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized delete dialog copy", () => {
    render(
      <LocaleProvider>
        <DeletePricingTemplateDialog
          deletePricingTemplateConfirm={{
            id: 1,
            profile_id: 1,
            name: "demo-template",
            version: 1,
            description: null,
            pricing_currency_code: "USD",
            pricing_unit: "PER_1M",
            input_price: "0.10",
            output_price: "0.20",
            cached_input_price: null,
            cache_creation_price: null,
            reasoning_price: null,
            missing_special_token_price_policy: "MAP_TO_OUTPUT",
            created_at: "",
            updated_at: "",
          }}
          deletePricingTemplateConflict={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          pricingTemplateDeleting={false}
          pricingTemplateUsageLoading={false}
          pricingTemplateUsageRows={[]}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除价格模板")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();
  });

  it("keeps the pricing template subject copy stable while the dialog is closing", () => {
    const template: PricingTemplate = {
      id: 1,
      profile_id: 1,
      name: "demo-template",
      version: 1,
      description: null,
      pricing_currency_code: "USD",
      pricing_unit: "PER_1M" as const,
      input_price: "0.10",
      output_price: "0.20",
      cached_input_price: null,
      cache_creation_price: null,
      reasoning_price: null,
      missing_special_token_price_policy: "MAP_TO_OUTPUT" as const,
      created_at: "",
      updated_at: "",
    };

    const { rerender } = render(
      <LocaleProvider>
        <DeletePricingTemplateDialog
          deletePricingTemplateConfirm={template}
          deletePricingTemplateConflict={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          pricingTemplateDeleting={false}
          pricingTemplateUsageLoading={false}
          pricingTemplateUsageRows={[]}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("确定要删除模板“demo-template”吗？")).toBeInTheDocument();

    rerender(
      <LocaleProvider>
        <DeletePricingTemplateDialog
          deletePricingTemplateConfirm={null}
          deletePricingTemplateConflict={null}
          onClose={vi.fn()}
          onDelete={vi.fn()}
          pricingTemplateDeleting={false}
          pricingTemplateUsageLoading={false}
          pricingTemplateUsageRows={[]}
        />
      </LocaleProvider>,
    );

    expect(screen.getByTestId("dialog-root")).toHaveAttribute("data-open", "false");
    expect(screen.getByText("确定要删除模板“demo-template”吗？")).toBeInTheDocument();
    expect(screen.queryByText("确定要删除模板“”吗？")).not.toBeInTheDocument();
  });
});
