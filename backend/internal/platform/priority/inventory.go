package priority

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	ClassificationClassified   = "classified"
	ClassificationUnclassified = "unclassified"
)

var ErrInventoryValidation = errors.New("priority inventory validation failed")

type RouteInventoryEntry struct {
	Name           string
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

type JobInventoryEntry struct {
	Name               string
	Priority           LogicalPriority
	ManagementTier     ManagementTier
	BackgroundSubclass BackgroundSubclass
	Source             string
	Functions          []string
	CurrentBehavior    string
	Classified         bool
}

type Inventory struct {
	Routes    []RouteInventoryEntry
	Resources []ResourceInventoryEntry
	Jobs      []JobInventoryEntry
}

type InventoryValidationProblem struct {
	Kind   string
	Name   string
	Source string
	Reason string
}

type InventoryValidationError struct {
	Problems []InventoryValidationProblem
}

func (e InventoryValidationError) Error() string {
	if len(e.Problems) == 0 {
		return ErrInventoryValidation.Error()
	}
	return fmt.Sprintf("%s: %d problem(s), first %s %q: %s", ErrInventoryValidation, len(e.Problems), e.Problems[0].Kind, e.Problems[0].Name, e.Problems[0].Reason)
}

func (e InventoryValidationError) Unwrap() error {
	return ErrInventoryValidation
}

func DefaultInventory() Inventory {
	return Inventory{
		Routes: []RouteInventoryEntry{
			{Name: "runtime OpenAI root", Method: "ANY", Path: "/v1", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Name: "runtime OpenAI wildcard", Method: "ANY", Path: "/v1/*", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Name: "runtime Gemini root", Method: "ANY", Path: "/v1beta", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Name: "runtime Gemini wildcard", Method: "ANY", Path: "/v1beta/*", Priority: PriorityProxy, Source: "internal/platform/http/server.go: mountRuntimeBranch", Classified: true},
			{Name: "auth status", Method: "GET", Path: "/api/auth/status", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "auth public bootstrap", Method: "GET", Path: "/api/auth/public-bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "auth login", Method: "POST", Path: "/api/auth/login", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "auth logout", Method: "POST", Path: "/api/auth/logout", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "auth refresh", Method: "POST", Path: "/api/auth/refresh", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "auth session", Method: "GET", Path: "/api/auth/session", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "password reset request", Method: "POST", Path: "/api/auth/password-reset/request", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "password reset confirm", Method: "POST", Path: "/api/auth/password-reset/confirm", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "profiles bootstrap", Method: "GET", Path: "/api/profiles/bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "profiles active", Method: "GET", Path: "/api/profiles/active", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "profile activate", Method: "POST", Path: "/api/profiles/{profile_id}/activate", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "bootstrap config read", Method: "GET", Path: "/api/config/bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "bootstrap config validate", Method: "POST", Path: "/api/config/bootstrap/validate", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "bootstrap config write", Method: "PUT", Path: "/api/config/bootstrap", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "explicit M2 management route specs", Method: "ANY", Path: "/api/{auth-settings|config|rules|connections|endpoints|loadbalance-strategies|models|profiles|settings|vendors} explicit route specs", Priority: PriorityManagement, ManagementTier: ManagementTierM2, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "realtime websocket", Method: "GET", Path: "/api/realtime/ws", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "stats reads", Method: "GET", Path: "/api/stats/*", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "stats broad deletes", Method: "DELETE", Path: "/api/stats/requests|statistics", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "audit reads", Method: "GET", Path: "/api/audit/logs{/{log_id}}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "audit deletes", Method: "DELETE", Path: "/api/audit/logs", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "loadbalance reads", Method: "GET", Path: "/api/loadbalance/current-state|events{/{event_id}}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "loadbalance deletes", Method: "DELETE", Path: "/api/loadbalance/events", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "profile config export", Method: "GET", Path: "/api/config/profile/export", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "profile config import", Method: "POST", Path: "/api/config/profile/import{/preview}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "vendor config export", Method: "GET", Path: "/api/config/vendors/export", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "vendor config import", Method: "POST", Path: "/api/config/vendors/import{/preview}", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "model connection batch", Method: "POST", Path: "/api/models/connections/batch", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "model health-check preview", Method: "POST", Path: "/api/models/{model_config_id}/connections/health-check-preview", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
			{Name: "connection health-check", Method: "POST", Path: "/api/connections/{connection_id}/health-check", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Source: "internal/platform/http/admission.go: managementRouteSpecs", Classified: true},
		},
		Resources: []ResourceInventoryEntry{
			{Family: "DB", Name: "management pool", Priority: PriorityManagement, Location: "internal/platform/db/pools.go; internal/platform/http/server.go", Functions: []string{"OpenDatabasePools(...).Management"}, CurrentBehavior: "Management API services use the named management Postgres lane only.", Classified: true},
			{Family: "DB", Name: "runtime execution pool", Priority: PriorityProxy, Location: "internal/platform/db/pools.go; internal/platform/http/server.go", Functions: []string{"OpenDatabasePools(...).RuntimeExecution"}, CurrentBehavior: "Runtime proxy execution uses the named runtime_execution Postgres lane.", Classified: true},
			{Family: "DB", Name: "runtime telemetry pool", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/platform/db/pools.go; internal/platform/http/server.go; internal/httpapi/runtime/telemetry_outbox.go", Functions: []string{"OpenDatabasePools(...).RuntimeTelemetry"}, CurrentBehavior: "Runtime telemetry outbox enqueue and materialization use the named runtime_telemetry Postgres lane.", Classified: true},
			{Family: "DB", Name: "runtime feedback pool", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassLow, Location: "internal/platform/db/pools.go; internal/platform/http/server.go; internal/httpapi/runtime/runtime.go", Functions: []string{"OpenDatabasePools(...).RuntimeFeedback", "runtimeFeedbackStore"}, CurrentBehavior: "Runtime feedback writes use the named runtime_feedback Postgres lane independent of runtime execution and telemetry.", Classified: true},
			{Family: "DB", Name: "realtime pool", Priority: PriorityManagement, ManagementTier: ManagementTierM3, Location: "internal/platform/db/pools.go; internal/platform/http/server.go; internal/httpapi/realtime", Functions: []string{"OpenDatabasePools(...).Realtime"}, CurrentBehavior: "Realtime websocket subscription and dashboard fanout storage use the named realtime Postgres lane.", Classified: true},
			{Family: "DB", Name: "cache refresh pool", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/platform/db/pools.go; internal/platform/http/server.go; internal/httpapi/runtime/cache.go", Functions: []string{"OpenDatabasePools(...).CacheRefresh"}, CurrentBehavior: "Runtime shared-cache refresh uses the named cache_refresh Postgres lane.", Classified: true},
			{Family: "DB", Name: "background jobs pool", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/platform/db/pools.go; internal/platform/http/server.go; internal/httpapi/management/auth/proxy_key_usage_writer.go", Functions: []string{"OpenDatabasePools(...).BackgroundJobs"}, CurrentBehavior: "Background DB workers use the named background_jobs Postgres lane.", Classified: true},
			{Family: "worker", Name: "runtime telemetry side effects", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/httpapi/runtime/runtime_side_effects.go", Functions: []string{"RuntimeSideEffectManager.SubmitRuntimeActivity", "RuntimeSideEffectManager.handleRuntimeActivity"}, CurrentBehavior: "Required-durable runtime activity is accepted by the side-effect manager and durably enqueued to the telemetry outbox by scheduler-owned work.", Classified: true},
			{Family: "worker", Name: "runtime telemetry outbox", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassLow, Location: "internal/httpapi/runtime/telemetry_outbox.go", Functions: []string{"runtimeTelemetryOutbox.Enqueue", "runtimeTelemetryOutbox.RegisterBackgroundWorker", "runtimeTelemetryOutbox.handleScheduledTelemetry"}, CurrentBehavior: "Committed runtime activity telemetry outbox rows are materialized by a scheduler-owned background worker.", Classified: true},
			{Family: "worker", Name: "async dashboard publisher", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/httpapi/realtime/async_publisher.go", Functions: []string{"AsyncDashboardPublisher.PublishDashboardUpdate", "AsyncDashboardPublisher.RegisterBackgroundWorker", "AsyncDashboardPublisher.handleScheduledPublish"}, CurrentBehavior: "Dashboard update work is submitted through the scheduler with latest-wins coalescing.", Classified: true},
			{Family: "worker", Name: "runtime shared-cache refresh", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/httpapi/runtime/cache.go", Functions: []string{"SharedCache.RefreshNow", "SharedCache.ScheduleRefresh", "SharedCache.RegisterBackgroundWorker", "SharedCache.handleScheduledRefresh"}, CurrentBehavior: "Runtime planning/auth snapshots refresh immediately or through a scheduler-owned coalesced refresh worker using the cache_refresh lane.", Classified: true},
			{Family: "worker", Name: "proxy-key usage writer", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/httpapi/management/auth/proxy_key_usage_writer.go", Functions: []string{"proxyKeyUsageWriter.Enqueue", "proxyKeyUsageWriter.RegisterBackgroundWorker", "proxyKeyUsageWriter.handleScheduledFlush"}, CurrentBehavior: "Proxy-key last-used updates are merged in memory and flushed by a scheduler-owned background worker with flush drain semantics.", Classified: true},
			{Family: "worker", Name: "management side-effect outbox", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/platform/managementsideeffects/outbox.go", Functions: []string{"Dispatcher.Wake", "Dispatcher.RegisterBackgroundWorker", "Dispatcher.handleScheduledDispatch"}, CurrentBehavior: "Committed management side-effect outbox rows are claimed, retried, and finalized by a scheduler-owned background worker using the background_jobs lane.", Classified: true},
			{Family: "side effect", Name: "runtime feedback", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/httpapi/runtime/runtime.go; internal/httpapi/runtime/feedback_pipeline.go", Functions: []string{"runtimeFeedbackPipeline.TryEnqueue", "runtimeFeedbackStore", "recordRuntimeProbeEligible", "recordRuntimeSuccess", "recordRuntimeFailoverHTTPFailure", "recordRuntimeTransportFailure"}, CurrentBehavior: "Proxy feedback paths construct events and submit bounded scheduler-owned writes through the isolated runtime_feedback lane; overload is best-effort/lossy under pressure.", Classified: true},
			{Family: "side effect", Name: "management dashboard invalidation outbox", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/httpapi/management/stats/service.go; internal/platform/managementsideeffects/outbox.go", Functions: []string{"managementsideeffects.InsertTx", "managementsideeffects.AfterCommit", "Service.handleDashboardSnapshotInvalidation"}, CurrentBehavior: "Stats delete mutations commit dashboard invalidation intents in management transactions and wake the scheduler-owned dispatcher only after commit.", Classified: true},
			{Family: "worker", Name: "email outbox worker", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Location: "internal/platform/email/outbox/outbox.go", Functions: []string{"Store.EnqueueTx", "Store.RegisterBackgroundWorker", "Store.handleScheduledSend"}, CurrentBehavior: "Committed auth email outbox rows are claimed, retried, sent, and dead-lettered by a scheduler-owned background worker using the background_jobs lane.", Classified: true},
			{Family: "side effect", Name: "auth email outbox enqueue", Priority: PriorityManagement, ManagementTier: ManagementTierM1, Location: "internal/httpapi/management/auth/routes.go; internal/platform/email/outbox/outbox.go", Functions: []string{"Service.enqueueAuthEmail", "outbox.InsertTx"}, CurrentBehavior: "Auth email request paths enqueue durable email jobs in the same management transaction as OTP or password reset state without calling SMTP.", Classified: true},
			{Family: "side effect", Name: "runtime cache invalidation middleware", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Location: "internal/platform/http/runtime_cache_invalidation.go", Functions: []string{"classifyRuntimeCacheInvalidation", "runtimeCacheInvalidationAction.apply"}, CurrentBehavior: "Post-response invalidation now attaches transaction-scoped generation bumps before commit and schedules cache warming only after successful mutations.", Classified: true},
			{Family: "cache", Name: "runtime shared-cache readers", Priority: PriorityProxy, Location: "internal/httpapi/runtime/cache.go; internal/httpapi/management/auth/runtime_cache.go", Functions: []string{"LoadFreshActiveRuntimePlan", "LoadFreshRuntimeAuthSettings", "LoadFreshRuntimeProxyKeyRecord", "LoadFreshRuntimeProxyKeyDecision"}, CurrentBehavior: "Runtime proxy/auth reads validate durable generation vectors before returning snapshots and synchronously refresh or reject stale state.", Classified: true},
		},
		Jobs: []JobInventoryEntry{
			{Name: "runtime telemetry outbox", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassLow, Source: "internal/httpapi/runtime/telemetry_outbox.go", Functions: []string{"runtimeTelemetryOutbox.handleScheduledTelemetry"}, CurrentBehavior: "Materializes accepted runtime activity telemetry from the durable outbox through the scheduler.", Classified: true},
			{Name: "runtime side effects", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Source: "internal/httpapi/runtime/runtime_side_effects.go", Functions: []string{"RuntimeSideEffectManager.handleRuntimeActivity"}, CurrentBehavior: "Commits accepted runtime activity intents to the durable telemetry outbox through the scheduler.", Classified: true},
			{Name: "runtime feedback pipeline", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Source: "internal/httpapi/runtime/feedback_pipeline.go", Functions: []string{"runtimeFeedbackPipeline.handleScheduledFeedback"}, CurrentBehavior: "Persists accepted lossy runtime feedback events through the scheduler and the runtime_feedback lane.", Classified: true},
			{Name: "async dashboard publisher", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassHigh, Source: "internal/httpapi/realtime/async_publisher.go", Functions: []string{"AsyncDashboardPublisher.handleScheduledPublish"}, CurrentBehavior: "Publishes queued dashboard updates through the scheduler.", Classified: true},
			{Name: "runtime shared-cache refresh", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Source: "internal/httpapi/runtime/cache.go", Functions: []string{"SharedCache.handleScheduledRefresh"}, CurrentBehavior: "Refreshes runtime planning/auth snapshots outside the request path through the scheduler.", Classified: true},
			{Name: "proxy-key usage writer", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Source: "internal/httpapi/management/auth/proxy_key_usage_writer.go", Functions: []string{"proxyKeyUsageWriter.handleScheduledFlush"}, CurrentBehavior: "Flushes buffered proxy-key last-used updates through the scheduler.", Classified: true},
			{Name: "management side-effect outbox", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Source: "internal/platform/managementsideeffects/outbox.go", Functions: []string{"Dispatcher.handleScheduledDispatch"}, CurrentBehavior: "Claims and processes committed management side-effect outbox rows through the scheduler and background_jobs lane.", Classified: true},
			{Name: "email outbox worker", Priority: PriorityBackground, BackgroundSubclass: BackgroundSubclassNormal, Source: "internal/platform/email/outbox/outbox.go", Functions: []string{"Store.handleScheduledSend"}, CurrentBehavior: "Claims and sends committed auth email outbox rows through the scheduler and background_jobs lane.", Classified: true},
		},
	}
}

func ValidateInventory(inventory Inventory) error {
	problems := inventory.ValidationProblems()
	if len(problems) == 0 {
		return nil
	}
	return InventoryValidationError{Problems: problems}
}

func (i Inventory) ValidationProblems() []InventoryValidationProblem {
	var problems []InventoryValidationProblem
	for _, route := range i.Routes {
		problems = append(problems, validateNamedMetadata("route", route.Name, route.Source, route.Classified, route.Metadata())...)
		if strings.TrimSpace(route.Method) == "" {
			problems = append(problems, inventoryProblem("route", route.Name, route.Source, "missing method"))
		}
		if strings.TrimSpace(route.Path) == "" {
			problems = append(problems, inventoryProblem("route", route.Name, route.Source, "missing path"))
		}
	}
	for _, resource := range i.Resources {
		problems = append(problems, validateNamedMetadata("resource", resource.Name, resource.Location, resource.Classified, resource.Metadata())...)
		if strings.TrimSpace(resource.Family) == "" {
			problems = append(problems, inventoryProblem("resource", resource.Name, resource.Location, "missing family"))
		}
	}
	for _, job := range i.Jobs {
		problems = append(problems, validateNamedMetadata("job", job.Name, job.Source, job.Classified, job.Metadata())...)
	}
	return problems
}

func validateNamedMetadata(kind string, name string, source string, classified bool, metadata Metadata) []InventoryValidationProblem {
	var problems []InventoryValidationProblem
	if strings.TrimSpace(name) == "" {
		problems = append(problems, inventoryProblem(kind, name, source, "missing name"))
	}
	if strings.TrimSpace(source) == "" {
		problems = append(problems, inventoryProblem(kind, name, source, "missing source"))
	}
	if !classified {
		problems = append(problems, inventoryProblem(kind, name, source, "unclassified"))
	}
	if err := validateInventoryMetadata(metadata); err != nil {
		problems = append(problems, inventoryProblem(kind, name, source, err.Error()))
	}
	return problems
}

func validateInventoryMetadata(metadata Metadata) error {
	switch metadata.Priority {
	case PriorityProxy:
		if metadata.ManagementTier != "" {
			return fmt.Errorf("proxy priority cannot declare management tier %q", metadata.ManagementTier)
		}
		if metadata.BackgroundSubclass != "" {
			return fmt.Errorf("proxy priority cannot declare background subclass %q", metadata.BackgroundSubclass)
		}
		return nil
	case PriorityManagement:
		if metadata.ManagementTier != "" {
			if _, err := ParseManagementTier(string(metadata.ManagementTier)); err != nil {
				return err
			}
		}
		if metadata.BackgroundSubclass != "" {
			return fmt.Errorf("management priority cannot declare background subclass %q", metadata.BackgroundSubclass)
		}
		return nil
	case PriorityBackground:
		if metadata.ManagementTier != "" {
			return fmt.Errorf("background priority cannot declare management tier %q", metadata.ManagementTier)
		}
		if metadata.BackgroundSubclass != "" {
			if _, err := ParseBackgroundSubclass(string(metadata.BackgroundSubclass)); err != nil {
				return err
			}
		}
		return nil
	default:
		return ErrMissingPriority
	}
}

func inventoryProblem(kind string, name string, source string, reason string) InventoryValidationProblem {
	return InventoryValidationProblem{Kind: kind, Name: name, Source: source, Reason: reason}
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
	for _, job := range i.Jobs {
		if !job.Classified {
			count++
		}
	}
	return count
}

func (r RouteInventoryEntry) Metadata() Metadata {
	return Metadata{Priority: r.Priority, ManagementTier: r.ManagementTier}
}

func (r ResourceInventoryEntry) Metadata() Metadata {
	return Metadata{Priority: r.Priority, ManagementTier: r.ManagementTier, BackgroundSubclass: r.BackgroundSubclass}
}

func (j JobInventoryEntry) Metadata() Metadata {
	return Metadata{Priority: j.Priority, ManagementTier: j.ManagementTier, BackgroundSubclass: j.BackgroundSubclass}
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
	if _, err := fmt.Fprintln(w, "## Jobs"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| Name | Priority | Subclass | Source | Functions | Current behavior | Status |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| --- | --- | --- | --- | --- | --- | --- |"); err != nil {
		return err
	}

	jobs := append([]JobInventoryEntry(nil), inventory.Jobs...)
	sort.SliceStable(jobs, func(left int, right int) bool {
		return jobs[left].Name < jobs[right].Name
	})
	for _, job := range jobs {
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s |\n", markdownTableCell(job.Name), markdownCodeOrDash(string(job.Priority)), markdownCodeOrDash(jobBackgroundOrTier(job)), markdownCodeOrDash(job.Source), strings.Join(markdownCodeList(job.Functions), ", "), markdownTableCell(job.CurrentBehavior), markdownTableCell(classificationStatus(job.Classified))); err != nil {
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
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", markdownTableCell(resource.Family), markdownTableCell(resource.Name), markdownCodeOrDash(string(resource.Priority)), markdownCodeOrDash(resourceBackgroundOrTier(resource)), markdownCodeOrDash(resource.Location), strings.Join(markdownCodeList(resource.Functions), ", "), markdownTableCell(resource.CurrentBehavior), markdownTableCell(classificationStatus(resource.Classified))); err != nil {
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

func resourceBackgroundOrTier(resource ResourceInventoryEntry) string {
	if resource.BackgroundSubclass != "" {
		return string(resource.BackgroundSubclass)
	}
	return string(resource.ManagementTier)
}

func jobBackgroundOrTier(job JobInventoryEntry) string {
	if job.BackgroundSubclass != "" {
		return string(job.BackgroundSubclass)
	}
	return string(job.ManagementTier)
}

func classificationStatus(classified bool) string {
	if classified {
		return ClassificationClassified
	}
	return ClassificationUnclassified
}
