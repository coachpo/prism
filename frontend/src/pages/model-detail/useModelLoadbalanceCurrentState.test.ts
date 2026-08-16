import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { GlobalCurrentStateItem } from "@/lib/types";
import { useModelLoadbalanceCurrentState } from "./useModelLoadbalanceCurrentState";

const mocks = vi.hoisted(() => ({ listCurrentState: vi.fn(), toastError: vi.fn() }));

vi.mock("@/lib/api", () => ({
  api: { loadbalance: { listCurrentState: mocks.listCurrentState, resetCurrentState: vi.fn() } },
}));

vi.mock("sonner", () => ({ toast: { error: mocks.toastError } }));

function observedItem(terminalTargetId: number): GlobalCurrentStateItem {
  return {
    model: { model_config_id: 8, id: "deepseek-v4-flash", label: "DeepSeek", configured: true },
    endpoint: { id: 1, label: "endpoint", configured: true },
    terminal_target: { id: terminalTargetId, label: `target-${terminalTargetId}`, configured: true },
    observation_state: "observed",
    state: "available",
    available: true,
    cycle_retry_attempts: 0,
    cumulative_retry_attempts: 0,
    next_retry_at: null,
    last_retry_delay_ms: 0,
    ban_mode: "off",
    banned_until_at: null,
    last_failure_kind: null,
    last_success_at: "2026-08-08T08:00:00Z",
    last_success_response_headers_latency_ms: 412,
    in_flight_stream: 0,
    in_flight_non_stream: 0,
    qps_window_started_at: null,
    qps_window_request_count: 0,
    created_at: "2026-08-08T07:00:00Z",
    updated_at: "2026-08-08T08:00:00Z",
  };
}

function response(items: GlobalCurrentStateItem[], overrides: Record<string, unknown> = {}) {
  return {
    generated_at: "2026-08-08T08:00:00Z",
    scope: "process",
    instance_id: "i-1",
    configuration_revision: "1",
    completeness: {
      state: "ready",
      complete: true,
      configured_target_count: items.length,
      observed_target_count: items.length,
      unobserved_target_count: 0,
      observed_subset_counts: {},
    },
    items,
    has_more: false,
    next_cursor: null,
    ...overrides,
  };
}

function renderCurrentState(modelId: string | undefined = "deepseek-v4-flash") {
  return renderHook(() => useModelLoadbalanceCurrentState({ modelId, revision: 0, enabled: true }));
}

describe("model current-state reads keep absence honest", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("queries the read model with the public model id it was given", async () => {
    mocks.listCurrentState.mockResolvedValue(response([observedItem(12)]));
    renderCurrentState("deepseek-v4-flash");

    await waitFor(() => {
      expect(mocks.listCurrentState).toHaveBeenCalledWith({ model_id: "deepseek-v4-flash" });
    });
    // A numeric config id would never match `mc.model_id` server-side and the
    // whole cohort would come back empty while looking like "never observed".
    expect(mocks.listCurrentState).not.toHaveBeenCalledWith({ model_id: "8" });
  });

  it("separates a row observed only in part from a row never observed", async () => {
    const partial = { ...observedItem(12), in_flight_non_stream: null };
    const unobserved = { ...observedItem(14), observation_state: "unobserved" as const };
    mocks.listCurrentState.mockResolvedValue(response([partial, unobserved, observedItem(16)]));

    const { result } = renderCurrentState();

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.has(16)).toBe(true);
    });
    // Neither incomplete row is silently dropped: dropping them would make them
    // indistinguishable from rows the cohort never contained.
    expect(result.current.currentStateGapByConnectionId.get(12)).toBe("partial");
    expect(result.current.currentStateGapByConnectionId.get(14)).toBe("unobserved");
    expect(result.current.currentStateByConnectionId.has(12)).toBe(false);
    expect(result.current.currentStateByConnectionId.has(14)).toBe(false);
  });

  it("reports a first-read failure as a failure rather than an empty cohort", async () => {
    mocks.listCurrentState.mockRejectedValue(new Error("upstream unreachable"));

    const { result } = renderCurrentState();

    await waitFor(() => {
      expect(result.current.currentStateFailure?.message).toBe("upstream unreachable");
    });
    expect(result.current.currentStateFailure?.staleData).toBe(false);
    expect(result.current.currentStateCompleteness).toBeNull();
    expect(result.current.currentStateByConnectionId.size).toBe(0);
  });

  it("keeps rows on screen and marks them stale when a refresh fails", async () => {
    mocks.listCurrentState.mockResolvedValueOnce(response([observedItem(12)]));
    const { result } = renderCurrentState();

    await waitFor(() => {
      expect(result.current.currentStateByConnectionId.has(12)).toBe(true);
    });

    mocks.listCurrentState.mockRejectedValueOnce(new Error("refresh failed"));
    await result.current.refreshCurrentState();

    await waitFor(() => {
      expect(result.current.currentStateFailure?.staleData).toBe(true);
    });
    expect(result.current.currentStateByConnectionId.has(12)).toBe(true);
  });

  it("carries the truncation flag so absence below the page limit proves nothing", async () => {
    mocks.listCurrentState.mockResolvedValue(response([observedItem(12)], { has_more: true }));

    const { result } = renderCurrentState();

    await waitFor(() => {
      expect(result.current.currentStateCompleteness?.hasMore).toBe(true);
    });
    expect(result.current.currentStateCompleteness?.state).toBe("ready");
  });
});
