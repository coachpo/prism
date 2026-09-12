import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PricingTemplateDialog } from "./PricingTemplateDialog";

const currencyState = vi.hoisted(() => ({
  ready: true,
  degraded: false,
  currency: { code: "EUR", symbol: "€" },
  refresh: vi.fn(),
}));
vi.mock("@/context/ReportingCurrencyContext", () => ({
  useReportingCurrencyContext: () => currencyState,
}));

function dialogProps() {
  return {
    editingPricingTemplate: null,
    impact: null,
    impactError: null,
    impactLoading: false,
    onClose: vi.fn(),
    onOpenChange: vi.fn(),
    onRetryImpact: vi.fn(),
    onSave: vi.fn().mockResolvedValue(undefined),
    open: true,
    pricingTemplateSaving: false,
  };
}

beforeEach(() => {
  currencyState.ready = true;
  currencyState.degraded = false;
  currencyState.refresh.mockReset();
});

describe("pricing template form recovery", () => {
  it("shows the actual currency and required price errors beside the editable fields, then saves corrected values", async () => {
    const user = userEvent.setup();
    const props = dialogProps();
    render(<LocaleProvider><PricingTemplateDialog {...props} /></LocaleProvider>);
    expect(screen.getByTestId("pricing-currency-unit")).toHaveTextContent("EUR（€）");
    const name = screen.getByRole("textbox", { name: "名称（必填）" });
    const input = screen.getByRole("textbox", { name: "输入价格（必填）" });
    const output = screen.getByRole("textbox", { name: "输出价格（必填）" });
    const cache = screen.getByRole("textbox", { name: "缓存读取价格（可选）" });
    await user.type(name, "日常模型价格");
    await user.type(input, "-1");
    await user.click(screen.getByRole("button", { name: "保存模板" }));
    await waitFor(() => expect(input).toHaveAttribute("aria-invalid", "true"));
    expect(output).toHaveAttribute("aria-invalid", "true");
    expect(input).toHaveAccessibleDescription(/请输入非负十进制价格/);
    expect(props.onSave).not.toHaveBeenCalled();
    await user.clear(input);
    await user.type(input, "1.25");
    await user.type(output, "2.5");
    await user.type(cache, "0");
    await user.click(screen.getByRole("button", { name: "保存模板" }));
    await waitFor(() => expect(props.onSave).toHaveBeenCalledWith(expect.objectContaining({
      input_price: "1.25", output_price: "2.5", cached_input_price: "0", cache_creation_price: "",
    })));
  });

  it("retains entered prices after a save failure and discards them only after closing", async () => {
    const user = userEvent.setup();
    const props = dialogProps();
    const { rerender } = render(<LocaleProvider><PricingTemplateDialog {...props} /></LocaleProvider>);
    await user.type(screen.getByRole("textbox", { name: "名称（必填）" }), "待保存价格");
    await user.type(screen.getByRole("textbox", { name: "输入价格（必填）" }), "1.5");
    rerender(<LocaleProvider><PricingTemplateDialog {...props} serverValidation={{ issues: [], summary: "服务暂时不可用，请重试。" }} /></LocaleProvider>);
    expect(screen.getByRole("textbox", { name: "输入价格（必填）" })).toHaveValue("1.5");
    expect(screen.getByTestId("pricing-form-server-error")).toHaveTextContent("填写内容已保留");
    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(props.onClose).toHaveBeenCalledOnce();
    rerender(<LocaleProvider><PricingTemplateDialog {...props} open={false} /></LocaleProvider>);
    rerender(<LocaleProvider><PricingTemplateDialog {...props} /></LocaleProvider>);
    expect(screen.getByRole("textbox", { name: "输入价格（必填）" })).toHaveValue("");
  });

  it("does not present a fallback currency as fact or allow a save before retrying the currency read", async () => {
    const user = userEvent.setup();
    currencyState.degraded = true;
    render(<LocaleProvider><PricingTemplateDialog {...dialogProps()} /></LocaleProvider>);
    expect(screen.queryByTestId("pricing-currency-unit")).not.toBeInTheDocument();
    expect(screen.getByText("暂时无法确认计价币种")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存模板" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "重试" }));
    expect(currencyState.refresh).toHaveBeenCalledOnce();
  });
});
