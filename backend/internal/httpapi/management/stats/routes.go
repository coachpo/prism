package stats

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
	query := r.URL.Query()
	return query.Get("ingress_final_result") != "" ||
		query.Get("confirmed_failover") != "" ||
		query.Get("final_result") != "" ||
		query.Get("final_model_id") != "" ||
		query.Get("final_endpoint_id") != "" ||
		query.Get("final_terminal_target_id") != "" ||
		query.Get("final_pricing_status") != "" ||
		len(query["final_unpriced_reason"]) > 0 ||
		query.Get("reporting_currency_epoch") != ""
}
