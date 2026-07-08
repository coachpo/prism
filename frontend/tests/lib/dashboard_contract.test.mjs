import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const typeSource = readFileSync(
  path.join(frontendDir, "src/lib/types/model-stats.ts"),
  "utf8",
);
const pageDataSource = readFileSync(
  path.join(frontendDir, "src/pages/dashboard/useDashboardPageData.ts"),
  "utf8",
);
const recentActivityCardSource = readFileSync(
  path.join(frontendDir, "src/pages/dashboard/RecentActivityCard.tsx"),
  "utf8",
);
const metricsGridSource = readFileSync(
  path.join(frontendDir, "src/pages/dashboard/DashboardMetricsGrid.tsx"),
  "utf8",
);

function extractInterface(name) {
  const match = typeSource.match(new RegExp(`export interface ${name} \\{([\\s\\S]*?)\\n\\}`));
  assert.ok(match, `expected ${name} interface to exist`);
  return match[1];
}

test("DashboardSnapshot is stats-only with revision and usage-event watermark", () => {
  const dashboardSnapshot = extractInterface("DashboardSnapshot");
  const removedSnapshotField = new RegExp(["recent", "requests"].join("_"));
  assert.match(dashboardSnapshot, /snapshot_revision: string;/);
  assert.match(dashboardSnapshot, /source_watermark: DashboardSnapshotSourceWatermark;/);
  assert.doesNotMatch(dashboardSnapshot, removedSnapshotField);
  assert.doesNotMatch(dashboardSnapshot, /latest_request_log_id/);
  assert.doesNotMatch(dashboardSnapshot, /request_log/);

  const sourceWatermark = extractInterface("DashboardSnapshotSourceWatermark");
  assert.match(sourceWatermark, /latest_usage_event_created_at: string \| null;/);
  assert.match(sourceWatermark, /latest_usage_event_id: number \| null;/);
});

test("recent activity REST contract uses request-history watermark and items array", () => {
  const activityResponse = extractInterface("DashboardRecentActivityResponse");
  assert.match(activityResponse, /generated_at: string;/);
  assert.match(activityResponse, /activity_watermark: DashboardRecentActivityWatermark;/);
  assert.match(activityResponse, /items: DashboardRecentActivityItem\[\];/);

  const activityWatermark = extractInterface("DashboardRecentActivityWatermark");
  assert.match(activityWatermark, /latest_request_log_created_at: string \| null;/);
  assert.match(activityWatermark, /latest_request_log_id: number \| null;/);

  const activityItem = extractInterface("DashboardRecentActivityItem");
  for (const field of [
    "request_log_id",
    "created_at",
    "model_id",
    "model_label",
    "endpoint_label",
    "status_code",
    "response_time_ms",
    "is_stream",
    "stream_outcome",
  ]) {
    assert.match(activityItem, new RegExp(`${field}:`));
  }
});

test("dashboard overview UI consumes separate recent activity items", () => {
  assert.match(pageDataSource, /recentActivityItems: DashboardRecentActivityItem\[\];/);
  assert.doesNotMatch(pageDataSource, /RequestLogListItem/);
  assert.doesNotMatch(pageDataSource, /recentRequests/);

  assert.match(recentActivityCardSource, /DashboardRecentActivityItem/);
  assert.match(recentActivityCardSource, /activity\.request_log_id/);
  assert.doesNotMatch(recentActivityCardSource, /RequestLogListItem/);
  assert.doesNotMatch(recentActivityCardSource, /recentRequests/);
  assert.doesNotMatch(recentActivityCardSource, /request\.id/);
});

test("dashboard contracts stay REST-only after websocket retirement", () => {
  assert.doesNotMatch(typeSource, /DashboardRealtime/);
  assert.doesNotMatch(typeSource, /dashboard\.update/);
  assert.doesNotMatch(typeSource, /dashboard\.snapshot/);
  assert.doesNotMatch(typeSource, /dashboard\.activity/);
});

test("dashboard unpriced spend detail links to filtered request logs", () => {
  assert.match(metricsGridSource, /to="\/observe\/requests"/);
  assert.match(metricsGridSource, /priced: "false"/);
  assert.match(metricsGridSource, /time_range: "30d"/);
});
