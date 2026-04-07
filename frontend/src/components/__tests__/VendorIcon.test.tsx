import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { VendorIcon } from "../VendorIcon";
import { VendorSelect } from "../VendorSelect";

describe("VendorIcon", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );

    Object.defineProperty(Element.prototype, "scrollIntoView", {
      configurable: true,
      writable: true,
      value: vi.fn(),
    });
  });

  it("renders a curated preset icon for known vendor icon keys", () => {
    render(
      <VendorIcon
        vendor={{
          key: "zai",
          name: "Z.ai",
          icon_key: "zhipu",
        }}
      />,
    );

    const icon = screen.getByLabelText("Vendor icon Z.ai");
    const svg = icon.querySelector("svg");

    expect(svg).not.toBeNull();
    expect(icon.querySelector("img")).toBeNull();
    expect(svg).toHaveClass("h-full", "w-full");
  });

  it("renders the Gemini glyph for the gemini vendor icon key", () => {
    render(
      <VendorIcon
        vendor={{
          key: "gemini",
          name: "Gemini",
          icon_key: "gemini",
        }}
      />,
    );

    const geminiIcon = screen.getByLabelText("Vendor icon Gemini");
    expect(geminiIcon.querySelector("svg path")).toHaveAttribute(
      "d",
      "M12 0C12 6.627 6.627 12 0 12c6.627 0 12 5.373 12 12 0-6.627 5.373-12 12-12-6.627 0-12-5.373-12-12z",
    );
  });

  it("renders a fallback monogram for vendors without a preset icon", () => {
    render(
      <VendorIcon
        vendor={{
          key: "groq",
          name: "Groq",
          icon_key: null,
        }}
      />,
    );

    expect(screen.getByLabelText("Vendor icon Groq")).toHaveTextContent("G");
  });

  it("renders a generic placeholder when vendor metadata is completely empty", () => {
    render(<VendorIcon vendor={{ key: "", name: "", icon_key: null }} />);

    expect(screen.getByLabelText("Vendor icon placeholder")).toHaveTextContent("?");
  });

  it("renders vendor icons alongside labels inside VendorSelect", () => {
    render(
      <VendorSelect
        value="30"
        onValueChange={vi.fn()}
        valueType="vendor_id"
        vendors={[
          {
            id: 30,
            key: "zai",
            name: "Z.ai",
            description: "Z.ai Open Platform",
            icon_key: "zhipu",
            audit_enabled: false,
            audit_capture_bodies: false,
            created_at: "",
            updated_at: "",
          },
        ]}
        showAll={false}
      />,
    );

    fireEvent.click(screen.getByRole("combobox"));

    expect(screen.getAllByText("Z.ai").length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText("Vendor icon Z.ai").length).toBeGreaterThan(0);
  });
});
