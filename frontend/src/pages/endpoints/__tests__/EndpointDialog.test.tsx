import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { EndpointDialog } from "../EndpointDialog";

describe("EndpointDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized endpoint form labels and helper text", () => {
    render(
      <LocaleProvider>
        <EndpointDialog
          open={true}
          onOpenChange={vi.fn()}
          onSubmit={vi.fn()}
          title="新增端点"
          submitLabel="保存端点"
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("名称")).toBeInTheDocument();
    expect(screen.getByText("基础 URL")).toBeInTheDocument();
    expect(screen.getByText("API 密钥")).toBeInTheDocument();
    expect(screen.getByLabelText("名称")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("基础 URL")).toHaveAttribute("autocomplete", "off");
    expect(screen.getByLabelText("API 密钥")).toHaveAttribute("autocomplete", "off");
  });
});
