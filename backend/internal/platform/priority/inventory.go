package priority

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

type RouteInventoryEntry struct {
	Method         string
	Path           string
	Priority       LogicalPriority
	ManagementTier ManagementTier
	Source         string
	Classified     bool
}

type ResourceInventoryEntry struct {
	Family             string
	Name               string
	Priority           LogicalPriority
	ManagementTier     ManagementTier
	BackgroundSubclass BackgroundSubclass
	Location           string
	Functions          []string
	CurrentBehavior    string
	Classified         bool
}

type Inventory struct {
	Routes    []RouteInventoryEntry
	Resources []ResourceInventoryEntry
}

func DefaultInventory() Inventory {
	return Inventory{
		Routes: []RouteInventoryEntry{
			{Method: "ANY", Path: "/v1", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Method: "ANY", Path: "/v1/*", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Method: "ANY", Path: "/v1beta", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Method: "ANY", Path: "/v1beta/*", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Method: "GET", Path: "/api/auth/status", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/auth/public-bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/auth/login", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/auth/logout", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/auth/refresh", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/auth/session", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/auth/password-reset/request", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/auth/password-reset/confirm", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/profiles/bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/profiles/active", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/profiles/{profile_id}/activate", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/config/bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/config/bootstrap/validate", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "PUT", Path: "/api/config/bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "ANY", Path: "/api/* default unlisted management route", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/server.go: classifyManagementRoute default", Classified: true},
			{Method: "GET", Path: "/api/realtime/ws", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/stats/*", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "DELETE", Path: "/api/stats/requests|statistics", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/audit/logs{/{log_id}}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "DELETE", Path: "/api/audit/logs", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/loadbalance/current-state|events{/{event_id}}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "DELETE", Path: "/api/loadbalance/events", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/config/profile/export", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/config/profile/import{/preview}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "GET", Path: "/api/config/vendors/export", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/config/vendors/import{/preview}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/models/connections/batch", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/models/{model_config_id}/connections/health-check-preview", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
			{Method: "POST", Path: "/api/connections/{connection_id}/health-check", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/server.go: managementRouteRules", Classified: true},
		},
		Resources: []ResourceInventoryEntry{
			{Family: "DB", Name: "management pool", Priority: PriorityManagement, Location: "internal/platform/http/server.go", Functions: []string{"newDatabasePool(settings.DatabaseURL, settings.ManagementDatabaseBudget(), \"management\")"}, CurrentBehavior: "Management API services, realtime, runtime cache refresh, and dashboard management traffic currently share the management pgx pool.", Classified: true},
			{Family: "DB", Name: "runtime execution pool", Priority: PriorityProxy, Location: "internal/platform/http/server.go; internal/httpapi/runtime/service.go", Functions: []string{"newDatabasePool(..., \"runtime execution\")", "newRuntimeServicePool(..., \"execution\")"}, CurrentBehavior: "Runtime proxy execution and best-effort runtime feedback currently use execution capacity.", Classified: true},
			{Family: "DB", Name: "runtime telemetry pool", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/platform/http/server.go; internal/httpapi/runtime/service.go", Functions: []string{"newDatabasePool(..., \"runtime telemetry\")", "newRuntimeServicePool(..., \"telemetry\")"}, CurrentBehavior: "Runtime telemetry outbox enqueue and materialization use telemetry capacity.", Classified: true},
			{Family: "worker", Name: "runtime telemetry outbox", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/httpapi/runtime/telemetry_outbox.go", Functions: []string{"runtimeTelemetryOutbox.Enqueue", "runtimeTelemetryOutbox.worker"}, CurrentBehavior: "Required-durable runtime activity is enqueued then materialized by a background worker.", Classified: true},
			{Family: "worker", Name: "async dashboard publisher", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/httpapi/realtime/async_publisher.go", Functions: []string{"AsyncDashboardPublisher.Enqueue", "AsyncDashboardPublisher.worker"}, CurrentBehavior: "Dashboard update work is queued and published asynchronously with bounded worker state.", Classified: true},
			{Family: "worker", Name: "runtime shared-cache refresh", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/httpapi/runtime/cache.go", Functions: []string{"SharedCache.RefreshNow", "SharedCache.ScheduleRefresh", "SharedCache.runRefreshWorker"}, CurrentBehavior: "Runtime planning/auth snapshots refresh immediately or through a queued refresh worker that currently uses the management pool.", Classified: true},
			{Family: "worker", Name: "proxy-key usage writer", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassLow, Location: "internal/httpapi/management/auth/proxy_key_usage_writer.go", Functions: []string{"proxyKeyUsageWriter.Record", "proxyKeyUsageWriter.run"}, CurrentBehavior: "Proxy-key last-used updates are buffered and flushed asynchronously with requeue semantics.", Classified: true},
			{Family: "side effect", Name: "runtime feedback", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassLow, Location: "internal/httpapi/runtime/runtime.go", Functions: []string{"runtimeFeedbackContext", "recordRuntimeProbeEligible", "recordRuntimeSuccess", "recordRuntimeFailoverHTTPFailure", "recordRuntimeTransportFailure"}, CurrentBehavior: "Feedback writes use detached bounded contexts and are best-effort/lossy under pressure.", Classified: true},
			{Family: "side effect", Name: "inline SMTP auth email", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Location: "internal/platform/email/mailer.go; internal/httpapi/management/auth/routes.go", Functions: []string{"SMTPMailer.sendSMTP", "Service.requestPasswordReset", "Service.verifyEmailOTP"}, CurrentBehavior: "Auth email delivery is currently synchronous on management auth request paths and remains inventoried only in Phase 0.", Classified: true},
			{Family: "side effect", Name: "runtime cache invalidation middleware", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/platform/http/runtime_cache_invalidation.go", Functions: []string{"classifyRuntimeCacheInvalidation", "runtimeCacheInvalidationAction.apply"}, CurrentBehavior: "Post-response invalidation refreshes or schedules runtime cache updates and cannot remain authoritative in later phases.", Classified: true},
			{Family: "cache", Name: "runtime shared-cache readers", Priority: PriorityProxy, Location: "internal/httpapi/runtime/cache.go; internal/httpapi/management/auth/runtime_cache.go", Functions: []string{"LoadPublishedActiveProfile", "LoadPublishedPlanningSnapshot", "LoadRuntimeAuthSettings", "LoadRuntimeProxyKeyRecord", "LoadRuntimeProxyKeyDecision"}, CurrentBehavior: "Runtime proxy/auth reads use published in-memory snapshots that need future freshness-aware APIs.", Classified: true},
		},
	}
}

func (i Inventory) UnclassifiedCount() int {
	count := 0
	for _, route := range i.Routes {
		if !route.Classified {
			count++
		}
	}
	for _, resource := range i.Resources {
		if !resource.Classified {
			count++
		}
	}
	return count
}

func WriteMarkdownInventory(w io.Writer, inventory Inventory) error {
	if _, err := fmt.Fprintln(w, "# Prism Priority Inventory Phase 0"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "## HTTP Routes"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| Method | Path | Priority | Tier | Source | Status |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| --- | --- | --- | --- | --- | --- |"); err != nil {
		return err
	}

	routes := append([]RouteInventoryEntry(nil), inventory.Routes...)
	sort.SliceStable(routes, func(left int, right int) bool {
		if routes[left].Path == routes[right].Path {
			return routes[left].Method < routes[right].Method
		}
		return routes[left].Path < routes[right].Path
	})
	for _, route := range routes {
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s |\n", markdownTableCell(route.Method), markdownCodeOrDash(route.Path), markdownCodeOrDash(string(route.Priority)), markdownCodeOrDash(string(route.ManagementTier)), markdownCodeOrDash(route.Source), markdownTableCell(classificationStatus(route.Classified))); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "## Resource Families"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| Family | Name | Priority | Subclass | Location | Functions | Current behavior | Status |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| --- | --- | --- | --- | --- | --- | --- | --- |"); err != nil {
		return err
	}

	resources := append([]ResourceInventoryEntry(nil), inventory.Resources...)
	sort.SliceStable(resources, func(left int, right int) bool {
		if resources[left].Family == resources[right].Family {
			return resources[left].Name < resources[right].Name
		}
		return resources[left].Family < resources[right].Family
	})
	for _, resource := range resources {
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", markdownTableCell(resource.Family), markdownTableCell(resource.Name), markdownCodeOrDash(string(resource.Priority)), markdownCodeOrDash(backgroundOrTier(resource)), markdownCodeOrDash(resource.Location), strings.Join(markdownCodeList(resource.Functions), ", "), markdownTableCell(resource.CurrentBehavior), markdownTableCell(classificationStatus(resource.Classified))); err != nil {
			return err
		}
	}
	return nil
}

func markdownCodeOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return "`" + markdownTableCell(value) + "`"
}

func markdownTableCell(value string) string {
	escaped := strings.ReplaceAll(value, "|", `\|`)
	escaped = strings.ReplaceAll(escaped, "\r\n", " ")
	escaped = strings.ReplaceAll(escaped, "\n", " ")
	escaped = strings.ReplaceAll(escaped, "\r", " ")
	return escaped
}

func markdownCodeList(values []string) []string {
	coded := make([]string, 0, len(values))
	for _, value := range values {
		coded = append(coded, markdownCodeOrDash(value))
	}
	return coded
}

func backgroundOrTier(resource ResourceInventoryEntry) string {
	if resource.BackgroundSubclass != "" {
		return string(resource.BackgroundSubclass)
	}
	return string(resource.ManagementTier)
}

func classificationStatus(classified bool) string {
	if classified {
		return "classified"
	}
	return "unclassified"
}
