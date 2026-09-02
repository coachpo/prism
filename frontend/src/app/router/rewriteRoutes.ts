import { z } from "zod";
import { modelDetailSearchSchema } from "@/features/models/detail/modelDetailSchemas";
import { OBSERVE_METRICS } from "@/features/observe/observeSearch";
import { isPositiveDecimalInt64 } from "@/pages/request-logs/queryParams";

export type PrismRouteScope =
  | "public"
  | "protected-global"
  | "protected-selected-profile"
  | "mixed";

export type PrismRouteId =
  | "observe"
  | "observe-routing-health"
  | "auth-login"
  | "models"
  | "model-export"
  | "model-detail"
  | "route-endpoints"
  | "route-ban-policies"
  | "system-settings"
  | "system-proxy-keys"
  | "route-pricing"
  | "observe-requests"
  | "observe-request-audit";

const searchStringSchema = z.preprocess(
  (value) => (value == null ? "" : String(value)),
  z.string(),
);
const requestIdSearchSchema = z
  .preprocess(
    (value) => (value == null ? "" : String(value)),
    z.string().refine(isPositiveDecimalInt64),
  )
  .catch("");
const optionalSearchStringSchema = z.preprocess(
  (value) => (value == null ? undefined : String(value)),
  z.string().optional(),
);
const optionalRequestIdSearchSchema = z
  .preprocess(
    (value) => (value == null ? undefined : String(value)),
    z
      .string()
      .refine(isPositiveDecimalInt64)
      .optional(),
  )
  .catch(undefined);

export const observeSearchSchema = z.object({
  // The dashboard is one view with a content switcher. `overview` and
  // `analytics` are the pre-redesign names and still parse, so old deep links
  // resolve; `events` is caught by the router and redirected to
  // /observe/routing-health.
  tab: z
    .enum([
      "overview",
      "analytics",
      "events",
      "trend",
      "errors",
      "activity",
      "terminal_targets",
    ])
    .catch("overview"),
  // The metric tuple stays a single key so one URL always names exactly one
  // main-chart view; output_rate and cache_read_share are first-class members,
  // not modifiers of the five original metrics.
  metric: z.enum(OBSERVE_METRICS).catch("requests"),
  scope: z
    .enum(["ingress", "final_execution", "route_attempt"])
    .catch("ingress"),
  group_by: z
    .enum([
      "none",
      "ingress_model",
      "final_target_model",
      "attempt_target_model",
      "attempt_trigger",
      "attempt_result",
      "api_family",
      "endpoint",
      "terminal_target",
    ])
    .catch("none"),
  interval: z
    .enum(["auto", "5m", "15m", "1h", "6h", "1d", "1w", "1mo", "1y"])
    .catch("auto"),
  cost_segment_key: searchStringSchema.catch(""),
  // Shared event window (Events timeline only; never sent to Current State).
  // Absent values resolve to the 24h default at the data layer; the URL stays
  // clean so unrelated tabs do not gain noisy query params.
  preset: z
    .enum(["1h", "6h", "24h", "7d", "30d", "all", "custom"])
    .optional()
    .catch(undefined),
  from_time: optionalSearchStringSchema.catch(undefined),
  to_time: optionalSearchStringSchema.catch(undefined),
  // Events timeline namespace (SPEC §8.1): filters, direction, selection.
  event_type: z
    .preprocess(
      (value) =>
        value == null ? undefined : Array.isArray(value) ? value : [value],
      z.array(z.string()).optional(),
    )
    .catch(undefined),
  event_failure_kind: z
    .preprocess(
      (value) =>
        value == null ? undefined : Array.isArray(value) ? value : [value],
      z.array(z.string()).optional(),
    )
    .catch(undefined),
  event_admission_reason: z
    .preprocess(
      (value) =>
        value == null ? undefined : Array.isArray(value) ? value : [value],
      z.array(z.string()).optional(),
    )
    .catch(undefined),
  event_model_id: optionalSearchStringSchema.catch(undefined),
  event_endpoint_id: optionalSearchStringSchema.catch(undefined),
  event_terminal_target_id: optionalSearchStringSchema.catch(undefined),
  event_sort_order: z.enum(["desc", "asc"]).optional(),
  event_id: optionalRequestIdSearchSchema.catch(undefined),
  event_cursor: optionalSearchStringSchema.catch(undefined),
  // Current State namespace (tokenless; never receives the event window).
  runtime_state: z
    .preprocess(
      (value) =>
        value == null ? undefined : Array.isArray(value) ? value : [value],
      z.array(z.string()).optional(),
    )
    .catch(undefined),
  runtime_model_id: optionalSearchStringSchema.catch(undefined),
  runtime_endpoint_id: optionalSearchStringSchema.catch(undefined),
  runtime_terminal_target_id: optionalSearchStringSchema.catch(undefined),
  runtime_cursor: optionalSearchStringSchema.catch(undefined),
});

/**
 * Routing health owns the event timeline and current-state namespaces after
 * they moved off the dashboard's third tab. The parameter keys are unchanged
 * so every `/observe?tab=events&…` deep link still maps one to one.
 */
export const routingHealthSearchSchema = observeSearchSchema.pick({
  preset: true,
  from_time: true,
  to_time: true,
  event_type: true,
  event_failure_kind: true,
  event_admission_reason: true,
  event_model_id: true,
  event_endpoint_id: true,
  event_terminal_target_id: true,
  event_sort_order: true,
  event_id: true,
  event_cursor: true,
  runtime_state: true,
  runtime_model_id: true,
  runtime_endpoint_id: true,
  runtime_terminal_target_id: true,
  runtime_cursor: true,
});

export const authLoginSearchSchema = z.object({
  redirect: optionalSearchStringSchema.catch(undefined),
});

export const requestLogSearchSchema = z.object({
  client_rule_id: searchStringSchema.catch(""),
  proxy_api_key_id: searchStringSchema.catch(""),
  cursor: z.coerce.number().int().min(0).catch(0),
  endpoint: searchStringSchema.catch(""),
  endpoint_id: searchStringSchema.catch(""),
  ingress_request_id: searchStringSchema.catch(""),
  limit: z.coerce
    .number()
    .refine((value) => [100, 300, 500].includes(value))
    .catch(100),
  ingress_model_id: searchStringSchema.catch(""),
  offset: z.coerce.number().int().min(0).catch(0),
  request_id: requestIdSearchSchema,
  attempt_target_model_id: searchStringSchema.catch(""),
  api_family: searchStringSchema.catch(""),
  row_kind: searchStringSchema.catch(""),
  selected_request_id: requestIdSearchSchema,
  status: z.enum(["all", "success", "client_error", "error"]).catch("all"),
  status_code: searchStringSchema.catch(""),
  stream_outcome: searchStringSchema.catch(""),
  stream_error_kind: searchStringSchema.catch(""),
  error_text: searchStringSchema.catch(""),
  from_time: optionalSearchStringSchema.catch(undefined),
  to_time: optionalSearchStringSchema.catch(undefined),
  observe_return: optionalSearchStringSchema.catch(undefined),
  terminal_target_id: searchStringSchema.catch(""),
  pricing_status: z
    .enum(["all", "priced", "unpriced", "ineligible", "unknown"])
    .catch("all"),
  unpriced_reason: searchStringSchema.catch(""),
  pricing_card_role: searchStringSchema.catch(""),
  pricing_selection_state: searchStringSchema.catch(""),
  status_family: z.enum(["all", "2xx", "4xx", "5xx"]).catch("all"),
  time_range: z.enum(["1h", "6h", "24h", "7d", "30d", "all"]).catch("24h"),
  // Observe finalized deep-link parameters (§4.3): query_context is required
  // whenever any final_* selector is present; the backend verifies the token.
  query_context: searchStringSchema.catch(""),
  final_result: searchStringSchema.catch(""),
  ingress_final_result: searchStringSchema.catch(""),
  confirmed_failover: searchStringSchema.catch(""),
  outcome_detail: searchStringSchema.catch(""),
  final_status_code: searchStringSchema.catch(""),
  final_stream_outcome: searchStringSchema.catch(""),
  final_stream_error_kind: searchStringSchema.catch(""),
  final_exclude: searchStringSchema.catch(""),
  final_target_model_id: searchStringSchema.catch(""),
  final_endpoint_id: searchStringSchema.catch(""),
  final_terminal_target_id: searchStringSchema.catch(""),
  final_pricing_status: searchStringSchema.catch(""),
  final_unpriced_reason: searchStringSchema.catch(""),
  reporting_currency_epoch: searchStringSchema.catch(""),
  cost_segment_key: searchStringSchema.catch(""),
  attempt_trigger: searchStringSchema.catch(""),
  attempt_result: searchStringSchema.catch(""),
  view: z.enum(["attempts", "ingress_chains"]).catch("ingress_chains"),
  sort_by: z
    .enum([
      "created_at",
      "display_status",
      "ttft_ms",
      "total_tokens",
      "total_cost_user_currency_micros",
    ])
    .optional()
    .catch(undefined),
  sort_order: z.enum(["asc", "desc"]).optional().catch(undefined),
  chain_limit: z.coerce
    .number()
    .int()
    .min(1)
    .max(50)
    .optional()
    .catch(undefined),
  chain_row_limit: z.coerce
    .number()
    .int()
    .min(1)
    .max(200)
    .optional()
    .catch(undefined),
  chain_cursor: optionalSearchStringSchema.catch(undefined),
  row_cursor: optionalSearchStringSchema.catch(undefined),
});

export const requestAuditSearchSchema = z.object({
  audit_id: optionalRequestIdSearchSchema,
  cursor: optionalSearchStringSchema.catch(undefined),
});

// Settings canonical search (SPEC §12.2): public scope is exactly
// global|instance; the legacy `tab` parameter is invalid and dropped during
// canonicalization (old tab=global meant instance, so it is never mapped).
// Billing-currency section-owned Pricing keys are allowlisted only under an
// explicit section=billing-currency.
export const settingsSearchSchema = z.object({
  scope: z.enum(["global", "instance"]).optional(),
  section: z
    .enum([
      "billing-currency",
      "timezone",
      "audit-privacy",
      "header-blocklist",
      "client-rules",
      "authentication",
      "retention",
      "manual-cleanup",
      "retention-jobs",
    ])
    .optional(),
  costing_action: z
    .enum(["currency_cutover", "repair_same_currency", "archive_unused_fx"])
    .optional(),
  pricing_inventory_id: z.string().optional(),
});

// Models list canonical search: filters, sort and pagination are URL state so
// a filtered view is a shareable link. `/route/models` previously accepted no
// parameters, so every key here is additive.
export const modelsListSearchSchema = z.object({
  scope: z
    .enum(["ingress", "final_execution", "route_attempt"])
    .optional(),
  search: z.string().optional(),
  api_family: z.enum(["all", "openai", "anthropic", "gemini"]).optional(),
  status: z.enum(["all", "enabled", "disabled"]).optional(),
  // Identity filters: upstream_decoupled matches entry models whose direct
  // Terminal Targets hold a persisted upstream identity differing from the
  // entry model_id (case-sensitive); has_model_target matches entries with at
  // least one Model Target row. `all` never persists to the URL.
  flag: z
    .enum([
      "all",
      "needs_target",
      "single_truncated",
      "upstream_decoupled",
      "has_model_target",
    ])
    .optional(),
  sort_by: z
    .enum([
      "name",
      "api_family",
      "status",
      "targets",
      "strategy",
      "success",
      "p95",
      "requests",
      "spend",
    ])
    .optional(),
  sort_order: z.enum(["asc", "desc"]).optional(),
  page: z.coerce.number().int().min(1).optional(),
  page_size: z.coerce.number().int().min(1).max(200).optional(),
});

export const emptySearchSchema = z.object({});

export type ObserveSearch = z.input<typeof observeSearchSchema>;
export type AuthLoginSearch = z.input<typeof authLoginSearchSchema>;
export type RequestLogSearch = z.input<typeof requestLogSearchSchema>;
export type RequestAuditSearch = z.input<typeof requestAuditSearchSchema>;
export type RoutingHealthSearch = z.input<typeof routingHealthSearchSchema>;
export type SettingsSearch = z.input<typeof settingsSearchSchema>;
export type ModelsListSearch = z.input<typeof modelsListSearchSchema>;
export type ModelDetailRouteSearch = z.input<typeof modelDetailSearchSchema>;
interface StaticRouteDefinition {
  readonly id: Exclude<PrismRouteId, "model-detail" | "observe-request-audit">;
  readonly path: string;
  readonly scope: PrismRouteScope;
  readonly searchSchema:
    | typeof authLoginSearchSchema
    | typeof emptySearchSchema
    | typeof modelsListSearchSchema
    | typeof observeSearchSchema
    | typeof requestLogSearchSchema
    | typeof routingHealthSearchSchema
    | typeof settingsSearchSchema;
}

export const prismRouteDefinitions = [
  {
    id: "observe",
    path: "/observe",
    scope: "mixed",
    searchSchema: observeSearchSchema,
  },
  {
    id: "observe-routing-health",
    path: "/observe/routing-health",
    scope: "protected-selected-profile",
    searchSchema: routingHealthSearchSchema,
  },
  {
    id: "auth-login",
    path: "/auth/login",
    scope: "public",
    searchSchema: authLoginSearchSchema,
  },
  {
    id: "models",
    path: "/route/models",
    scope: "protected-selected-profile",
    searchSchema: modelsListSearchSchema,
  },
  {
    id: "model-export",
    path: "/route/models/export",
    scope: "protected-selected-profile",
    searchSchema: emptySearchSchema,
  },
  {
    id: "route-endpoints",
    path: "/route/endpoints",
    scope: "protected-selected-profile",
    searchSchema: emptySearchSchema,
  },
  {
    id: "route-ban-policies",
    path: "/route/ban-policies",
    scope: "protected-selected-profile",
    searchSchema: emptySearchSchema,
  },
  {
    id: "system-settings",
    path: "/system/settings",
    scope: "mixed",
    searchSchema: settingsSearchSchema,
  },
  {
    id: "system-proxy-keys",
    path: "/system/proxy-keys",
    scope: "protected-global",
    searchSchema: emptySearchSchema,
  },
  {
    id: "route-pricing",
    path: "/route/pricing",
    scope: "protected-selected-profile",
    searchSchema: emptySearchSchema,
  },
  {
    id: "observe-requests",
    path: "/observe/requests",
    scope: "protected-selected-profile",
    searchSchema: requestLogSearchSchema,
  },
] as const satisfies readonly StaticRouteDefinition[];

export const prismDynamicRouteDefinitions = [
  {
    id: "model-detail",
    path: "/route/models/$modelId",
    scope: "protected-selected-profile",
  },
  {
    id: "observe-request-audit",
    path: "/observe/requests/$requestId/audit",
    scope: "protected-selected-profile",
  },
] as const;

export const rewriteRoutePaths = [
  ...prismRouteDefinitions.map((route) => route.path),
  ...prismDynamicRouteDefinitions.map((route) => route.path),
] as const;

export type RewriteRoutePath = (typeof rewriteRoutePaths)[number];

export const prismPathById = {
  observe: "/observe",
  "observe-routing-health": "/observe/routing-health",
  "auth-login": "/auth/login",
  models: "/route/models",
  "model-export": "/route/models/export",
  "model-detail": "/route/models/$modelId",
  "route-endpoints": "/route/endpoints",
  "route-ban-policies": "/route/ban-policies",
  "system-settings": "/system/settings",
  "system-proxy-keys": "/system/proxy-keys",
  "route-pricing": "/route/pricing",
  "observe-requests": "/observe/requests",
  "observe-request-audit": "/observe/requests/$requestId/audit",
} as const satisfies Record<PrismRouteId, RewriteRoutePath>;

export function buildModelDetailPath(modelId: string | number): string {
  return `/route/models/${encodeURIComponent(String(modelId))}`;
}

export function buildRequestAuditPath(requestId: string): string {
  return `/observe/requests/${encodeURIComponent(requestId)}/audit`;
}
