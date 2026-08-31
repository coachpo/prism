package platformhttp

import (
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/priority"
	"net/http"
	pathpkg "path"
	"strings"
	"time"
)

const managementAdmissionTimeout = 60 * time.Second

// runtimeCacheEffect declares which runtime caches a registered management
// route must advance after a successful write. Mutation specs must explicitly
// declare none or at least one non-none flag, enforced by
// TestManagementMutationRouteSpecsDeclareCacheEffect.
type runtimeCacheEffect struct {
	none         bool // explicitly "this write affects no runtime cache"
	auth         bool
	planning     bool // Default profile planning snapshot
	allPlanning  bool
	routeWitness bool
}

type managementRouteSpec struct {
	name                        string
	method                      string
	pattern                     string
	tier                        priority.ManagementTier
	timeout                     time.Duration
	releaseAdmissionFromHandler bool
	settingsSchemaGuard         bool
	profileScoped               bool               // whether the frontend must send X-Profile-Id
	privateNoStore              bool               // whether every response, including middleware rejection, is private and non-cacheable
	cache                       runtimeCacheEffect // runtime cache invalidation contract
	notes                       string             // notes column in the generated contract JSON
}

var managementRouteSpecs = []managementRouteSpec{
	{name: "auth status", method: http.MethodGet, pattern: "/auth/status", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "auth public bootstrap", method: http.MethodGet, pattern: "/auth/public-bootstrap", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "auth login", method: http.MethodPost, pattern: "/auth/login", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{none: true}},
	{name: "auth logout", method: http.MethodPost, pattern: "/auth/logout", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{none: true}},
	{name: "auth refresh", method: http.MethodPost, pattern: "/auth/refresh", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{none: true}},
	{name: "auth session", method: http.MethodGet, pattern: "/auth/session", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "auth public operation status", method: http.MethodGet, pattern: "/auth/operations/{operation_id}/status", tier: priority.ManagementTierM1, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "auth operation result", method: http.MethodGet, pattern: "/settings/auth/operations/{operation_id}", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{}, notes: "protected auth operation result"},
	{name: "auth settings read", method: http.MethodGet, pattern: "/settings/auth", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "auth settings write", method: http.MethodPut, pattern: "/settings/auth", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{auth: true}, notes: "Auth settings writes refresh runtime auth state."},
	{name: "auth proxy keys list", method: http.MethodGet, pattern: "/settings/auth/proxy-keys", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "auth proxy keys create", method: http.MethodPost, pattern: "/settings/auth/proxy-keys", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{auth: true}, notes: "Proxy key creation refreshes runtime auth state."},
	{name: "auth proxy key update", method: http.MethodPatch, pattern: "/settings/auth/proxy-keys/{key_id}", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{auth: true}, notes: "Proxy key mutation or retirement refreshes runtime auth state."},
	{name: "auth proxy key rotate", method: http.MethodPost, pattern: "/settings/auth/proxy-keys/{key_id}/rotate", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{auth: true}, notes: "Proxy key rotation refreshes runtime auth state."},
	{name: "auth proxy key delete", method: http.MethodDelete, pattern: "/settings/auth/proxy-keys/{key_id}", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{auth: true}, notes: "Proxy key mutation or retirement refreshes runtime auth state."},
	{name: "audit logs list", method: http.MethodGet, pattern: "/audit/logs", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile audit log list read."},
	{name: "audit log read", method: http.MethodGet, pattern: "/audit/logs/{log_id}", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile audit log detail read."},
	{name: "audit request body download", method: http.MethodGet, pattern: "/audit/logs/{log_id}/body/request", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile audit raw request body download."},
	{name: "audit response body download", method: http.MethodGet, pattern: "/audit/logs/{log_id}/body/response", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile audit raw response body download."},
	{name: "log retention job create", method: http.MethodPost, pattern: "/maintenance/log-retention/jobs", tier: priority.ManagementTierM3, settingsSchemaGuard: true, profileScoped: false, cache: runtimeCacheEffect{none: true}, notes: "sealed global manual log-retention job acceptance"},
	{name: "management jobs list", method: http.MethodGet, pattern: "/management/jobs", tier: priority.ManagementTierM3, profileScoped: false, cache: runtimeCacheEffect{}, notes: "global log-retention job discovery uses explicit scope/type filters"},
	{name: "management job read", method: http.MethodGet, pattern: "/management/jobs/{job_id}", tier: priority.ManagementTierM3, profileScoped: false, cache: runtimeCacheEffect{}, notes: "global log-retention job detail uses explicit scope/type filters"},
	{name: "management job cancel", method: http.MethodPost, pattern: "/management/jobs/{job_id}/cancel", tier: priority.ManagementTierM3, settingsSchemaGuard: true, profileScoped: false, cache: runtimeCacheEffect{none: true}, notes: "durable global log-retention cancel operation"},
	{name: "config header rules list", method: http.MethodGet, pattern: "/config/header-blocklist-rules", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Header blocklist reads are scoped; creation refreshes planning."},
	{name: "config header rule read", method: http.MethodGet, pattern: "/config/header-blocklist-rules/{rule_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Header blocklist reads are scoped; mutations refresh planning."},
	{name: "config header rule create", method: http.MethodPost, pattern: "/config/header-blocklist-rules", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Header blocklist reads are scoped; creation refreshes planning."},
	{name: "config header rule update", method: http.MethodPatch, pattern: "/config/header-blocklist-rules/{rule_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Header blocklist reads are scoped; mutations refresh planning."},
	{name: "config header rule delete", method: http.MethodDelete, pattern: "/config/header-blocklist-rules/{rule_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Header blocklist reads are scoped; mutations refresh planning."},
	{name: "config user-agent rules list", method: http.MethodGet, pattern: "/config/user-agent-client-rules", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile user-agent client rules are non-invalidating in current classifier."},
	{name: "config user-agent rule read", method: http.MethodGet, pattern: "/config/user-agent-client-rules/{rule_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile user-agent client rule detail and mutations are non-invalidating in current classifier."},
	{name: "config user-agent rule create", method: http.MethodPost, pattern: "/config/user-agent-client-rules", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile user-agent client rules are non-invalidating in current classifier."},
	{name: "config user-agent rule update", method: http.MethodPatch, pattern: "/config/user-agent-client-rules/{rule_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile user-agent client rule detail and mutations are non-invalidating in current classifier."},
	{name: "config user-agent rule delete", method: http.MethodDelete, pattern: "/config/user-agent-client-rules/{rule_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile user-agent client rule detail and mutations are non-invalidating in current classifier."},
	{name: "model catalog read", method: http.MethodGet, pattern: "/models/{model_config_id}/catalog", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{}, notes: "Default-profile models.dev binding read; metadata never enters the runtime snapshot."},
	{name: "model catalog candidates read", method: http.MethodGet, pattern: "/models/{model_config_id}/catalog/candidates", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{}, notes: "Bounded catalog candidate search for manual binding."},
	{name: "model catalog match preview", method: http.MethodPost, pattern: "/models/{model_config_id}/catalog/match-preview", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only unique-match preview; remote I/O stays outside transactions and never invalidates planning."},
	{name: "model catalog bind", method: http.MethodPost, pattern: "/models/{model_config_id}/catalog/bind", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Catalog metadata bind/refresh only; never invalidates planning."},
	{name: "model catalog refresh preview", method: http.MethodPost, pattern: "/models/{model_config_id}/catalog/refresh/preview", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only source diff preview against the fetched catalog revision."},
	{name: "model catalog refresh commit", method: http.MethodPost, pattern: "/models/{model_config_id}/catalog/refresh/commit", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Replaces source_values only after the revision guard; manual overrides survive and planning is untouched."},
	{name: "model catalog override write", method: http.MethodPut, pattern: "/models/{model_config_id}/catalog/override", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Per-field manual override authoring; management-only metadata."},
	{name: "model catalog override clear", method: http.MethodDelete, pattern: "/models/{model_config_id}/catalog/override", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Bulk per-field restore to source values."},
	{name: "model catalog unbind", method: http.MethodDelete, pattern: "/models/{model_config_id}/catalog", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Drops the models.dev binding row; model runtime identity is unchanged."},
	{name: "model connection batch", method: http.MethodPost, pattern: "/models/connections/batch", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile batch connection read."},
	{name: "model connections list", method: http.MethodGet, pattern: "/models/{model_config_id}/connections", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Owner-scoped connection list reads and creation writes."},
	{name: "model connection create", method: http.MethodPost, pattern: "/models/{model_config_id}/connections", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Owner-scoped connection list reads and creation writes."},
	{name: "model connection update", method: http.MethodPatch, pattern: "/models/{model_config_id}/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Owner-scoped connection mutation refreshes planning."},
	{name: "model connection copy", method: http.MethodPost, pattern: "/models/{model_config_id}/connections/{connection_id}/copies", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Transactional Terminal Target batch copy; single after-commit planning invalidation."},
	{name: "model connection delete", method: http.MethodDelete, pattern: "/models/{model_config_id}/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Owner-scoped connection mutation refreshes planning."},
	{name: "legacy model connection put rejection", method: http.MethodPut, pattern: "/models/{model_config_id}/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Owner-scoped connection mutation refreshes planning."},
	{name: "legacy model connection priority rejection", method: http.MethodPatch, pattern: "/models/{model_config_id}/connections/{connection_id}/priority", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Classifier still recognizes legacy owner-scoped priority mutation."},
	{name: "connections list", method: http.MethodGet, pattern: "/connections", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile reusable connection list read."},
	{name: "public connection create rejection", method: http.MethodPost, pattern: "/connections", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}},
	{name: "connection read", method: http.MethodGet, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Connection read is scoped; legacy write/delete paths refresh planning if successful."},
	{name: "public connection put rejection", method: http.MethodPut, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Connection read is scoped; legacy write/delete paths refresh planning if successful."},
	{name: "public connection patch rejection", method: http.MethodPatch, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Connection read is scoped; legacy write/delete paths refresh planning if successful."},
	{name: "public connection delete rejection", method: http.MethodDelete, pattern: "/connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Connection read is scoped; legacy write/delete paths refresh planning if successful."},
	{name: "connection references list", method: http.MethodGet, pattern: "/connections/{connection_id}/references", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile connection reference read."},
	{name: "pricing templates list", method: http.MethodGet, pattern: "/pricing-templates", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Pricing template list reads and creation writes."},
	{name: "pricing template create", method: http.MethodPost, pattern: "/pricing-templates", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Pricing template list reads and creation writes."},
	{name: "pricing template import", method: http.MethodPost, pattern: "/pricing-templates/import", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Pricing template JSON import creates or updates templates and refreshes planning."},
	{name: "pricing template import commit", method: http.MethodPost, pattern: "/pricing-templates/import/commit", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}},
	{name: "pricing template catalog preview", method: http.MethodPost, pattern: "/pricing-templates/catalog/preview", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only source-linked price plan preview; remote I/O stays outside transactions."},
	{name: "pricing template catalog commit", method: http.MethodPost, pattern: "/pricing-templates/catalog/commit", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{planning: true}, notes: "Atomic source-linked template import/revision plus Terminal Target assignment; wakes planning after commit."},
	{name: "pricing template read", method: http.MethodGet, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Pricing template reads are scoped; writes refresh planning."},
	{name: "pricing template update", method: http.MethodPut, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Pricing template reads are scoped; writes refresh planning."},
	{name: "pricing template delete", method: http.MethodDelete, pattern: "/pricing-templates/{template_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Pricing template reads are scoped; writes refresh planning."},
	{name: "pricing template revisions", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/revisions", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}},
	{name: "pricing template impact", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/impact", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}},
	{name: "currency migration preview", method: http.MethodPost, pattern: "/settings/costing/currency-migrations/preview", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Currency cutover, repair, and archive previews are read-only authoritative owner checks."},
	{name: "currency migration commit", method: http.MethodPost, pattern: "/settings/costing/currency-migrations/commit", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Currency migration commit is atomic and wakes the existing pricing planning invalidation owner."},
	{name: "currency migration inventory templates", method: http.MethodGet, pattern: "/settings/costing/pricing-migration-inventories/{inventory_id}/templates", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Pricing-owned immutable template migration evidence is a bounded Settings read-only scaffold."},
	{name: "currency migration inventory fx evidence", method: http.MethodGet, pattern: "/settings/costing/pricing-migration-inventories/{inventory_id}/fx-evidence", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Pricing-owned immutable FX migration evidence is a bounded Settings read-only scaffold."},
	{name: "currency migration draft create", method: http.MethodPost, pattern: "/settings/costing/currency-migration-drafts", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Currency migration draft creation reserves an operation and stores only bounded draft metadata."},
	{name: "currency migration draft read", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Currency draft header recovery is bounded and non-planning."},
	{name: "currency migration draft chunks", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/chunks", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Currency draft chunk metadata uses a signed bounded cursor."},
	{name: "currency migration draft chunk write", method: http.MethodPut, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/chunks/{ordinal}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Currency draft chunks are isolated authoring writes and do not touch live pricing state."},
	{name: "currency migration draft seal", method: http.MethodPost, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/seal", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Currency draft sealing only materializes immutable draft items."},
	{name: "currency migration draft items", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/items", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Sealed currency draft items are returned through a bounded signed page."},
	{name: "currency migration draft preview items", method: http.MethodGet, pattern: "/settings/costing/currency-migration-drafts/{draft_id}/preview-items", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Currency preview items are owner-derived bounded pages."},
	{name: "pricing template connections list", method: http.MethodGet, pattern: "/pricing-templates/{template_id}/connections", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile pricing-template connection read."},
	{name: "endpoint connections list", method: http.MethodGet, pattern: "/endpoints/connections", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile endpoint connection dropdown read."},
	{name: "endpoint references batch", method: http.MethodPost, pattern: "/endpoints/references/batch", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only direct endpoint reference batch; no planning invalidation."},
	{name: "endpoints list", method: http.MethodGet, pattern: "/endpoints", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Endpoint list reads and endpoint creation writes."},
	{name: "endpoint create", method: http.MethodPost, pattern: "/endpoints", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Endpoint list reads and endpoint creation writes."},
	{name: "endpoint update", method: http.MethodPut, pattern: "/endpoints/{endpoint_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Endpoint update and delete refresh planning."},
	{name: "endpoint references detail", method: http.MethodGet, pattern: "/endpoints/{endpoint_id}/references", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Read-only bounded direct endpoint reference detail."},
	{name: "endpoint verify", method: http.MethodPost, pattern: "/endpoints/{endpoint_id}/verify", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only one-time family-aware endpoint metadata verification."},
	{name: "endpoint orphan cleanup", method: http.MethodDelete, pattern: "/endpoints/{endpoint_id}/orphan-connections/{connection_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Ownerless endpoint connection cleanup refreshes planning after commit."},
	{name: "endpoint duplicate", method: http.MethodPost, pattern: "/endpoints/{endpoint_id}/duplicate", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Endpoint duplication refreshes planning."},
	{name: "endpoint delete", method: http.MethodDelete, pattern: "/endpoints/{endpoint_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Endpoint update and delete refresh planning."},
	{name: "loadbalance strategies list", method: http.MethodGet, pattern: "/loadbalance/strategies", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Load-balance strategy list reads and creation writes."},
	{name: "loadbalance strategy create", method: http.MethodPost, pattern: "/loadbalance/strategies", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Load-balance strategy list reads and creation writes."},
	{name: "loadbalance strategy defaults create", method: http.MethodPost, pattern: "/loadbalance/strategies/defaults", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Default strategy creation refreshes planning."},
	{name: "loadbalance strategy preview", method: http.MethodPost, pattern: "/loadbalance/strategies/preview", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Side-effect-free retry/ban effect preview; never invalidates planning."},
	{name: "loadbalance strategy read", method: http.MethodGet, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Strategy reads are scoped; writes refresh planning."},
	{name: "loadbalance strategy update", method: http.MethodPut, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Strategy reads are scoped; writes refresh planning."},
	{name: "loadbalance strategy set default", method: http.MethodPut, pattern: "/loadbalance/strategies/{strategy_id}/default", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Set-default only changes authoring metadata; existing model bindings and planning stay untouched."},
	{name: "loadbalance strategy models list", method: http.MethodGet, pattern: "/loadbalance/strategies/{strategy_id}/models", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Bounded lazy strategy impact list read."},
	{name: "loadbalance strategy delete", method: http.MethodDelete, pattern: "/loadbalance/strategies/{strategy_id}", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Strategy reads are scoped; writes refresh planning."},
	{name: "loadbalance current-state list", method: http.MethodGet, pattern: "/loadbalance/current-state", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile runtime state read."},
	{name: "loadbalance current-state reset", method: http.MethodPost, pattern: "/loadbalance/current-state/{connection_id}/reset", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile runtime state reset is non-invalidating in current classifier."},
	{name: "loadbalance incidents list", method: http.MethodGet, pattern: "/loadbalance/incidents", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile active ban and recent failover incident read."},
	{name: "loadbalance events list", method: http.MethodGet, pattern: "/loadbalance/events", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile load-balance event list read."},
	{name: "loadbalance events query context", method: http.MethodPost, pattern: "/loadbalance/events/query-context", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Signed events query-context issue; read-only and never invalidates planning."},
	{name: "loadbalance event read", method: http.MethodGet, pattern: "/loadbalance/events/{event_id}", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile load-balance event detail read."},
	{name: "model export source", method: http.MethodGet, pattern: "/models/exports/pi/source", tier: priority.ManagementTierM3, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{}, notes: "Default-profile Pi client model-config export source snapshot; read-only, never invalidates planning."},
	{name: "model export render", method: http.MethodPost, pattern: "/models/exports/pi/render", tier: priority.ManagementTierM3, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Deterministic Pi client file render; digest-guarded replay with no network I/O and no planning effects."},
	{name: "model pi read", method: http.MethodGet, pattern: "/models/{model_config_id}/pi", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{}, notes: "Single-model Pi management read: model identity, one catalog evidence block, live exact-candidate evidence, and the persisted binding; read-only, planning-neutral, loads no targets/pricing/digest/credential."},
	{name: "model pi bind", method: http.MethodPost, pattern: "/models/{model_config_id}/pi/bind", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Freezes a pi.dev directory coordinate plus the confirmed Prism identity snapshot into model_pi_catalog_bindings; never invalidates planning."},
	{name: "model pi catalog search", method: http.MethodPost, pattern: "/models/{model_config_id}/pi/search", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only bounded pi.dev model-id directory search restricted to the model's current final Pi API; never selects, never writes, never invalidates planning."},
	{name: "model pi refresh preview", method: http.MethodPost, pattern: "/models/{model_config_id}/pi/refresh/preview", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Read-only source diff preview against the fetched pi.dev catalog revision."},
	{name: "model pi refresh commit", method: http.MethodPost, pattern: "/models/{model_config_id}/pi/refresh/commit", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Replaces the binding's source fields only after the revision guard; manual overrides survive and planning is untouched."},
	{name: "model pi override write", method: http.MethodPut, pattern: "/models/{model_config_id}/pi/override", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Per-field manual override authoring on the seven safe pi.dev leaves; management-only metadata."},
	{name: "model pi override clear", method: http.MethodDelete, pattern: "/models/{model_config_id}/pi/override", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Bulk per-field restore to the bound source values."},
	{name: "model pi unbind", method: http.MethodDelete, pattern: "/models/{model_config_id}/pi", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{none: true}, notes: "Drops the model_pi_catalog_bindings row; model runtime identity is unchanged."},
	{name: "models by endpoints", method: http.MethodPost, pattern: "/models/by-endpoints", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile endpoint-to-model lookup."},
	{name: "model targets list", method: http.MethodGet, pattern: "/models/{model_config_id}/targets", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Model target list reads and target creation writes."},
	{name: "model target create", method: http.MethodPost, pattern: "/models/{model_config_id}/targets", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model target list reads and target creation writes."},
	{name: "model target update", method: http.MethodPut, pattern: "/models/{model_config_id}/targets/{target_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model target update, metadata patch, and delete refresh planning."},
	{name: "model target metadata patch", method: http.MethodPatch, pattern: "/models/{model_config_id}/targets/{target_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model target update, metadata patch, and delete refresh planning."},
	{name: "model target move", method: http.MethodPatch, pattern: "/models/{model_config_id}/targets/{target_id}/position", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model target ordering changes refresh planning."},
	{name: "model target delete", method: http.MethodDelete, pattern: "/models/{model_config_id}/targets/{target_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model target update, metadata patch, and delete refresh planning."},
	{name: "model read", method: http.MethodGet, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2, profileScoped: true, privateNoStore: true, cache: runtimeCacheEffect{}, notes: "Model reads are scoped; model updates and deletes refresh planning. The response embeds models.dev binding evidence while that legacy field remains, so it stays private and non-cacheable."},
	{name: "model routing diagnostics", method: http.MethodGet, pattern: "/models/{model_config_id}/routing-diagnostics", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Read-only static routing diagnostics; no planning invalidation."},
	{name: "model create", method: http.MethodPost, pattern: "/models", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Default-profile model list reads and model creation writes."},
	{name: "model update", method: http.MethodPut, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model reads are scoped; model updates and deletes refresh planning."},
	{name: "model delete", method: http.MethodDelete, pattern: "/models/{model_config_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true, routeWitness: true}, notes: "Model reads are scoped; model updates and deletes refresh planning."},
	{name: "models by endpoint", method: http.MethodGet, pattern: "/models/by-endpoint/{endpoint_id}", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile models for one endpoint."},
	{name: "models list", method: http.MethodGet, pattern: "/models", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile model list reads and model creation writes."},
	{name: "models route-witness resolver", method: http.MethodGet, pattern: "/models/route-witnesses", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}},
	{name: "settings audit read", method: http.MethodGet, pattern: "/settings/audit", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Audit reads are scoped; full-family replacement refreshes planning."},
	{name: "settings audit write", method: http.MethodPut, pattern: "/settings/audit", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Audit reads are scoped; full-family replacement refreshes planning."},
	{name: "settings audit storage summary", method: http.MethodGet, pattern: "/settings/audit/storage-summary", tier: priority.ManagementTierM3, profileScoped: false, cache: runtimeCacheEffect{}, notes: "settings audit logical storage summary (M3)"},
	{name: "settings costing read", method: http.MethodGet, pattern: "/settings/costing", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Costing reads are scoped; writes refresh planning."},
	{name: "settings costing write", method: http.MethodPut, pattern: "/settings/costing", tier: priority.ManagementTierM2, profileScoped: true, cache: runtimeCacheEffect{planning: true}, notes: "Costing reads are scoped; writes refresh planning."},
	{name: "settings log retention read", method: http.MethodGet, pattern: "/settings/log-retention", tier: priority.ManagementTierM2, profileScoped: false, cache: runtimeCacheEffect{}},
	{name: "settings log retention write", method: http.MethodPut, pattern: "/settings/log-retention", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: false, cache: runtimeCacheEffect{none: true}},
	{name: "settings owner drift archive", method: http.MethodPost, pattern: "/settings/log-retention/owner-drift-archive", tier: priority.ManagementTierM2, settingsSchemaGuard: true, profileScoped: false, cache: runtimeCacheEffect{none: true}, notes: "archive current owner-drift heads; no policy change (no runtime cache invalidation: read-only evidence / evidence-only)"},
	{name: "retention preflight create", method: http.MethodPost, pattern: "/maintenance/log-retention/preflights", tier: priority.ManagementTierM3, settingsSchemaGuard: true, profileScoped: false, cache: runtimeCacheEffect{none: true}, notes: "fresh destructive preflight (read-only) (no runtime cache invalidation: read-only evidence / evidence-only)"},
	{name: "retention job checkpoints", method: http.MethodGet, pattern: "/management/jobs/{job_id}/checkpoints", tier: priority.ManagementTierM3, profileScoped: false, cache: runtimeCacheEffect{}, notes: "global log-retention job checkpoints"},
	{name: "retention job partitions", method: http.MethodGet, pattern: "/management/jobs/{job_id}/partitions", tier: priority.ManagementTierM3, profileScoped: false, cache: runtimeCacheEffect{}, notes: "global log-retention partition evidence"},
	{name: "stats requests list", method: http.MethodGet, pattern: "/stats/requests", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile request log list read."},
	{name: "stats requests export", method: http.MethodGet, pattern: "/stats/requests/export", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile full filtered CSV export."},
	{name: "stats cost segments", method: http.MethodGet, pattern: "/stats/cost-segments", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile cost segment catalogue."},
	{name: "stats cost segment symbols", method: http.MethodGet, pattern: "/stats/cost-segments/{segment_key}/symbols", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}},
	{name: "stats request filter options", method: http.MethodGet, pattern: "/stats/request-filter-options/proxy-api-keys", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}},
	{name: "dashboard stats", method: http.MethodGet, pattern: "/stats/dashboard", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile dashboard statistics read."},
	{name: "dashboard recent activity", method: http.MethodGet, pattern: "/stats/dashboard/recent-activity", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile dashboard recent activity read."},
	{name: "stats request read", method: http.MethodGet, pattern: "/stats/requests/{request_id}", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile request log detail read."},
	{name: "stats summary", method: http.MethodGet, pattern: "/stats/summary", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile stats summary read."},
	{name: "stats model metrics", method: http.MethodPost, pattern: "/stats/models/metrics", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{none: true}, notes: "Default-profile model metrics batch read."},
	{name: "stats connection success rates", method: http.MethodGet, pattern: "/stats/connection-success-rates", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile connection success-rate read."},
	{name: "stats throughput", method: http.MethodGet, pattern: "/stats/throughput", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile throughput statistics read."},
	{name: "stats spending", method: http.MethodGet, pattern: "/stats/spending", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile spending report read."},
	{name: "stats usage snapshot", method: http.MethodGet, pattern: "/stats/usage-snapshot", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile usage snapshot read."},
	{name: "stats query context", method: http.MethodGet, pattern: "/stats/query-context", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Observe signed query-context creation."},
	{name: "stats usage summary", method: http.MethodGet, pattern: "/stats/usage-summary", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Observe window KPI aggregate."},
	{name: "stats usage series", method: http.MethodGet, pattern: "/stats/usage-series", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Observe single main chart."},
	{name: "stats usage errors", method: http.MethodGet, pattern: "/stats/usage-errors", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Observe error aggregation with Requests deep-link filters."},
	{name: "stats dashboard now", method: http.MethodGet, pattern: "/stats/dashboard/now", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Observe Now rolling strip."},
	{name: "stats observe activity", method: http.MethodGet, pattern: "/stats/observe-activity", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}},
	{name: "stats endpoint model statistics", method: http.MethodGet, pattern: "/stats/endpoints/{endpoint_id}/models", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Default-profile endpoint model usage read."},
	{name: "stats endpoint terminal target statistics", method: http.MethodGet, pattern: "/stats/endpoints/{endpoint_id}/terminal-targets", tier: priority.ManagementTierM3, profileScoped: true, cache: runtimeCacheEffect{}, notes: "Bounded Terminal Target drill-down per endpoint (lazy expansion)."},
}

func (s managementRouteSpec) AdmissionSpec() admission.Spec {
	timeout := s.timeout
	if timeout == 0 {
		timeout = managementAdmissionTimeout
	}
	return admission.Spec{
		Name: s.name,
		Metadata: priority.Metadata{
			Priority:       priority.PriorityManagement,
			ManagementTier: s.tier,
		},
		Timeout: timeout,
	}
}

func matchManagementRouteSpec(method string, rawPath string) (managementRouteSpec, bool) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod == http.MethodHead {
		normalizedMethod = http.MethodGet
	}
	if normalizedMethod == "" || normalizedMethod == http.MethodOptions {
		return managementRouteSpec{}, false
	}

	normalizedPath := normalizeManagementRoutePath(rawPath)
	if normalizedPath == "" || normalizedPath == "/" {
		return managementRouteSpec{}, false
	}

	for _, spec := range managementRouteSpecs {
		if spec.matches(normalizedMethod, normalizedPath) {
			return spec, true
		}
	}
	return managementRouteSpec{}, false
}

func normalizeManagementRoutePath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(trimmed, "/"))
	if cleaned == "/api" {
		return "/"
	}
	if strings.HasPrefix(cleaned, "/api/") {
		return strings.TrimPrefix(cleaned, "/api")
	}
	return cleaned
}

func (s managementRouteSpec) matches(method string, path string) bool {
	if s.method != method {
		return false
	}
	patternSegments := managementRouteSegments(normalizeManagementRoutePath(s.pattern))
	pathSegments := managementRouteSegments(path)
	if len(patternSegments) != len(pathSegments) {
		return false
	}
	for idx := range patternSegments {
		segment := patternSegments[idx]
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			if pathSegments[idx] == "" {
				return false
			}
			continue
		}
		if segment != pathSegments[idx] {
			return false
		}
	}
	return true
}

func managementRouteSegments(path string) []string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
