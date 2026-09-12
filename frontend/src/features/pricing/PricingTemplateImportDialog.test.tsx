import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { PricingTemplateImportDialog } from "./PricingTemplateImportDialog";

function priceFile(contents: string) {
  const file = new File([contents], "prices.json", { type: "application/json" });
  Object.defineProperty(file, "text", { value: async () => contents });
  return file;
}

describe("price file import", () => {
  it("recovers from an unreadable format by choosing a valid file and only requests a preview", async () => {
    const user = userEvent.setup();
    const onImport = vi.fn().mockResolvedValue(true);
    render(<LocaleProvider><PricingTemplateImportDialog importing={false} open onClose={vi.fn()} onOpenChange={vi.fn()} onImport={onImport} /></LocaleProvider>);
    const upload = screen.getByLabelText("选择价格文件");
    const preview = screen.getByRole("button", { name: "预览导入变更" });
    expect(preview).toBeDisabled();
    await user.upload(upload, priceFile("not valid json"));
    await user.click(preview);
    expect(screen.getByText(/无法识别此价格文件/)).toBeInTheDocument();
    expect(onImport).not.toHaveBeenCalled();
    const template = { name: "日常价格", template_kind: "standard", card: { input_price: "1", output_price: "2" } };
    await user.upload(upload, priceFile(JSON.stringify({ templates: [template] })));
    await waitFor(() => expect(preview).toBeEnabled());
    await user.click(preview);
    await waitFor(() => expect(onImport).toHaveBeenCalledWith({ schema_version: 3, mode: "upsert_by_name", templates: [template] }));
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByText(/input_price/)).not.toBeInTheDocument();
  });
});
