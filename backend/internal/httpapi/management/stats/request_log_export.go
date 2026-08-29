package stats

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
)

// handleExportRequestLogs streams the full filtered CSV export (Requests SPEC
// §6.8). The snapshot, preflight count, spool, and digest happen before any
// response body bytes are sent; typed rejections never produce a partial file.
func (s *Service) handleExportRequestLogs(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	// Reject pagination keys up front.
	if strings.TrimSpace(r.URL.Query().Get("limit")) != "" || strings.TrimSpace(r.URL.Query().Get("offset")) != "" ||
		strings.TrimSpace(r.URL.Query().Get("cursor")) != "" || strings.TrimSpace(r.URL.Query().Get("chain_cursor")) != "" ||
		strings.TrimSpace(r.URL.Query().Get("row_cursor")) != "" || strings.TrimSpace(r.URL.Query().Get("chain_limit")) != "" ||
		strings.TrimSpace(r.URL.Query().Get("chain_row_limit")) != "" || strings.TrimSpace(r.URL.Query().Get("anchor_request_log_id")) != "" {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "export_pagination_unsupported")
		return
	}
	// A bounded export must state its range before the handler resolves the
	// interactive default (24h). Exact ingress selection is the sole
	// range-free exception; otherwise a preset or both explicit bounds are
	// required so a download can never silently become a browser-window dump.
	query := r.URL.Query()
	hasQueryContext := strings.TrimSpace(query.Get("query_context")) != ""
	if requestLogHasSignedCohortSelector(r) && !hasQueryContext {
		writeDomainError(w, r, s.corsSnapshot(), &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_required", Detail: "query_context is required with final filters"})
		return
	}
	if !requestLogExportHasBoundedRange(r) {
		writeDomainError(w, r, s.corsSnapshot(), &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "export_range_required", Detail: "Export requires an explicit time range."})
		return
	}
	if strings.TrimSpace(r.Header.Get("Accept")) != "text/csv" {
		// Allow Accept: text/csv or */*; missing Accept still proceeds (browsers
		// trigger downloads via the endpoint URL).
	}
	result, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (statsdomain.ExportResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ExportResult{}, err
		}
		view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
		if view == "" {
			view = "ingress_chains"
		}
		if view != "ingress_chains" && view != "attempts" {
			return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "view must be ingress_chains or attempts"}
		}
		var signedRequestBounds *statsdomain.QueryBounds
		rawQueryContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
		if rawQueryContext != "" || requestLogHasSignedCohortSelector(r) {
			if rawQueryContext == "" {
				return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_required", Detail: "query_context is required with final filters"}
			}
			token, _, resolveErr := s.resolveQueryContextFromRequest(r)
			if resolveErr != nil {
				return statsdomain.ExportResult{}, resolveErr
			}
			if token.ProfileID != profile.ID {
				return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_scope_mismatch", Detail: "query_context scope mismatch"}
			}
			requestBounds, boundsErr := statsdomain.QueryBoundsForDomain(token, "request_logs")
			if boundsErr != nil {
				return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_query_context", Detail: "invalid query_context"}
			}
			signedRequestBounds = &requestBounds
		}
		if view == "ingress_chains" {
			chainParams, parseErr := parseChainQueryParams(r, profile.ID)
			if parseErr != nil {
				return statsdomain.ExportResult{}, parseErr
			}
			if signedRequestBounds != nil {
				fromTime := signedRequestBounds.UsageFrom.UTC()
				toTime := signedRequestBounds.UsageTo.UTC()
				chainParams.CoveragePreset = "custom"
				chainParams.CoverageRequestedFrom = &fromTime
				chainParams.CoverageRequestedTo = &toTime
				chainParams.FromTime = &fromTime
				chainParams.ToTime = &toTime
			}
			chainParams.CoverageReferenceNow = s.nowUTC()
			exportParams := statsdomain.ExportParams{ChainQueryParams: &chainParams, View: view}
			return statsdomain.ExportCSV(r.Context(), tx, exportParams)
		}
		params, err := parseRequestLogListParams(r, profile.ID, s.observabilitySigningKey(), s.nowUTC())
		if err != nil {
			return statsdomain.ExportResult{}, err
		}
		// Signed final-result selectors bind the export to the same per-domain
		// owner window as the JSON attempt list; browser bounds cannot widen it.
		if signedRequestBounds != nil {
			fromTime := signedRequestBounds.UsageFrom.UTC()
			toTime := signedRequestBounds.UsageTo.UTC()
			params.CoveragePreset = "custom"
			params.CoverageRequestedFrom = &fromTime
			params.CoverageRequestedTo = &toTime
			params.FromTime = &fromTime
			params.ToTime = &toTime
		}
		exportParams := statsdomain.ExportParams{RequestLogListParams: params, View: view}
		return statsdomain.ExportCSV(r.Context(), tx, exportParams)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	// Stream the exact verified spool bytes.
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"prism-requests-%s.csv\"", time.Now().UTC().Format("20060102-150405")))
	w.Header().Set("X-Prism-Export-Row-Count", fmt.Sprintf("%d", result.RowCount))
	w.Header().Set("X-Prism-Export-View", result.View)
	w.Header().Set("X-Prism-Export-Coverage", "retained")
	w.Header().Set("X-Prism-Metric-Scope", result.Caliber.Scope)
	w.Header().Set("X-Prism-Metric-Grain", result.Caliber.Grain)
	w.Header().Set("X-Prism-Dataset", strings.Join(result.Caliber.Datasets, ","))
	w.Header().Set("X-Prism-Coverage-Complete", fmt.Sprintf("%t", result.Coverage.Complete))
	w.Header().Set("Digest", "sha-256="+result.DigestSHA256)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.Content)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(result.Content); err != nil {
		slog.Warn("export stream interrupted", "error", err)
	}
}

func requestLogExportHasBoundedRange(r *http.Request) bool {
	query := r.URL.Query()
	hasExactIngress := strings.TrimSpace(query.Get("ingress_request_id")) != ""
	hasPreset := strings.TrimSpace(query.Get("time_range")) != ""
	hasFrom := strings.TrimSpace(query.Get("from_time")) != ""
	hasTo := strings.TrimSpace(query.Get("to_time")) != ""
	hasQueryContext := strings.TrimSpace(query.Get("query_context")) != ""
	return hasExactIngress || hasPreset || (hasFrom && hasTo) || hasQueryContext
}
