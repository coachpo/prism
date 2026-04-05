import { act, renderHook, waitFor } from "@testing-library/react";
import { useEffect, type ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { useRequestLogPageState } from "../useRequestLogPageState";

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

describe("useRequestLogPageState", () => {
  it("defaults to the approved 1h time range when the URL is unset", () => {
    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper("/request-logs"),
    });

    expect(result.current.state.time_range).toBe("1h");
    expect(result.current.hasActiveFilters).toBe(false);
  });

  it("ignores removed filter query keys and actions", async () => {
    let currentLocation = "";

    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper(
        "/request-logs?offset=50&search=timeout&api_family=anthropic&outcome_filter=error&triage=true",
        (value) => {
          currentLocation = value;
        },
      ),
    });

    await waitFor(() => {
      expect(currentLocation).toBe("/request-logs?offset=50");
    });

    expect(result.current.hasActiveFilters).toBe(false);
    expect(result.current.state.offset).toBe(50);
    expect("search" in result.current.state).toBe(false);
    expect("api_family" in result.current.state).toBe(false);
    expect("outcome_filter" in result.current.state).toBe(false);
    expect("triage" in result.current.state).toBe(false);
    expect("setSearch" in result.current).toBe(false);
    expect("setApiFamily" in result.current).toBe(false);
    expect("setOutcomeFilter" in result.current).toBe(false);
    expect("setTriage" in result.current).toBe(false);
  });

  it("resets pagination offset when a server-backed filter changes", async () => {
    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper("/request-logs?offset=50"),
    });

    act(() => {
      result.current.setModelId("gpt-5.4");
    });

    await waitFor(() => {
      expect(result.current.state.model_id).toBe("gpt-5.4");
    });

    expect(result.current.state.offset).toBe(0);

    act(() => {
      result.current.setOffset(75);
    });

    await waitFor(() => {
      expect(result.current.state.offset).toBe(75);
    });

    act(() => {
      result.current.setStatusFamily("4xx");
    });

    await waitFor(() => {
      expect(result.current.state.status_family).toBe("4xx");
    });

    expect(result.current.state.offset).toBe(0);

    act(() => {
      result.current.setOffset(125);
    });

    await waitFor(() => {
      expect(result.current.state.offset).toBe(125);
    });

    act(() => {
      result.current.setIngressRequestId("ingress_req_42");
    });

    await waitFor(() => {
      expect(result.current.state.ingress_request_id).toBe("ingress_req_42");
    });

    expect(result.current.state.offset).toBe(0);
  });

  it("clears status-family filters while preserving exact-request mode state", async () => {
    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper(
        "/request-logs?status_family=5xx&model_id=gpt-5.4&search=timeout&request_id=123&detail_tab=audit",
      ),
    });

    act(() => {
      result.current.clearFilters();
    });

    await waitFor(() => {
      expect(result.current.state.status_family).toBe("all");
    });

    expect(result.current.state.model_id).toBe("");
    expect(result.current.state.time_range).toBe("1h");
    expect(result.current.state.request_id).toBe("123");
    expect(result.current.state.detail_tab).toBe("audit");
    expect("search" in result.current.state).toBe(false);
  });

  it("parses ingress_request_id from the URL and preserves it across state serialization", async () => {
    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper("/request-logs?ingress_request_id=ingress_req_42&offset=50"),
    });

    expect(result.current.state.ingress_request_id).toBe("ingress_req_42");
    expect(result.current.state.request_id).toBe("");

    act(() => {
      result.current.setModelId("gpt-5.4");
    });

    await waitFor(() => {
      expect(result.current.state.model_id).toBe("gpt-5.4");
    });

    expect(result.current.state.ingress_request_id).toBe("ingress_req_42");
    expect(result.current.state.offset).toBe(0);
  });

  it("enters exact-request mode through request_id lookup state", async () => {
    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper("/request-logs"),
    });

    expect(result.current.isExactMode).toBe(false);

    act(() => {
      result.current.setRequestId("42");
    });

    await waitFor(() => {
      expect(result.current.state.request_id).toBe("42");
    });

    expect(result.current.isExactMode).toBe(true);
    expect(result.current.state.detail_tab).toBe("overview");
  });

  it("keeps only retained browse filters active after serialization updates", async () => {
    const { result } = renderHook(() => useRequestLogPageState(), {
      wrapper: createWrapper("/request-logs?offset=50&model_id=gpt-5.4&status_family=4xx"),
    });

    act(() => {
      result.current.setTimeRange("7d");
    });

    await waitFor(() => {
      expect(result.current.state.time_range).toBe("7d");
    });

    expect(result.current.hasActiveFilters).toBe(true);
    expect("search" in result.current.state).toBe(false);
  });
});
