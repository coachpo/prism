import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { clearUserTimezonePreference } from "@/lib/timezone";
import type {
  RetentionAffectedDomain,
  RetentionPreflightResponse,
  RetentionSettingsResponse,
} from "@/lib/types";
import { rewriteTestServer } from "@/test/msw/server";
import { DeleteConfirmDialog } from "./dialogs/DeleteConfirmDialog";
import { useRetentionDeletionData } from "./useRetentionDeletionData";

function affectedDomain(): RetentionAffectedDomain {
  return {
    dataset: "request_logs",
    owner_snapshot: {},
    impact: {
      change: {},
      resolved_cutoff: "2026-07-14T00:00:00Z",
      logical_coverage_after: {
        from_time: "2026-07-14T00:00:00Z",
        to_time: "2026-08-13T00:00:00Z",
        gaps: [],
        accuracy: "exact",
        basis: "owner",
      },
      physical_reclaim_not_before: null,
      matched_rows: { value: "1024", accuracy: "exact", method: "count" },
      retained_rows: { value: "2048", accuracy: "exact", method: "count" },
      matched_logical_bytes: { value: "4096", accuracy: "estimated", basis: "owner" },
      reclaimable_physical_bytes: { value: "4096", accuracy: "estimated", basis: "owner" },
      matched_fraction: "0.33",
      whole_partitions: { count: "0", names_preview: [], names_total_count: "0", truncated: false },
      boundary_partitions: [],
      storage_layers: [],
      consumers: [],
      non_cascades: [],
      semantic_facts_complete: true,
      warnings: [],
    },
  };
}

function preflight(kind: RetentionPreflightResponse["kind"]): RetentionPreflightResponse {
  return {
    preflight_id: "pf_test",
    preflight_token: "token_test",
    kind,
    operation_id: "op-test",
    preflight_attempt_id: "attempt-test",
    scope: "instance",
    request_hash: "hash-test",
    previewed_at: "2026-08-13T00:00:00Z",
    generated_at: "2026-08-13T00:00:00Z",
    expires_at: "2026-08-13T00:05:00Z",
    settings_revision: "4",
    // Protocol constant from the server, deliberately not the localized label.
    confirmation_keyword: "DELETE",
    affected_domains: [affectedDomain()],
  };
}

function retentionSettings(): RetentionSettingsResponse {
  return {
    state: "ready",
    scope: "instance",
    revision: "4",
    updated_at: "2026-08-13T00:00:00Z",
    server_now: "2026-08-13T00:00:00Z",
    policies: {
      request_logs_retention_days: 30,
      statistics_retention_days: 90,
      audit_logs_retention_days: 7,
      loadbalance_events_retention_days: 30,
    },
    recommendations: [],
    policy_generation: {
      request_logs: "1",
      audit_logs: "1",
      usage_request_events: "1",
      loadbalance_events: "1",
    },
    configured_logical_cutoffs: {
      request_logs: null,
      audit_logs: null,
      usage_request_events: null,
      loadbalance_events: null,
    },
    published_retention_floors: {
      request_logs: null,
      audit_logs: null,
      usage_request_events: null,
      loadbalance_events: null,
    },
    retention_source_revision: {},
    actual_coverage: {},
    protection: {},
  };
}

describe("Destructive retention confirmation keyword", () => {
  beforeEach(() => {
    clearUserTimezonePreference();
    // Both dialogs render timestamps through `useTimezone`, which reads the
    // costing settings once per mount.
    rewriteTestServer.use(
      http.get("/api/settings/costing", () =>
        HttpResponse.json({
          report_currency_code: "USD",
          report_currency_symbol: "$",
          timezone_preference: "UTC",
          updated_at: "2026-08-13T00:00:00Z",
        }),
      ),
    );
  });

  it("accepts only the keyword the manual cleanup preflight issued", async () => {
    rewriteTestServer.use(
      http.post("/api/maintenance/log-retention/preflights", () =>
        HttpResponse.json(preflight("manual_cleanup")),
      ),
    );

    const { result } = renderHook(() => useRetentionDeletionData({ enabled: false }));

    act(() => {
      result.current.setCleanupType("requests");
      result.current.setRetentionPreset("30");
    });
    await act(async () => {
      await result.current.handleOpenDeleteConfirm();
    });

    expect(result.current.manualPreflight?.confirmation_keyword).toBe("DELETE");

    act(() => result.current.setDeleteConfirmPhrase("删除"));
    expect(result.current.isDeletePhraseValid).toBe(false);

    act(() => result.current.setDeleteConfirmPhrase("DELETE"));
    expect(result.current.isDeletePhraseValid).toBe(true);
  });

  it("accepts only the keyword the policy-change preflight issued", async () => {
    rewriteTestServer.use(
      http.get("/api/settings/log-retention", () => HttpResponse.json(retentionSettings())),
      http.get("/api/management/jobs", () =>
        HttpResponse.json({ items: [], has_more: false, next_cursor: null, generated_at: "2026-08-13T00:00:00Z" }),
      ),
      http.post("/api/maintenance/log-retention/preflights", () =>
        HttpResponse.json(preflight("policy_change")),
      ),
    );

    const { result } = renderHook(() => useRetentionDeletionData({ enabled: true }));
    await waitFor(() => expect(result.current.retentionSettings).not.toBeNull());

    // Shortening 30 -> 7 days is the destructive transition that forces a
    // preflight plus typed confirmation.
    act(() => result.current.setRetentionDays("request_logs_retention_days", 7));
    expect(result.current.policyIsDestructive).toBe(true);

    await act(async () => {
      await result.current.handleSaveRetentionSettings();
    });
    await waitFor(() => expect(result.current.policyPreflight).not.toBeNull());

    act(() => result.current.setPolicyConfirmationPhrase("删除"));
    expect(result.current.isPolicyPhraseValid).toBe(false);

    act(() => result.current.setPolicyConfirmationPhrase("DELETE"));
    expect(result.current.isPolicyPhraseValid).toBe(true);
  });

  it("names the server keyword in the manual cleanup dialog", async () => {
    render(
      <LocaleProvider>
        <DeleteConfirmDialog
          deleteConfirm={{ type: "requests", days: 30, deleteAll: false }}
          open
          setDeleteConfirm={vi.fn()}
          deleteConfirmPhrase=""
          setDeleteConfirmPhrase={vi.fn()}
          handleBatchDelete={vi.fn()}
          deleting={false}
          isDeletePhraseValid={false}
          preflightSemanticsComplete
          preflight={preflight("manual_cleanup")}
        />
      </LocaleProvider>,
    );

    expect(await screen.findByText("输入 DELETE 以继续")).toBeTruthy();
    expect(screen.getByRole("textbox")).toHaveAttribute("placeholder", "DELETE");
  });

  it("states that a discarded preflight voids the confirmation instead of showing a dead input", () => {
    render(
      <LocaleProvider>
        <DeleteConfirmDialog
          deleteConfirm={{ type: "requests", days: 30, deleteAll: false }}
          open
          setDeleteConfirm={vi.fn()}
          deleteConfirmPhrase=""
          setDeleteConfirmPhrase={vi.fn()}
          handleBatchDelete={vi.fn()}
          deleting={false}
          isDeletePhraseValid={false}
          preflightSemanticsComplete={false}
          preflight={null}
        />
      </LocaleProvider>,
    );

    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("本次预检已作废，服务端不会再接受它的确认。请关闭本对话框后重新发起预检。")).toBeTruthy();
  });
});
