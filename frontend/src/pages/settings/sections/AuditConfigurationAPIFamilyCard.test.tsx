import { createRef } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { AuditConfigurationAPIFamilyCard } from "./AuditConfigurationAPIFamilyCard";
import type { AuditAPIFamilySetting } from "@/lib/types";

const apiFamilyAuditSettings: AuditAPIFamilySetting[] = [
  { api_family: "openai", audit_enabled: false, audit_capture_bodies: false },
  { api_family: "anthropic", audit_enabled: true, audit_capture_bodies: true },
  { api_family: "gemini", audit_enabled: false, audit_capture_bodies: false },
];

function renderCard(overrides?: Partial<Parameters<typeof AuditConfigurationAPIFamilyCard>[0]>) {
  const props = {
    apiFamilyAuditSettings,
    apiFamilyAuditSettingsDirty: true,
    cardRef: createRef<HTMLDivElement>(),
    loadingAPIFamilyAuditSettings: false,
    renderSectionSaveState: () => null,
    savingAPIFamilyAuditSettings: false,
    setAPIFamilyAuditCaptureBodies: vi.fn(),
    setAPIFamilyAuditEnabled: vi.fn(),
    ...overrides,
  };

  render(
    <LocaleProvider>
      <AuditConfigurationAPIFamilyCard {...props} />
    </LocaleProvider>,
  );

  return props;
}

describe("AuditConfigurationAPIFamilyCard", () => {
  it("renders the fixed API-family order and disables capture when audit is off", () => {
    renderCard();

    const rows = screen.getAllByTestId(/audit-api-family-row-/);
    expect(rows.map((row) => row.textContent)).toEqual([
      "OpenAI",
      "Anthropic",
      "Gemini",
    ]);
    expect(screen.getByRole("switch", { name: "OpenAI 捕获正文" })).toBeDisabled();
    expect(screen.getByRole("switch", { name: "Anthropic 捕获正文" })).toBeEnabled();
    expect(screen.getByRole("switch", { name: "Gemini 捕获正文" })).toBeDisabled();
  });

  it("sends family-specific switch updates through the card callbacks", async () => {
    const user = userEvent.setup();
    const props = renderCard();

    await user.click(screen.getByRole("switch", { name: "OpenAI 启用审计" }));
    await user.click(screen.getByRole("switch", { name: "Anthropic 捕获正文" }));

    expect(props.setAPIFamilyAuditEnabled).toHaveBeenCalledWith("openai", true);
    expect(props.setAPIFamilyAuditCaptureBodies).toHaveBeenCalledWith("anthropic", false);
  });

  it("owns no save control: saving is the page header's single action", () => {
    renderCard();

    expect(screen.queryByRole("button", { name: "保存审计设置" })).toBeNull();
  });
});
