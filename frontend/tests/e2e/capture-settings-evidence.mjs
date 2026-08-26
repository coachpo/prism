// Settings 1200/1680 responsive evidence capture (Settings SPEC §12.5/§15).
// Run: node tests/e2e/capture-settings-evidence.mjs (vite dev server on 15174)
// Captures scope=instance (retention + jobs) and scope=global (billing/audit)
// at 1680x1050 and 1200x800, plus a retention preflight dialog evidence frame.
import { chromium } from "@playwright/test";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const baseURL = process.env.PLAYWRIGHT_BASE_URL || "http://127.0.0.1:15174";
// Resolve against this file, not the caller CWD, so the evidence always lands
// in the repository-local artifacts/evidence directory.
const evidenceDir = process.env.EVIDENCE_DIR
  || join(dirname(fileURLToPath(import.meta.url)), "../../../artifacts/evidence");

const timestamp = "2026-08-09T12:00:00Z";

function retentionSettings() {
  const floor = "2026-07-10T00:00:00Z";
  return {
    state: "ready",
    scope: "instance",
    revision: "4",
    updated_at: timestamp,
    server_now: timestamp,
    policies: {
      request_logs_retention_days: 30,
      statistics_retention_days: 90,
      audit_logs_retention_days: 7,
      loadbalance_events_retention_days: 30,
    },
    recommendations: [
      {
        id: "balanced-v1",
        policies: {
          request_logs_retention_days: 30,
          statistics_retention_days: 90,
          audit_logs_retention_days: 7,
          loadbalance_events_retention_days: 30,
        },
        rationale_codes: ["balanced-v1"],
      },
    ],
    policy_generation: {
      request_logs: "7",
      audit_logs: "5",
      usage_request_events: "9",
      loadbalance_events: "6",
    },
    configured_logical_cutoffs: {
      request_logs: "2026-07-10T00:00:00Z",
      audit_logs: "2026-08-02T00:00:00Z",
      usage_request_events: "2026-05-11T00:00:00Z",
      loadbalance_events: "2026-07-10T00:00:00Z",
    },
    published_retention_floors: {
      request_logs: "2026-07-09T00:00:00Z",
      audit_logs: null,
      usage_request_events: "2026-05-10T00:00:00Z",
      loadbalance_events: "2026-07-09T00:00:00Z",
    },
    retention_source_revision: {
      request_logs: "src-rev-1",
      audit_logs: "src-rev-2",
      usage_request_events: "src-rev-3",
      loadbalance_events: "src-rev-4",
    },
    actual_coverage: {
      request_logs: {
        from_time: "2026-07-09T00:00:00Z",
        to_time: timestamp,
        source: "retention_source",
        precision: "owner_projection",
        gaps: [],
        complete: true,
        freshness: "fresh",
        source_revision: "src-rev-1",
        retention_epoch: "2",
        retention_generation: "7",
        purge_state: "idle",
      },
      audit_logs: {
        from_time: null,
        to_time: timestamp,
        source: "retention_source",
        precision: "owner_projection",
        gaps: [],
        complete: true,
        freshness: "fresh",
        source_revision: "src-rev-2",
        retention_epoch: "0",
        retention_generation: "5",
        purge_state: "idle",
      },
      usage_request_events: {
        from_time: "2026-05-10T00:00:00Z",
        to_time: timestamp,
        source: "retention_source",
        precision: "owner_projection",
        gaps: [],
        complete: true,
        freshness: "fresh",
        source_revision: "src-rev-3",
        retention_epoch: "1",
        retention_generation: "9",
        purge_state: "idle",
      },
      loadbalance_events: {
        from_time: "2026-07-09T00:00:00Z",
        to_time: timestamp,
        source: "retention_source",
        precision: "owner_projection",
        gaps: [],
        complete: true,
        freshness: "fresh",
        source_revision: "src-rev-4",
        retention_epoch: "1",
        retention_generation: "6",
        purge_state: "idle",
      },
    },
    protection: {
      request_logs: {
        kind: "observe_query_token",
        token_ttl_seconds: 86400,
        extra_grace_seconds: 86400,
        physical_reclaim_not_before: "2026-08-11T12:00:00Z",
        source_revision: "src-rev-1",
        retention_epoch: "2",
        retention_generation: "7",
        purge_state: "idle",
      },
      audit_logs: {
        kind: "audit_retention_fence",
        contract_version: 3,
        retention_source: { domain: "audit_logs", purge_state: "idle", contract_version: 1 },
        audit_protection: { contract_version: 1, reader_fence_state: "clear", materializer_state: "ready" },
        fixed_token_ttl_seconds: null,
        fixed_extra_grace_seconds: null,
        physical_reclaim_not_before: null,
      },
    },
    owner_drift_inventory: {
      inventory_generation: "4",
      state: "resolved",
      current_heads: [],
      generated_at: timestamp,
    },
  };
}

function retentionJobs() {
  return {
    items: [
      {
        id: "job-2001",
        contract_version: 2,
        type: "log_retention",
        job_scope: "instance",
        origin: "automatic",
        legacy_origin_provenance: null,
        legacy_execution_provenance: null,
        dataset: "usage_request_events",
        state: "running",
        terminal_disposition: null,
        legacy_original_state: null,
        mode: "cutoff",
        cutoff: "2026-05-11T00:00:00Z",
        purge_to_time: null,
        policy_revision: "4",
        preflight_id: null,
        operation_id: "op-automatic-2001",
        requested_at: "2026-08-09T11:00:00Z",
        started_at: "2026-08-09T11:00:02Z",
        finished_at: null,
        last_heartbeat_at: "2026-08-09T11:59:58Z",
        attempt_count: 1,
        cancel_allowed: false,
        progress: {
          accounting_provenance: "v2_exact",
          stage: "deleting_boundary_rows",
          visibility_state: "scheduled_cutoff_active",
          purge_state: "idle",
          protection: null,
          rows_matched_estimate: null,
          rows_matched_accuracy: "unavailable",
          boundary_rows_deleted: "184",
          boundary_batches_completed: "2",
          dropped_partition_count: "14",
          dropped_partition_count_accuracy: "exact",
          dropped_partition_names_preview: ["prism_usage_request_events_p20260401", "prism_usage_request_events_p20260402"],
          dropped_partition_names_total_count: "14",
          dropped_partition_names_truncated: true,
          dropped_rows_estimate: "1048576",
          dropped_rows_accuracy: "estimated",
          staged_items_tombstoned: null,
          sensitive_artifact_bytes_deleted: null,
          last_checkpoint_at: "2026-08-09T11:59:58Z",
        },
        error: null,
      },
      {
        id: "job-2002",
        contract_version: 2,
        type: "log_retention",
        job_scope: "instance",
        origin: "manual",
        legacy_origin_provenance: null,
        legacy_execution_provenance: null,
        dataset: "request_logs",
        state: "succeeded",
        terminal_disposition: "completed",
        legacy_original_state: null,
        mode: "delete_all",
        cutoff: null,
        purge_to_time: "2026-08-08T09:14:00Z",
        policy_revision: "4",
        preflight_id: "pf_manual_2002",
        operation_id: "op-manual-2002",
        requested_at: "2026-08-08T09:00:00Z",
        started_at: "2026-08-08T09:14:00Z",
        finished_at: "2026-08-08T09:14:31Z",
        last_heartbeat_at: "2026-08-08T09:14:31Z",
        attempt_count: 1,
        cancel_allowed: false,
        progress: {
          accounting_provenance: "v2_exact",
          stage: "finished",
          visibility_state: "revoked",
          purge_state: "published",
          protection: null,
          rows_matched_estimate: "524288",
          rows_matched_accuracy: "estimated",
          boundary_rows_deleted: "97",
          boundary_batches_completed: "1",
          dropped_partition_count: "9",
          dropped_partition_count_accuracy: "exact",
          dropped_partition_names_preview: ["prism_request_logs_p20260401"],
          dropped_partition_names_total_count: "9",
          dropped_partition_names_truncated: true,
          dropped_rows_estimate: "2097152",
          dropped_rows_accuracy: "estimated",
          staged_items_tombstoned: "0",
          sensitive_artifact_bytes_deleted: null,
          last_checkpoint_at: "2026-08-08T09:14:31Z",
        },
        error: null,
      },
    ],
    has_more: false,
    next_cursor: null,
    generated_at: timestamp,
  };
}

function authSettings() {
  return {
    revision: "3",
    auth_mode: {
      desired: "enabled",
      effective: "enabled",
      access_state: "enabled",
      desired_generation: "3",
      effective_generation: "3",
    },
    operator_account: {
      effective: {
        username: "operator",
        email: null,
        email_bound_at: null,
        password_version: 1,
        updated_at: timestamp,
      },
      desired: null,
    },
    transition: null,
    proxy_key_readiness: {
      readiness_generation: "12",
      fingerprint: "fp-v1-abc",
      counted_at: timestamp,
      counts: {
        active: 3,
        safe_active: 3,
        expiring_within_30s: 0,
        expired: 0,
        disabled: 1,
        total: 4,
      },
      activation_guard: {
        safe_active: "3",
        frontier_pending: false,
        frontier_at: null,
      },
      state: "ready",
    },
    attribution_mode_when_disabled: "permissive",
    updated_at: timestamp,
  };
}

function auditSettings() {
  return {
    revision: "2",
    updated_at: timestamp,
    policies: [
      { family: "openai", mode: "body_capture" },
      { family: "anthropic", mode: "metadata_only" },
      { family: "gemini", mode: "disabled" },
    ],
    fixed_capture_limits: {
      per_request_body_bytes: 4194304,
      aggregate_request_body_bytes: 12582912,
      final_response_body_bytes: 4194304,
      aggregate_raw_body_bytes_per_ingress: 16777216,
    },
  };
}

function retentionPreflight() {
  return {
    preflight_id: "pf_manual_evidence",
    preflight_token: "token_manual_evidence",
    kind: "manual_cleanup",
    operation_id: "op-manual-evidence",
    preflight_attempt_id: "attempt-manual-evidence",
    scope: "instance",
    request_hash: "hash-manual-evidence",
    previewed_at: timestamp,
    generated_at: timestamp,
    expires_at: "2026-08-09T12:05:00Z",
    settings_revision: "4",
    confirmation_keyword: "DELETE",
    affected_domains: [
      {
        dataset: "request_logs",
        impact: {
          matched_rows: { value: "524288", accuracy: "estimated" },
          retained_rows: { value: "123456", accuracy: "exact" },
          matched_logical_bytes: { value: "987654321", accuracy: "estimated" },
          reclaimable_physical_bytes: { value: "876543210", accuracy: "estimated" },
          matched_fraction: "0.04",
          whole_partitions: { count: "9", names_preview: ["prism_request_logs_p20260401"], names_total_count: "9", truncated: true },
          boundary_partitions: [{ name: "prism_request_logs_p20260408", boundary_rows: "97" }],
          storage_layers: [{ layer: "partitioned_log", state: "bounded" }],
          consumers: ["Requests", "Audit"],
          non_cascades: [],
          semantic_facts_complete: true,
          warnings: [],
          resolved_cutoff: "2026-08-02T00:00:00Z",
          logical_coverage_after: { from_time: "2026-08-02T00:00:00Z", to_time: timestamp },
          physical_reclaim_not_before: "2026-08-10T12:00:00Z",
        },
      },
    ],
  };
}

function auditStorageSummary() {
  return {
    source_revision: "src-rev-2",
    storage_fact_evidence: { state: "bound", generation: "fact-gen-9" },
    generated_at: timestamp,
    retention_source: { domain: "audit_logs", purge_state: "idle", contract_version: 1, source_revision: "src-rev-2" },
    audit_protection: { contract_version: 1, reader_fence_state: "clear", materializer_state: "ready" },
    retained_rows: "12345678",
    logical_header_bytes: "987654321",
    logical_body_bytes: "2468013579",
    last_7d_logical_bytes_added: "123456789",
    sampled_days: 7,
    daily_average_logical_bytes: "17636684",
    precision: "exact",
    freshness: "fresh",
  };
}

function costingSettings() {
  return {
    profile_id: 1,
    report_currency_code: "USD",
    report_currency_symbol: "$",
    reporting_currency_epoch: 1,
    currency_effective_at: "2026-01-01T00:00:00Z",
    pricing_migration_state: "active",
    legacy_migration_issues: [],
    timezone_preference: "Asia/Shanghai",
    pricing_template_generation: 3,
    pricing_reference_generation: 4,
    updated_at: timestamp,
  };
}

async function installSettingsRoutes(page) {
  // Specific handlers only: a catch-all 200 `{}` fallback silently satisfies
  // unmocked bootstrap/session calls and breaks the shell, so it is omitted.
  await page.route("**/api/auth/public-bootstrap", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: true, auth_enabled: true, username: "operator" }) }),
  );
  await page.route("**/api/auth/status", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ state: "enabled", transition_state: null, login_available: true, effective_generation: "3", retry_after_seconds: null }) }),
  );
  await page.route("**/api/auth/session", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ authenticated: true, auth_enabled: true, username: "operator", subject_key: "auth:subject:1" }) }),
  );
  await page.route("**/api/models*", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname !== "/api/models") return route.continue();
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([]) });
  });
  await page.route("**/api/settings/auth", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(authSettings()) }),
  );
  await page.route("**/api/settings/costing", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(costingSettings()) }),
  );
  await page.route("**/api/settings/log-retention", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(retentionSettings()) }),
  );
  await page.route("**/api/settings/audit/storage-summary", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(auditStorageSummary()) }),
  );
  await page.route("**/api/settings/audit", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(auditSettings()) }),
  );
  await page.route("**/api/maintenance/log-retention/preflights", (route) =>
    route.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify(retentionPreflight()) }),
  );
  await page.route("**/api/config/header-blocklist-rules*", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([
      { id: 1, name: "Authorization", match_type: "exact", pattern: "Authorization", enabled: true, is_system: true, created_at: timestamp, updated_at: timestamp },
      { id: 2, name: "X-Api-Key", match_type: "exact", pattern: "X-Api-Key", enabled: true, is_system: true, created_at: timestamp, updated_at: timestamp },
      { id: 3, name: "Cookie", match_type: "exact", pattern: "Cookie", enabled: true, is_system: true, created_at: timestamp, updated_at: timestamp },
    ]) }),
  );
  await page.route("**/api/config/user-agent-client-rules*", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([
      { id: 1, name: "OpenAI SDK", pattern: "openai-python", enabled: true, is_system: false, created_at: timestamp, updated_at: timestamp },
    ]) }),
  );
  await page.route("**/api/management/jobs*", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(retentionJobs()) }),
  );
  await page.route("**/api/stats/proxy-api-key-filter-options*", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [], selected: null, next_cursor: null }) }),
  );
}

async function run() {
  const launchOptions = process.env.PLAYWRIGHT_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH }
    : { channel: process.env.PLAYWRIGHT_CHANNEL || undefined };
  const browser = await chromium.launch(launchOptions);
  const viewports = [
    { name: "1680", width: 1680, height: 1050 },
    { name: "1200", width: 1200, height: 800 },
  ];
  for (const viewport of viewports) {
    const page = await browser.newPage({ viewport: { width: viewport.width, height: viewport.height } });
    await installSettingsRoutes(page);

    // Instance scope: authentication + retention policy + actual coverage + jobs.
    await page.goto(`${baseURL}/system/settings?scope=instance&section=retention#retention`, { waitUntil: "domcontentloaded" });
    await page.getByText("自动保留策略", { exact: false }).first().waitFor({ timeout: 20000 });
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${evidenceDir}/ux-settings-retention-${viewport.name}.png`, fullPage: false });

    if (viewport.name === "1200") {
      const manualCleanup = page.locator("#manual-cleanup");
      await manualCleanup.getByRole("combobox").nth(0).click();
      await page.getByRole("option", { name: "请求日志", exact: true }).click();
      await manualCleanup.getByRole("combobox").nth(1).click();
      await page.getByRole("option", { name: "7 天", exact: true }).click();
      await manualCleanup.getByRole("button", { name: "删除数据", exact: true }).click();
      await page.getByRole("dialog").waitFor({ state: "visible", timeout: 20000 });
      await page.waitForTimeout(800);
      await page.screenshot({ path: `${evidenceDir}/ux-settings-retention-preflight-${viewport.name}.png`, fullPage: false });
    }

    await page.goto(`${baseURL}/system/settings?scope=instance&section=retention-jobs#retention-jobs`, { waitUntil: "domcontentloaded" });
    await page.getByText("清理作业", { exact: true }).first().waitFor({ timeout: 20000 });
    await page.locator("#retention-jobs").scrollIntoViewIfNeeded();
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${evidenceDir}/ux-settings-jobs-${viewport.name}.png`, fullPage: false });

    // Global scope: billing/timezone + audit & privacy.
    await page.goto(`${baseURL}/system/settings?scope=global&section=billing-currency#billing-currency`, { waitUntil: "domcontentloaded" });
    await page.getByText("报告货币", { exact: false }).first().waitFor({ timeout: 20000 });
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${evidenceDir}/ux-settings-billing-${viewport.name}.png`, fullPage: false });

    await page.goto(`${baseURL}/system/settings?scope=global&section=audit-privacy#audit-privacy`, { waitUntil: "domcontentloaded" });
    await page.getByText("审计与隐私", { exact: false }).first().waitFor({ timeout: 20000 });
    await page.waitForTimeout(800);
    await page.screenshot({ path: `${evidenceDir}/ux-settings-audit-${viewport.name}.png`, fullPage: false });

    await page.close();
  }

  // Narrow a11y capture for the settings retention surface (keyboard focus evidence).
  const narrow = await browser.newPage({ viewport: { width: 390, height: 844 } });
  await installSettingsRoutes(narrow);
  await narrow.goto(`${baseURL}/system/settings?scope=instance&section=retention#retention`, { waitUntil: "domcontentloaded" });
  await narrow.getByText("自动保留策略", { exact: false }).first().waitFor({ timeout: 20000 });
  await narrow.waitForTimeout(800);
  await narrow.keyboard.press("Tab");
  await narrow.screenshot({ path: `${evidenceDir}/ux-settings-retention-390.png`, fullPage: false });
  await narrow.close();

  await browser.close();
  console.log("settings evidence captured under", evidenceDir);
}

await run();
