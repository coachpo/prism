import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TimezoneSection } from "../sections/TimezoneSection";

function renderTimezoneSection() {
  return render(
    <LocaleProvider>
      <TimezoneSection
        timezoneDirty={false}
        renderSectionSaveState={() => null}
        handleSaveCostingSettings={vi.fn()}
        costingUnavailable={false}
        costingLoading={false}
        costingSaving={false}
        costingForm={{
          report_currency_code: "USD",
          report_currency_symbol: "$",
          timezone_preference: "America/North_Dakota/New_Salem",
          endpoint_fx_mappings: [],
        }}
        setCostingForm={vi.fn()}
        timezonePreviewText="2026-02-27 15:39"
        timezonePreviewZone="America/North_Dakota/New_Salem"
      />
    </LocaleProvider>,
  );
}

describe("TimezoneSection", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("uses shrink-safe field wrappers for long selected timezone values", () => {
    renderTimezoneSection();

    const trigger = screen.getByRole("combobox");
    const field = screen.getByText("Timezone preference").closest("div");
    const grid = field?.parentElement;
    const contentStack = grid?.parentElement;

    expect(trigger).toHaveTextContent("America/North_Dakota/New_Salem");
    expect(trigger).toHaveClass("w-full", "min-w-0", "max-w-full");
    expect(field).toHaveClass("min-w-0");
    expect(grid).toHaveClass("min-w-0");
    expect(contentStack).toHaveClass("min-w-0");
  });
});
