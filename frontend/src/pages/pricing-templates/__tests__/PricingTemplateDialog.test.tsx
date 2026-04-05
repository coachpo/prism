import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PricingTemplateDialog } from "../PricingTemplateDialog";

describe("PricingTemplateDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized pricing template dialog copy", () => {
    render(
      <LocaleProvider>
        <PricingTemplateDialog
          editingPricingTemplate={null}
          onClose={vi.fn()}
          onOpenChange={vi.fn()}
          onSave={vi.fn()}
          open={true}
          pricingTemplateForm={{
            name: "",
            description: "",
            pricing_currency_code: "USD",
            input_price: "0.10",
            output_price: "0.20",
            cached_input_price: "",
            cache_creation_price: "",
            reasoning_price: "",
            missing_special_token_price_policy: "MAP_TO_OUTPUT",
          }}
          pricingTemplateSaving={false}
          setPricingTemplateForm={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("新增价格模板")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存模板" })).toBeInTheDocument();
    expect(screen.getByLabelText("货币代码")).toBeInTheDocument();
    expect(screen.getByLabelText("描述（可选）")).toBeInTheDocument();
    expect(screen.getByText("特殊令牌缺失定价策略")).toBeInTheDocument();
    expect(
      screen.getByText("若未设置特殊令牌（如推理令牌）的专用价格，则按此策略计费。"),
    ).toBeInTheDocument();
  });

  it("submits through a real form and exposes stable field names", () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <LocaleProvider>
        <PricingTemplateDialog
          editingPricingTemplate={null}
          onClose={vi.fn()}
          onOpenChange={vi.fn()}
          onSave={onSave}
          open={true}
          pricingTemplateForm={{
            name: "Starter",
            description: "Base tier",
            pricing_currency_code: "USD",
            input_price: "0.10",
            output_price: "0.20",
            cached_input_price: "",
            cache_creation_price: "",
            reasoning_price: "",
            missing_special_token_price_policy: "MAP_TO_OUTPUT",
          }}
          pricingTemplateSaving={false}
          setPricingTemplateForm={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByLabelText("名称")).toHaveAttribute("name", "name");
    expect(screen.getByLabelText("名称")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("货币代码")).toHaveAttribute("name", "pricing_currency_code");
    expect(screen.getByLabelText("货币代码")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("描述（可选）")).toHaveAttribute("name", "description");
    expect(screen.getByLabelText("描述（可选）")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("输入价格（每 100 万令牌）")).toHaveAttribute("name", "input_price");
    expect(screen.getByLabelText("输入价格（每 100 万令牌）")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("输出价格（每 100 万令牌）")).toHaveAttribute("name", "output_price");
    expect(screen.getByLabelText("输出价格（每 100 万令牌）")).toHaveAttribute("autocomplete", "off");
    expect(document.querySelector('input[type="hidden"][name="missing_special_token_price_policy"]')).toHaveValue(
      "MAP_TO_OUTPUT",
    );

    const form = screen.getByRole("button", { name: "保存模板" }).closest("form");
    expect(form).not.toBeNull();

    fireEvent.submit(form!);

    expect(onSave).toHaveBeenCalledTimes(1);
  });
});
