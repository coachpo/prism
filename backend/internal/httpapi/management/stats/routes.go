package stats

// The stats route table is the management HTTP boundary for retained
// observability reads. It registers every public path while keeping request
// parsing in the query modules and persistence in the domain stats package.
// Static export and filter paths precede parameterized request paths so the
// mounted API remains deterministic.
//
// Dashboard routes expose the aggregate snapshot plus its activity feed.
// Request routes expose attempts, ingress chains, details, and CSV export.
// Aggregate routes cover summary, metrics, throughput, spending, and usage.
// Observe routes issue signed contexts. Endpoint routes expose drilldowns.
// Cost routes expose segment pages and symbol pages.
//
// This file owns dispatch only. It does not decide time ranges, coerce query
// values, open transactions, write response bodies, or rebuild snapshots.
// Those responsibilities stay in the handler, query, error, domain, and
// snapshot owners. Keeping dispatch narrow makes additions visible without a
// second implementation of the statistics contract.
//
// The profile header remains a compatibility input to the shared Service seam;
// management resolution remains pinned to the Default profile. Runtime proxy
// traffic never reaches this router, so provider behavior does not belong here.
// Export remains server-side and cursor ownership stays in the domain package.
// Filter-option routes retain their own bounded query grammar. Exact request
// detail routes retain their private-cache headers. No route in this table
// changes those response policies; each handler owns its boundary explicitly.
// The resulting table is intentionally boring: its value is the complete
// mounted contract rather than a layer of indirection around handlers.
// Read-model names remain in the domain package and never become route keys.
// This keeps HTTP assembly independent from SQL projection details.
// The route file therefore remains the single navigation map for this package.
// It is reviewed alongside the management mount rather than beside SQL code.
// There is no catch-all route in the stats surface.
//
import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/stats", func(router chi.Router) {
		router.Get("/dashboard", s.handleDashboardStats)
		router.Get("/dashboard/recent-activity", s.handleDashboardRecentActivity)
		router.Get("/requests", s.handleListRequestLogs)
		router.Get("/requests/export", s.handleExportRequestLogs)
		router.Get("/request-filter-options/proxy-api-keys", s.handleProxyAPIKeyFilterOptions)
		router.Get("/requests/{request_id}", s.handleGetRequestLog)
		router.Get("/summary", s.handleStatsSummary)
		router.Post("/models/metrics", s.handleModelMetrics)
		router.Get("/connection-success-rates", s.handleConnectionSuccessRates)
		router.Get("/throughput", s.handleThroughput)
		router.Get("/spending", s.handleSpending)
		router.Get("/usage-snapshot", s.handleUsageSnapshot)
		router.Get("/query-context", s.handleQueryContext)
		router.Get("/usage-summary", s.handleUsageSummary)
		router.Get("/usage-series", s.handleUsageSeries)
		router.Get("/usage-errors", s.handleUsageErrors)
		router.Get("/dashboard/now", s.handleDashboardNow)
		router.Get("/observe-activity", s.handleObserveActivity)
		router.Get("/endpoints/{endpoint_id}/models", s.handleEndpointModelStatistics)
		router.Get("/endpoints/{endpoint_id}/terminal-targets", s.handleEndpointTerminalTargetStatistics)
		router.Get("/cost-segments", s.handleCostSegments)
		router.Get("/cost-segments/{segment_key}/symbols", s.handleCostSegmentSymbols)
	})
}

func requestLogHasSignedCohortSelector(r *http.Request) bool {
	return len(collectRepeatedCommaValues(r, "final_result")) > 0 ||
		len(collectRepeatedCommaValues(r, "outcome_detail")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_status_code")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_stream_outcome")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_stream_error_kind")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_target_model_id")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_endpoint_id")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_terminal_target_id")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_pricing_status")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_unpriced_reason")) > 0 ||
		len(collectRepeatedCommaValues(r, "reporting_currency_epoch")) > 0 ||
		len(collectRepeatedCommaValues(r, "final_exclude")) > 0 ||
		len(collectRepeatedCommaValues(r, "attempt_trigger")) > 0 ||
		len(collectRepeatedCommaValues(r, "attempt_result")) > 0
}
