import { act, renderHook, waitFor } from "@testing-library/react";
import { useEffect, type ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { useDashboardPageState } from "../useDashboardPageState";

function createWrapper(initialEntry: string, onLocationChange?: (value: string) => void) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter initialEntries={[initialEntry]}>
        <LocationProbe onLocationChange={onLocationChange} />
        {children}
      </MemoryRouter>
    );
  };
}

function LocationProbe({ onLocationChange }: { onLocationChange?: (value: string) => void }) {
  const location = useLocation();

  useEffect(() => {
    onLocationChange?.(`${location.pathname}${location.search}`);
  }, [location, onLocationChange]);

  return null;
}

describe("useDashboardPageState", () => {
  it("defaults to the overview tab when the URL is unset", () => {
    const { result } = renderHook(() => useDashboardPageState(), {
      wrapper: createWrapper("/dashboard"),
    });

    expect(result.current.state.tab).toBe("overview");
  });

  it("canonicalizes invalid dashboard tabs back to the base dashboard URL", async () => {
    let currentLocation = "";

    const { result } = renderHook(() => useDashboardPageState(), {
      wrapper: createWrapper("/dashboard?tab=detail", (value) => {
        currentLocation = value;
      }),
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=overview");
    });

    expect(result.current.state.tab).toBe("overview");
  });

  it("keeps analytics deep links stable", async () => {
    let currentLocation = "";

    const { result } = renderHook(() => useDashboardPageState(), {
      wrapper: createWrapper("/dashboard?tab=analytics", (value) => {
        currentLocation = value;
      }),
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=analytics");
    });

    expect(result.current.state.tab).toBe("analytics");
  });

  it("updates the address bar when the dashboard tab changes", async () => {
    let currentLocation = "";

    const { result } = renderHook(() => useDashboardPageState(), {
      wrapper: createWrapper("/dashboard", (value) => {
        currentLocation = value;
      }),
    });

    act(() => {
      result.current.setTab("analytics");
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=analytics");
    });

    expect(result.current.state.tab).toBe("analytics");

    act(() => {
      result.current.setTab("overview");
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/dashboard?tab=overview");
    });

    expect(result.current.state.tab).toBe("overview");
  });
});
