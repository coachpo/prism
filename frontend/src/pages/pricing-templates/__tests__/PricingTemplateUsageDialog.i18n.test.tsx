import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PricingTemplateUsageDialog } from "../PricingTemplateUsageDialog";

describe("PricingTemplateUsageDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized usage dialog copy", () => {
    render(
      <LocaleProvider>
        <PricingTemplateUsageDialog
          open={true}
          onOpenChange={vi.fn()}
          pricingTemplateUsageLoading={false}
          pricingTemplateUsageRows={[]}
          pricingTemplateUsageTemplate={{
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
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("模板使用情况")).toBeInTheDocument();
    expect(screen.getByText("此模板当前没有被任何连接使用。")) .toBeInTheDocument();
    expect(screen.getByRole("button", { name: "关闭" })).toBeInTheDocument();
  });
});
