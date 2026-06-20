export type RewriteRouteScope = "public" | "protected-global" | "protected-selected-profile" | "mixed";

export interface RewriteRouteContract {
  readonly currentPath: string;
  readonly targetPath: string;
  readonly component: string;
  readonly featureOwner: string;
  readonly scope: RewriteRouteScope;
  readonly protection: string;
  readonly workflows: readonly string[];
  readonly deletionCriterion: string;
}

export interface RewriteApiModuleContract {
  readonly module: string;
  readonly responsibility: string;
  readonly scopeRule: "global" | "selected-profile" | "mixed" | "runtime-bypass";
  readonly criticalRules: readonly string[];
}

export interface RewriteApiScopeRule {
  readonly id: string;
  readonly scope: "selected-profile" | "global" | "runtime-bypass";
  readonly paths: readonly string[];
  readonly rule: string;
}

export interface RewriteSafeguardContract {
  readonly id: string;
  readonly surface: string;
  readonly scope: RewriteRouteScope;
  readonly trigger: string;
  readonly clientGuard: string;
  readonly serverConflict: string;
  readonly evidence: readonly string[];
}

export interface RewriteImportExportContract {
  readonly id: string;
  readonly surface: string;
  readonly scope: "selected-profile" | "global";
  readonly transport: readonly string[];
  readonly validation: readonly string[];
}

export interface RewriteAuditHistoryContract {
  readonly id: string;
  readonly surface: string;
  readonly scope: RewriteRouteScope;
  readonly requirements: readonly string[];
}

export interface RewriteRealtimeContract {
  readonly id: string;
  readonly surface: string;
  readonly scope: RewriteRouteScope;
  readonly requirements: readonly string[];
}

export interface RewriteFeatureDeletionCriterion {
  readonly feature: string;
  readonly currentFiles: readonly string[];
  readonly requiredEvidence: readonly string[];
  readonly deleteWhen: string;
}

export interface RewriteContractMatrix {
  readonly routes: readonly RewriteRouteContract[];
  readonly apiModules: readonly RewriteApiModuleContract[];
  readonly apiScopeRules: readonly RewriteApiScopeRule[];
  readonly validationRules: readonly string[];
  readonly destructiveSafeguards: readonly RewriteSafeguardContract[];
  readonly importExportFlows: readonly RewriteImportExportContract[];
  readonly auditHistoryBehaviors: readonly RewriteAuditHistoryContract[];
  readonly realtimeBehaviors: readonly RewriteRealtimeContract[];
  readonly featureDeletionCriteria: readonly RewriteFeatureDeletionCriterion[];
  readonly assumptions: readonly string[];
}

export interface RewriteContractValidationResult {
  readonly valid: boolean;
  readonly errors: readonly string[];
}

export const REQUIRED_CURRENT_ROUTES = [
  "/",
  "/login",
  "/forgot-password",
  "/reset-password",
  "/dashboard",
  "/models",
  "/models/:id",
  "/endpoints",
  "/loadbalance-strategies",
  "/settings",
  "/proxy-api-keys",
  "/pricing-templates",
  "/request-logs",
  "/request-logs/:requestId/audit",
] as const;

export const REQUIRED_ROUTE_SCOPES = {
  "/": "protected-global",
  "/login": "public",
  "/forgot-password": "public",
  "/reset-password": "public",
  "/dashboard": "mixed",
  "/models": "protected-selected-profile",
  "/models/:id": "protected-selected-profile",
  "/endpoints": "protected-selected-profile",
  "/loadbalance-strategies": "protected-selected-profile",
  "/settings": "mixed",
  "/proxy-api-keys": "protected-global",
  "/pricing-templates": "protected-selected-profile",
  "/request-logs": "protected-selected-profile",
  "/request-logs/:requestId/audit": "protected-selected-profile",
} as const satisfies Record<(typeof REQUIRED_CURRENT_ROUTES)[number], RewriteRouteScope>;

export const REQUIRED_DESTRUCTIVE_SAFEGUARDS = [
  "profiles",
  "loadbalance-strategies",
  "pricing-templates",
  "proxy-api-keys",
  "startup-structural-secrets",
  "retention-deletion",
] as const;

export const rewriteContractMatrix = {
  routes: [
    {
      currentPath: "/login",
      targetPath: "/auth/login",
      component: "src/pages/LoginPage.tsx",
      featureOwner: "src/features/auth/",
      scope: "public",
      protection: "PublicOnlyRoute redirects to /dashboard when auth is disabled or the session is already authenticated.",
      workflows: ["auth-enabled login", "auth-disabled redirect", "server error display"],
      deletionCriterion: "Old login page/provider paths deleted after public-only auth parity tests pass.",
    },
    {
      currentPath: "/forgot-password",
      targetPath: "/auth/forgot-password",
      component: "src/pages/ForgotPasswordPage.tsx",
      featureOwner: "src/features/auth/",
      scope: "public",
      protection: "Public bootstrap mode only; authenticated or auth-disabled users redirect to the app.",
      workflows: ["password reset request", "SMTP/no-op status handling", "server validation messages"],
      deletionCriterion: "Old forgot-password page deleted after recovery request parity is covered.",
    },
    {
      currentPath: "/reset-password",
      targetPath: "/auth/reset-password",
      component: "src/pages/ResetPasswordPage.tsx",
      featureOwner: "src/features/auth/",
      scope: "public",
      protection: "Public bootstrap mode only; token/code failure states remain server-driven.",
      workflows: ["password reset confirm", "token/code validation", "server failure display"],
      deletionCriterion: "Old reset-password page deleted after confirm success/failure parity is covered.",
    },
    {
      currentPath: "/",
      targetPath: "/observe",
      component: "src/App.tsx redirect",
      featureOwner: "src/app/routes/",
      scope: "protected-global",
      protection: "ProtectedAppShell gate runs before redirecting to the dashboard/observe surface.",
      workflows: ["root redirect", "unauthenticated redirect to login with return state"],
      deletionCriterion: "Legacy root redirect removed only after target route redirect parity is tested.",
    },
    {
      currentPath: "/dashboard",
      targetPath: "/observe",
      component: "src/pages/DashboardPage.tsx",
      featureOwner: "src/features/observe/",
      scope: "mixed",
      protection: "Protected shell; overview combines global shell status with selected-profile stats and realtime subscriptions.",
      workflows: ["dashboard snapshot", "routing diagram", "recent request handoff", "analytics tab"],
      deletionCriterion: "Old dashboard/statistics/routing-diagram files deleted after realtime and routing parity evidence exists.",
    },
    {
      currentPath: "/models",
      targetPath: "/models",
      component: "src/features/models/ModelsFeaturePage.tsx",
      featureOwner: "src/features/models/",
      scope: "protected-selected-profile",
      protection: "Selected profile scopes model CRUD through X-Profile-Id; active runtime profile is not changed by selection.",
      workflows: ["model list/create/edit/search", "api-family grouping", "access-target authoring"],
      deletionCriterion: "Old models directory deleted after model CRUD and access-target parity tests pass.",
    },
    {
      currentPath: "/models/:id",
      targetPath: "/models/$modelId",
      component: "src/features/models/detail/ModelDetailFeaturePage.tsx",
      featureOwner: "src/features/models/detail/",
      scope: "protected-selected-profile",
      protection: "Selected-profile model detail; model-private connections and access targets stay scoped to the current profile.",
      workflows: ["terminal target connections", "ordered access targets", "connection health", "request-log handoff"],
      deletionCriterion: "Old model-detail cluster deleted after connection/access target/health parity evidence exists.",
    },
    {
      currentPath: "/endpoints",
      targetPath: "/route/endpoints",
      component: "src/features/endpoints/EndpointsFeaturePage.tsx",
      featureOwner: "src/features/endpoints/",
      scope: "protected-selected-profile",
      protection: "Selected-profile endpoint CRUD through X-Profile-Id with dependency-aware delete conflicts.",
      workflows: ["endpoint CRUD", "search/filter", "reorder", "duplicate", "delete conflict display"],
      deletionCriterion: "Old endpoints directory deleted after CRUD/reorder/delete conflict parity is covered.",
    },
    {
      currentPath: "/loadbalance-strategies",
      targetPath: "/route/ban-policies",
      component: "src/features/loadbalance/BanPoliciesFeaturePage.tsx",
      featureOwner: "src/features/loadbalance/",
      scope: "protected-selected-profile",
      protection: "Selected-profile explicit Ban Policy strategy CRUD; delete blocks on attached model count.",
      workflows: ["strategy CRUD", "default strategy creation", "current state/events handoff", "attached-model delete blocker"],
      deletionCriterion: "Old loadbalance strategy files deleted after Ban Policy bounds/defaults/blocker evidence exists.",
    },
    {
      currentPath: "/settings",
      targetPath: "/system/settings",
      component: "src/pages/SettingsPage.tsx",
      featureOwner: "src/features/settings/",
      scope: "mixed",
      protection: "Protected mixed route: Profile tab is selected-profile scoped; Global and Startup tabs are instance scoped.",
      workflows: ["profile config import/export", "billing/currency", "timezone", "audit/privacy", "auth", "retention", "startup bootstrap"],
      deletionCriterion: "Old settings cluster deleted after profile/global/startup flow parity evidence exists.",
    },
    {
      currentPath: "/proxy-api-keys",
      targetPath: "/control/proxy-keys",
      component: "src/features/proxy-keys/ProxyKeysFeaturePage.tsx",
      featureOwner: "src/features/proxy-keys/",
      scope: "protected-global",
      protection: "Global runtime credential management; selected profile must not scope proxy-key APIs.",
      workflows: ["create", "rotate", "one-time reveal", "edit metadata", "delete warnings", "quota/auth status"],
      deletionCriterion: "Old proxy-key directory deleted after credential lifecycle and delete warning evidence exists.",
    },
    {
      currentPath: "/pricing-templates",
      targetPath: "/route/pricing",
      component: "src/features/pricing/PricingFeaturePage.tsx",
      featureOwner: "src/features/pricing/",
      scope: "protected-selected-profile",
      protection: "Selected-profile pricing template CRUD with usage lookup and in-use delete blockers.",
      workflows: ["pricing CRUD", "decimal normalization", "usage drilldown", "CAS edit conflict", "delete blocker"],
      deletionCriterion: "Old pricing directory deleted after CRUD/usage/delete blocker parity evidence exists.",
    },
    {
      currentPath: "/request-logs",
      targetPath: "/observe/requests",
      component: "src/pages/RequestLogsPage.tsx",
      featureOwner: "src/features/request-logs/",
      scope: "protected-selected-profile",
      protection: "Selected-profile request history; URL owns filters, cursor state, and exact-request mode.",
      workflows: ["retained browse", "exact request mode", "detail sheet", "body preview/redaction metadata", "audit handoff"],
      deletionCriterion: "Old request-log directory deleted after URL-state/detail/audit parity evidence exists.",
    },
    {
      currentPath: "/request-logs/:requestId/audit",
      targetPath: "/observe/requests/$requestId/audit",
      component: "src/pages/request-logs/RequestLogAuditPage.tsx",
      featureOwner: "src/features/audit/",
      scope: "protected-selected-profile",
      protection: "Selected-profile dedicated audit drilldown; audit payload fetching stays isolated from normal browse/detail sheet.",
      workflows: ["audit cursor/detail", "weak request references", "request-time provenance", "body capture states"],
      deletionCriterion: "Old audit drilldown deleted after dedicated audit route parity evidence exists.",
    },
  ],
  apiModules: [
    {
      module: "src/lib/api/core.ts",
      responsibility: "Single request boundary with credentials, ApiError detail extraction, one auth-refresh retry, and X-Profile-Id injection.",
      scopeRule: "mixed",
      criticalRules: ["credentials: include", "one refresh retry for eligible /api/* 401s", "profile header injected only by profileScope matcher"],
    },
    {
      module: "src/lib/api/profileScope.ts",
      responsibility: "Allowlist selected-profile management API families that receive X-Profile-Id.",
      scopeRule: "selected-profile",
      criticalRules: ["models/endpoints/connections/pricing/stats/audit/loadbalance selected", "config profile import/export selected", "proxy keys/auth global"],
    },
    {
      module: "src/lib/api/authSettings.ts",
      responsibility: "Auth bootstrap/session/password recovery/auth settings/proxy key endpoints.",
      scopeRule: "global",
      criticalRules: ["public bootstrap for public auth routes", "proxy keys are global", "proxy key rotation returns one-time secret"],
    },
    {
      module: "src/lib/api/management.ts",
      responsibility: "Profiles, models, endpoints, loadbalance strategies, connections, pricing templates.",
      scopeRule: "mixed",
      criticalRules: ["profiles are global", "model/endpoint/loadbalance/pricing families are selected-profile", "Ban Policy fields are explicit and normalized"],
    },
    {
      module: "src/lib/api/observability.ts",
      responsibility: "Stats, request logs, audit, bootstrap config, config import/export, loadbalance state/events, costing/timezone/retention.",
      scopeRule: "mixed",
      criticalRules: ["stats/audit/loadbalance state are selected-profile", "bootstrap config and retention are global", "profile import/export tokens stay selected-profile"],
    },
    {
      module: "runtime /v1 and /v1beta proxy paths",
      responsibility: "Supported LLM runtime operations pass through launcher/backend runtime, not the management API client.",
      scopeRule: "runtime-bypass",
      criticalRules: ["must not receive X-Profile-Id", "must not route through generated or hand-authored management client helpers"],
    },
  ],
  apiScopeRules: [
    {
      id: "selected-profile-management",
      scope: "selected-profile",
      paths: ["/api/models", "/api/endpoints", "/api/connections", "/api/pricing-templates", "/api/stats", "/api/audit", "/api/loadbalance", "/api/settings/costing", "/api/settings/timezone", "/api/config/profile", "/api/config/header-blocklist-rules", "/api/config/user-agent-client-rules"],
      rule: "Attach X-Profile-Id when a selected profile exists; query keys must include selected profile ID.",
    },
    {
      id: "global-management",
      scope: "global",
      paths: ["/api/auth", "/api/profiles", "/api/settings/auth", "/api/settings/auth/proxy-keys", "/api/settings/log-retention", "/api/maintenance/log-retention/jobs", "/api/config/bootstrap"],
      rule: "Never attach X-Profile-Id; these surfaces are instance-global or active-runtime orchestration.",
    },
    {
      id: "runtime-bypass",
      scope: "runtime-bypass",
      paths: ["/v1", "/v1beta"],
      rule: "Runtime traffic is driven by the active runtime profile and backend operation registry; selected management profile must not influence headers or routing.",
    },
  ],
  validationRules: [
    "Profile config import is bundle version 3 and kind profile_config with encrypted secret_payload.",
    "Pricing import missing, null, blank, or whitespace component prices normalize to string '0' before decimal validation.",
    "OpenAI connection fields are valid only for api_family=openai, and OpenAI connections require openai_text_capability.",
    "preferred_context_utilization_threshold must be <= max_context_utilization for models and connections.",
    "Access target positions are ordered, unique, and target_type-specific; connection targets omit model target fields and model targets cannot self-reference.",
    "Every imported connection ref must be owned by exactly one model access target.",
    "Loadbalance Ban Policy fields preserve 100-599 status-code bounds, retry/backoff/jitter bounds, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, and ban modes off/temporary/until_reset.",
    "Startup values preserve backend revision, etag, secret update actions, required runtime.transport.request_timeout, required runtime.side_effects.attempt_timeout, and backend-provided apply capabilities.",
    "Profile activation sends expected_active_profile_id and refreshes the profile snapshot on stale 409 conflicts.",
    "Request-log filters remain URL-backed for ingress_request_id, model_id, endpoint_id, status_family, time_range, cursor, and exact request_id mode.",
  ],
  destructiveSafeguards: [
    {
      id: "profiles",
      surface: "Profile switcher dialogs",
      scope: "protected-global",
      trigger: "Delete selected profile",
      clientGuard: "Default and active profiles cannot be deleted; operator types the localized profile delete phrase before soft delete.",
      serverConflict: "Profile activation sends expected_active_profile_id and handles stale-active 409 by refreshing the profile snapshot.",
      evidence: ["src/components/layout/app-layout/ProfileDialogs.tsx", "src/context/profile/actions.ts"],
    },
    {
      id: "loadbalance-strategies",
      surface: "Loadbalance strategy delete dialog",
      scope: "protected-selected-profile",
      trigger: "Delete explicit Ban Policy strategy",
      clientGuard: "Dialog disables destructive action and shows attached_model_count when models reference the strategy.",
      serverConflict: "409 delete response may include attached_model_count and must update the dialog blocker.",
      evidence: ["src/features/loadbalance/BanPoliciesFeaturePage.tsx", "src/features/loadbalance/useBanPoliciesFeatureData.ts"],
    },
    {
      id: "pricing-templates",
      surface: "Pricing template delete dialog",
      scope: "protected-selected-profile",
      trigger: "Delete pricing template",
      clientGuard: "Usage lookup runs before delete; dependencies disable delete and render model/endpoint/terminal-target rows.",
      serverConflict: "409 delete response parses usage rows and keeps the conflict visible.",
      evidence: ["src/features/pricing/DeletePricingTemplateDialog.tsx", "src/features/pricing/usePricingFeatureData.ts"],
    },
    {
      id: "proxy-api-keys",
      surface: "Proxy API key delete alert",
      scope: "protected-global",
      trigger: "Delete runtime proxy API key",
      clientGuard: "Alert warns when auth enforcement is enabled and when a successor key exists; key preview and lifecycle remain visible.",
      serverConflict: "Delete result patches global key ledger without selected-profile scope.",
      evidence: ["src/pages/proxy-api-keys/ProxyKeyDeleteAlertDialog.tsx"],
    },
    {
      id: "startup-structural-secrets",
      surface: "Settings startup bootstrap save",
      scope: "protected-global",
      trigger: "Save structural or secret bootstrap changes",
      clientGuard: "Secret updates default to preserve; changed capabilities and dangerous confirmations are rendered before update.",
      serverConflict: "Backend validation may return required_confirmations for host, port, database URL, JWT signing key, bundle encryption key, or field-specific tokens.",
      evidence: ["src/features/settings/startup/startupFieldMetadata.ts"],
    },
    {
      id: "retention-deletion",
      surface: "Settings retention deletion dialog",
      scope: "protected-global",
      trigger: "Create manual log-retention cleanup job",
      clientGuard: "Operator chooses table/preset and types the delete keyword before job creation.",
      serverConflict: "Creates a durable maintenance job for request_logs, usage_request_events, audit_logs, or loadbalance_events with cutoff/delete_all and status URL.",
      evidence: ["src/pages/settings/useRetentionDeletionData.ts", "src/pages/settings/dialogs/DeleteConfirmDialog.tsx"],
    },
  ],
  importExportFlows: [
    {
      id: "profile-config-safe-export",
      surface: "Settings profile backup",
      scope: "selected-profile",
      transport: ["GET /api/config/profile/export", "requires X-Profile-Id", "downloads prism-profile-config-v3-YYYY-MM-DD.json"],
      validation: ["bundle version 3", "profile_config", "safe export excludes plaintext secrets"],
    },
    {
      id: "profile-config-dangerous-export",
      surface: "Settings profile backup with secrets",
      scope: "selected-profile",
      transport: ["POST /api/config/profile/export/with-secrets", "X-Prism-Dangerous-Confirm: profile-export", "requires X-Profile-Id"],
      validation: ["operator acknowledgement before export", "encrypted secret_payload retained"],
    },
    {
      id: "profile-config-import",
      surface: "Settings profile import",
      scope: "selected-profile",
      transport: ["POST /api/config/profile/import/preview", "POST /api/config/profile/import", "X-Prism-Preview-Token from preview", "requires X-Profile-Id"],
      validation: ["frontend Zod mirror validates before preview", "preview invalidates on bundle or profile change", "blocking errors prevent apply", "preview token binds exact bundle/profile snapshot"],
    }
  ],
  auditHistoryBehaviors: [
    {
      id: "audit-logs",
      surface: "Audit logs and request-log audit drilldown",
      scope: "protected-selected-profile",
      requirements: ["cursor-paged selected-profile reads", "weak request references retained", "dedicated /request-logs/:requestId/audit route remains distinct from generic request-log detail", "body capture provenance shown as disabled/metadata-only/body-present states"],
    },
    {
      id: "request-logs",
      surface: "Request log browse and exact-request mode",
      scope: "protected-selected-profile",
      requirements: ["URL-backed filters and cursor", "request_id exact mode", "body preview/redaction metadata visible", "ingress_request_id, requested model, final target, usage, TTFT, token-rate, and spend trust preserved"],
    },
    {
      id: "loadbalance-history",
      surface: "Loadbalance current state and events",
      scope: "protected-selected-profile",
      requirements: ["current state values available/retry_wait/banned", "events retry_scheduled/retry_exhausted/banned/unbanned/recovered/admission_rejected", "policy threshold snapshots preserved for history"],
    },
  ],
  realtimeBehaviors: [
    {
      id: "dashboard-realtime",
      surface: "Dashboard overview realtime",
      scope: "mixed",
      requirements: ["singleton websocket client", "dashboard.snapshot revision reconciliation", "dashboard.activity recent-activity reconciliation", "profile-aware subscription", "reconnect triggers REST bootstrap reconciliation"],
    },
    {
      id: "analytics-realtime",
      surface: "Dashboard analytics/statistics realtime",
      scope: "protected-selected-profile",
      requirements: ["analytics.snapshot includes profile_id, preset, sequence, generated_at", "scope matching includes preset", "refresh message available for analytics", "events buffer during reconnect sync"],
    },
  ],
  featureDeletionCriteria: [
    {
      feature: "Auth/session",
      currentFiles: ["src/context/AuthContext.tsx", "src/pages/LoginPage.tsx", "src/pages/ForgotPasswordPage.tsx", "src/pages/ResetPasswordPage.tsx"],
      requiredEvidence: ["route guard tests", "login/reset e2e"],
      deleteWhen: "Public-only, protected redirect, bootstrap mode, refresh, logout, and recovery flows have parity coverage.",
    },
    {
      feature: "Profile shell",
      currentFiles: ["src/context/ProfileContext.tsx", "src/components/layout/app-layout/*"],
      requiredEvidence: ["profile header tests", "profile switch/activate/delete e2e"],
      deleteWhen: "Selected-profile persistence, active mismatch, create/edit/activate/delete, and max-profile conflict behavior are ported.",
    },
    {
      feature: "API/data",
      currentFiles: ["src/lib/api/*.ts", "src/lib/types/*"],
      requiredEvidence: ["profile/global/runtime header tests", "auth refresh tests"],
      deleteWhen: "No direct fetches exist outside the API layer and hand-authored typed contracts are preserved.",
    },
    {
      feature: "Feature routes",
      currentFiles: ["src/pages/dashboard/*", "src/pages/models/*", "src/pages/model-detail/*", "src/pages/endpoints/*", "src/pages/loadbalance-strategies/*", "src/pages/pricing-templates/*", "src/pages/request-logs/*"],
      requiredEvidence: ["feature CRUD e2e", "destructive conflict e2e", "table/filter/search tests"],
      deleteWhen: "Each selected-profile route has executable parity evidence for CRUD, tables, forms, history, and destructive safeguards.",
    },
    {
      feature: "Settings/startup/global controls",
      currentFiles: ["src/pages/settings/*", "src/features/settings/startup/*", "src/pages/proxy-api-keys/*", ],
      requiredEvidence: ["config import/export e2e", "startup confirmation e2e", "proxy key lifecycle e2e"],
      deleteWhen: "Mixed/global settings, startup bootstrap, and proxy-key contracts are ported and verified.",
    },
  ],
  assumptions: [
    "The current frontend remains the functional contract oracle until each feature is replaced with executable parity evidence.",
    "API contracts remain hand-authored TypeScript clients; generated OpenAPI/client logic is intentionally out of scope.",
    "Selected profile scopes only management APIs that match profileScope.ts; active runtime profile continues to drive /v1 and /v1beta proxy traffic.",
    "Backend-enforced permission/conflict rules are mirrored in UI copy but never replaced by frontend-only checks.",
  ],
} as const satisfies RewriteContractMatrix;

function collectIds<T extends { readonly id: string }>(items: readonly T[]): Set<string> {
  return new Set(items.map((item) => item.id));
}

export function validateRewriteContractMatrix(matrix: RewriteContractMatrix): RewriteContractValidationResult {
  const errors: string[] = [];
  const routeByPath = new Map(matrix.routes.map((route) => [route.currentPath, route]));
  if (routeByPath.size !== matrix.routes.length) {
    errors.push("matrix routes must not contain duplicate currentPath entries");
  }
  if (matrix.routes.length !== REQUIRED_CURRENT_ROUTES.length) {
    errors.push(`matrix must list exactly ${REQUIRED_CURRENT_ROUTES.length} current routes`);
  }

  for (const route of REQUIRED_CURRENT_ROUTES) {
    const entry = routeByPath.get(route);
    if (!entry) {
      errors.push(`missing route ${route}`);
      continue;
    }
    if (entry.scope !== REQUIRED_ROUTE_SCOPES[route]) {
      errors.push(`route ${route} has scope ${entry.scope}; expected ${REQUIRED_ROUTE_SCOPES[route]}`);
    }
    if (entry.workflows.length === 0) {
      errors.push(`route ${route} must list workflows`);
    }
    if (!entry.deletionCriterion.trim()) {
      errors.push(`route ${route} must list a deletion criterion`);
    }
  }

  for (const scope of ["public", "protected-global", "protected-selected-profile", "mixed"] as const) {
    if (!matrix.routes.some((route) => route.scope === scope)) {
      errors.push(`missing route scope ${scope}`);
    }
  }

  const scopeRulesById = collectIds(matrix.apiScopeRules);
  for (const ruleId of ["selected-profile-management", "global-management", "runtime-bypass"]) {
    if (!scopeRulesById.has(ruleId)) {
      errors.push(`missing API scope rule ${ruleId}`);
    }
  }
  const selectedProfileRule = matrix.apiScopeRules.find((rule) => rule.id === "selected-profile-management");
  if (!selectedProfileRule?.rule.includes("X-Profile-Id")) {
    errors.push("selected-profile API rule must mention X-Profile-Id");
  }
  const runtimeRule = matrix.apiScopeRules.find((rule) => rule.id === "runtime-bypass");
  if (!runtimeRule?.paths.includes("/v1") || !runtimeRule.paths.includes("/v1beta")) {
    errors.push("runtime bypass rule must include /v1 and /v1beta");
  }
  if (!runtimeRule?.rule.includes("must not")) {
    errors.push("runtime bypass rule must forbid selected-profile headers/routing");
  }

  const safeguardIds = collectIds(matrix.destructiveSafeguards);
  for (const safeguardId of REQUIRED_DESTRUCTIVE_SAFEGUARDS) {
    if (!safeguardIds.has(safeguardId)) {
      errors.push(`missing destructive safeguard ${safeguardId}`);
    }
  }

  for (const safeguard of matrix.destructiveSafeguards) {
    if (!safeguard.clientGuard.trim()) {
      errors.push(`destructive safeguard ${safeguard.id} must describe a client guard`);
    }
    if (!safeguard.serverConflict.trim()) {
      errors.push(`destructive safeguard ${safeguard.id} must describe server conflict behavior`);
    }
  }

  const importFlowIds = collectIds(matrix.importExportFlows);
  for (const flowId of ["profile-config-safe-export", "profile-config-dangerous-export", "profile-config-import"]) {
    if (!importFlowIds.has(flowId)) {
      errors.push(`missing import/export flow ${flowId}`);
    }
  }
  const profileImportFlow = matrix.importExportFlows.find((flow) => flow.id === "profile-config-import");
  if (!profileImportFlow?.transport.some((item) => item.includes("X-Prism-Preview-Token"))) {
    errors.push("profile import flow must require X-Prism-Preview-Token");
  }


  if (!matrix.validationRules.some((rule) => rule.includes("OpenAI") && rule.includes("openai_text_capability"))) {
    errors.push("missing OpenAI connection validation rule");
  }
  if (!matrix.validationRules.some((rule) => rule.includes("preferred_context_utilization_threshold"))) {
    errors.push("missing context utilization validation rule");
  }
  if (!matrix.validationRules.some((rule) => rule.includes("Access target positions"))) {
    errors.push("missing access target validation rule");
  }
  if (!matrix.validationRules.some((rule) => rule.includes("Startup") && rule.includes("etag"))) {
    errors.push("missing startup revision/etag validation rule");
  }

  for (const module of matrix.apiModules) {
    if (module.criticalRules.length === 0) {
      errors.push(`API module ${module.module} must list critical rules`);
    }
  }

  if (matrix.auditHistoryBehaviors.length === 0) {
    errors.push("matrix audit/history behaviors must be populated");
  }
  if (matrix.realtimeBehaviors.length === 0) {
    errors.push("matrix realtime behaviors must be populated");
  }
  if (matrix.featureDeletionCriteria.length === 0) {
    errors.push("matrix deletion criteria must be populated");
  }
  if (matrix.assumptions.length === 0) {
    errors.push("matrix assumptions must be populated");
  }

  return { valid: errors.length === 0, errors };
}

export function assertRewriteContractMatrix(matrix: RewriteContractMatrix = rewriteContractMatrix): void {
  const result = validateRewriteContractMatrix(matrix);
  if (!result.valid) {
    throw new Error(result.errors.join("\n"));
  }
}

function markdownList(items: readonly string[]): string {
  return items.map((item) => `  - ${item}`).join("\n");
}

export function renderRewriteContractMatrixMarkdown(matrix: RewriteContractMatrix = rewriteContractMatrix): string {
  assertRewriteContractMatrix(matrix);

  const lines = [
    "# Frontend Rewrite Contract Matrix",
    "",
    "## Routes",
    "| Current route | Target route | Scope | Feature owner | Protection | Workflows | Deletion criterion |",
    "|---|---|---|---|---|---|---|",
    ...matrix.routes.map((route) => `| \`${route.currentPath}\` | \`${route.targetPath}\` | ${route.scope} | \`${route.featureOwner}\` | ${route.protection} | ${route.workflows.join("; ")} | ${route.deletionCriterion} |`),
    "",
    "## API Modules",
    ...matrix.apiModules.flatMap((module) => [
      `### ${module.module}`,
      `- Scope rule: ${module.scopeRule}`,
      `- Responsibility: ${module.responsibility}`,
      "- Critical rules:",
      markdownList(module.criticalRules),
      "",
    ]),
    "## API Scope Rules",
    ...matrix.apiScopeRules.flatMap((rule) => [
      `### ${rule.id}`,
      `- Scope: ${rule.scope}`,
      `- Paths: ${rule.paths.map((apiPath) => `\`${apiPath}\``).join(", ")}`,
      `- Rule: ${rule.rule}`,
      "",
    ]),
    "## Validation Rules",
    markdownList(matrix.validationRules),
    "",
    "## Destructive Safeguards",
    ...matrix.destructiveSafeguards.flatMap((safeguard) => [
      `### ${safeguard.surface}`,
      `- ID: ${safeguard.id}`,
      `- Scope: ${safeguard.scope}`,
      `- Trigger: ${safeguard.trigger}`,
      `- Client guard: ${safeguard.clientGuard}`,
      `- Server conflict: ${safeguard.serverConflict}`,
      "- Evidence:",
      markdownList(safeguard.evidence),
      "",
    ]),
    "## Import / Export Flows",
    ...matrix.importExportFlows.flatMap((flow) => [
      `### ${flow.surface}`,
      `- ID: ${flow.id}`,
      `- Scope: ${flow.scope}`,
      "- Transport:",
      markdownList(flow.transport),
      "- Validation:",
      markdownList(flow.validation),
      "",
    ]),
    "## Audit / History Behavior",
    ...matrix.auditHistoryBehaviors.flatMap((behavior) => [
      `### ${behavior.surface}`,
      `- ID: ${behavior.id}`,
      `- Scope: ${behavior.scope}`,
      "- Requirements:",
      markdownList(behavior.requirements),
      "",
    ]),
    "## Realtime Behavior",
    ...matrix.realtimeBehaviors.flatMap((behavior) => [
      `### ${behavior.surface}`,
      `- ID: ${behavior.id}`,
      `- Scope: ${behavior.scope}`,
      "- Requirements:",
      markdownList(behavior.requirements),
      "",
    ]),
    "## Feature Deletion Criteria",
    ...matrix.featureDeletionCriteria.flatMap((criterion) => [
      `### ${criterion.feature}`,
      "- Current files:",
      markdownList(criterion.currentFiles),
      "- Required evidence:",
      markdownList(criterion.requiredEvidence),
      `- Delete when: ${criterion.deleteWhen}`,
      "",
    ]),
    "## Assumptions",
    markdownList(matrix.assumptions),
    "",
  ];

  return `${lines.join("\n").trimEnd()}\n`;
}
