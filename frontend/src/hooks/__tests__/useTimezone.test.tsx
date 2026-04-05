import { StrictMode, type ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTimezone } from "@/hooks/useTimezone";
import { clearUserTimezonePreference } from "@/lib/timezone";

const api = vi.hoisted(() => ({
  settings: {
    timezone: {
      get: vi.fn(),
    },
  },
}));

const profileState = vi.hoisted(() => ({
  revision: 1,
  selectedProfileId: 7,
}));

const localeState = vi.hoisted(() => ({
  locale: "en" as "en" | "zh-CN",
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => profileState,
}));
vi.mock("@/i18n/useLocale", () => ({
  useLocale: () => localeState,
}));

function StrictWrapper({ children }: { children: ReactNode }) {
  return <StrictMode>{children}</StrictMode>;
}

describe("useTimezone", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    profileState.revision = 1;
    profileState.selectedProfileId = 7;
    localeState.locale = "en";
    clearUserTimezonePreference();
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: "Europe/Helsinki" });
  });

  afterEach(() => {
    cleanup();
    clearUserTimezonePreference();
  });

  it("reuses a shared cached timezone preference across hook instances", async () => {
    const first = renderHook(() => useTimezone(), { wrapper: StrictWrapper });
    const second = renderHook(() => useTimezone(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(first.result.current.loading).toBe(false);
      expect(second.result.current.loading).toBe(false);
    });

    expect(api.settings.timezone.get).toHaveBeenCalledTimes(1);
    expect(first.result.current.timezone).toBe("Europe/Helsinki");
    expect(second.result.current.timezone).toBe("Europe/Helsinki");
  });

  it("forces a fresh settings fetch when refresh is called", async () => {
    const { result } = renderHook(() => useTimezone(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    api.settings.timezone.get.mockResolvedValueOnce({ timezone_preference: "UTC" });

    await act(async () => {
      await result.current.refresh();
    });

    expect(api.settings.timezone.get).toHaveBeenCalledTimes(2);
    expect(result.current.timezone).toBe("UTC");
  });

  it("formats timestamps with the active locale", async () => {
    const { result, rerender } = renderHook(() => useTimezone(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    const enFormatted = result.current.format("2026-03-13T12:00:00Z");

    localeState.locale = "zh-CN";
    rerender();

    const zhFormatted = result.current.format("2026-03-13T12:00:00Z");
    expect(zhFormatted).not.toBe(enFormatted);
  });
});
