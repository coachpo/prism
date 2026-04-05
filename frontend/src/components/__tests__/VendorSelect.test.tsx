import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { VendorSelect } from "../VendorSelect";

const vendors = [
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
  {
    id: 31,
    key: "openai",
    name: "OpenAI",
    description: "OpenAI",
    icon_key: "openai",
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "",
    updated_at: "",
  },
];

describe("VendorSelect", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("renders the selected vendor through the select value slot", () => {
    render(
      <VendorSelect
        value="30"
        onValueChange={vi.fn()}
        valueType="vendor_id"
        vendors={vendors}
        showAll={false}
      />,
    );

    const combobox = screen.getByRole("combobox");
    const selectValue = combobox.querySelector('[data-slot="select-value"]');
    const selectedContent = selectValue?.firstElementChild;
    const selectedLabel = selectedContent?.querySelector("span:last-child");

    expect(selectValue).not.toBeNull();
    expect(combobox).toHaveClass("w-full", "min-w-0", "max-w-full");
    expect(selectValue).toHaveTextContent("Z.ai");
    expect(selectedContent).toHaveClass("min-w-0", "max-w-full", "flex", "items-center", "gap-2");
    expect(selectedLabel).toHaveClass("truncate");
  });

  it("uses a shrink-safe trigger contract for long selected vendor labels", () => {
    const longLabel = "OpenAI Ultra Long Vendor Label That Should Truncate Inside Dense Forms";

    render(
      <div className="w-48">
        <VendorSelect
          value="31"
          onValueChange={vi.fn()}
          valueType="vendor_id"
          vendors={vendors.map((vendor) =>
            vendor.id === 31 ? { ...vendor, name: longLabel } : vendor,
          )}
          showAll={false}
        />
      </div>,
    );

    const combobox = screen.getByRole("combobox");
    const triggerValue = combobox.querySelector('[data-slot="select-trigger-value"]');

    expect(combobox).toHaveClass("w-full", "min-w-0", "max-w-full");
    expect(triggerValue).not.toBeNull();
    expect(triggerValue).toHaveClass("min-w-0", "flex-1", "overflow-hidden");

    fireEvent.click(combobox);

    const selectContent = document.querySelector('[data-slot="select-content"]');

    expect(selectContent).not.toBeNull();
    expect(selectContent).toHaveClass(
      "w-[var(--radix-select-trigger-width)]",
      "min-w-[var(--radix-select-trigger-width)]",
    );
  });
});
