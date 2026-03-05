# Dashboard Routing Diagram Upgrade Plan (Topology + Traffic Sankey)

## Summary
- Add a new full-width `Routing Diagram` section to [DashboardPage.tsx](/Users/liqing/Documents/PersonalProjects/My_Proj/prism/frontend/src/pages/DashboardPage.tsx) between KPI blocks and recent activity.
- Implement a Recharts Sankey with two modes: `Topology` and `Traffic (24h)`.
- Make nodes and links clickable for drilldown.

## Implementation Changes
1. Data model and fetch pipeline
- Keep existing dashboard KPI fetches; add a dedicated routing-diagram data loader.
- Fetch `models.list()` and then `connections.list(modelId)` in parallel for all models to build runtime topology.
- Build a normalized graph map keyed by `model_id + endpoint_id`, including `model_config_id`, `endpoint_id`, endpoint label, and active connection count.
- Fetch traffic weights from `stats.spending` with `group_by=model_endpoint`, `preset=custom`, and `from_time=now-24h` to `to_time=now`.
- Parse spending keys (`model#endpoint_id`) and merge into topology edges as `traffic_request_count_24h`.

2. Diagram rendering and UX
- Add mode toggle in the new card: `Topology` (default) and `Traffic (24h)`.
- Render Sankey links from endpoint nodes to model nodes.
- Topology mode link value: `active_connection_count`.
- Traffic mode link value: `traffic_request_count_24h` (only links with count > 0).
- Add clear empty states for: no active topology links, no traffic in last 24h.
- Ensure responsive behavior: reduced height and readable labels on mobile.

3. Drilldown behavior (clickable)
- Model node click: navigate to `/models/{model_config_id}`.
- Endpoint node click: navigate to `/request-logs?endpoint_id={endpoint_id}`.
- Link click: navigate to `/request-logs?model_id={model_id}&endpoint_id={endpoint_id}`.
- Tooltip content:
- Endpoint name
- Model name/id
- Active connection count
- 24h request count (when in traffic mode)

4. Interfaces and API/type impact
- No backend API changes.
- No public API contract changes required.
- Add internal frontend types for routing graph view models (node/link/mode), ideally in a small dashboard-local helper module to keep `DashboardPage` lean.

## Test Plan
1. Verification commands
- Run `pnpm run lint` and `pnpm run build` in `frontend/`.

2. Manual scenarios
- Topology mode shows expected endpoint-model relationships for current selected profile.
- Traffic mode shows only edges with 24h traffic and correct relative thickness.
- Clicking model/endpoint/link routes to correct page/query filters.
- Empty states render correctly when no models, no active connections, or no 24h traffic.
- Switching selected profile refreshes graph correctly and does not leak prior-profile data.

3. Data correctness checks
- Confirm weighted edges align with `stats.spending(...group_by=model_endpoint...)` totals.
- Confirm topology edges align with active connections from `connections.list(modelId)`.

## Assumptions and Defaults
- `Topology` is default mode.
- Traffic window is fixed to last 24 hours.
- Diagram focuses on active runtime routing (active connections only).
- Proxy models without direct connections are not rendered as standalone topology targets in v1.
- If dataset is large, links are sorted by mode weight and rendered as-is in v1 (no additional clustering/virtualization).
