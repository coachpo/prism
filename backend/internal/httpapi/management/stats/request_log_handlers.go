package stats

import (
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var requestLogIDPattern = regexp.MustCompile(`^[0-9]+$`)

func (s *Service) handleListRequestLogs(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (any, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
		if view != "" && view != "ingress_chains" && view != "attempts" {
			return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "view must be ingress_chains or attempts"}
		}
		var signedRequestBounds *statsdomain.QueryBounds
		rawQueryContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
		// Observe final-selector deep links bind a signed query context in both
		// views. Ordinary triage selectors deliberately stay outside this gate.
		if rawQueryContext != "" || requestLogHasSignedCohortSelector(r) {
			if rawQueryContext == "" {
				return nil, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_required", Detail: "query_context is required with final filters"}
			}
			token, _, err := s.resolveQueryContextFromRequest(r)
			if err != nil {
				return nil, err
			}
			if token.ProfileID != profile.ID {
				return nil, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_scope_mismatch", Detail: "query_context scope mismatch"}
			}
			requestBounds, boundsErr := statsdomain.QueryBoundsForDomain(token, "request_logs")
			if boundsErr != nil {
				return nil, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_query_context", Detail: "invalid query_context"}
			}
			signedRequestBounds = &requestBounds
		}
		if view == "ingress_chains" {
			params, parseErr := parseChainQueryParams(r, profile.ID)
			if parseErr != nil {
				return nil, parseErr
			}
			if signedRequestBounds != nil {
				fromTime := signedRequestBounds.UsageFrom.UTC()
				toTime := signedRequestBounds.UsageTo.UTC()
				params.CoveragePreset = "custom"
				params.CoverageRequestedFrom = &fromTime
				params.CoverageRequestedTo = &toTime
				params.FromTime = &fromTime
				params.ToTime = &toTime
			}
			params.CoverageReferenceNow = s.nowUTC()
			return statsdomain.ListIngressChains(r.Context(), tx, params)
		}
		params, err := parseRequestLogListParams(r, profile.ID, s.observabilitySigningKey(), s.nowUTC())
		if err != nil {
			return nil, err
		}

		// The signed context is authoritative for final-result deep links. Do
		// not trust browser-supplied from_time/to_time values to widen or shift
		// that cohort; the owner resolves the signed bounds against its actual
		// coverage projection in the same transaction.
		if signedRequestBounds != nil {
			fromTime := signedRequestBounds.UsageFrom.UTC()
			toTime := signedRequestBounds.UsageTo.UTC()
			params.CoveragePreset = "custom"
			params.CoverageRequestedFrom = &fromTime
			params.CoverageRequestedTo = &toTime
			params.FromTime = &fromTime
			params.ToTime = &toTime
		}
		return statsdomain.ListRequestLogs(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleProxyAPIKeyFilterOptions(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "q", "from_time", "to_time", "limit", "cursor", "selected_id"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats proxy API key filter options", func(tx pgx.Tx) (statsdomain.ProxyAPIKeyFilterOptionsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ProxyAPIKeyFilterOptionsResponse{}, err
		}
		params, err := parseProxyAPIKeyFilterOptionsParams(r, profile.ID)
		if err != nil {
			return statsdomain.ProxyAPIKeyFilterOptionsResponse{}, err
		}
		return statsdomain.ListProxyAPIKeyFilterOptions(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func parseProxyAPIKeyFilterOptionsParams(r *http.Request, profileID int) (statsdomain.ProxyAPIKeyFilterOptionsParams, error) {
	params := statsdomain.ProxyAPIKeyFilterOptionsParams{ProfileID: profileID}
	if raw := strings.TrimSpace(r.URL.Query().Get("q")); raw != "" {
		params.Query = &raw
	}
	var err error
	params.FromTime, err = parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.ProxyAPIKeyFilterOptionsParams{}, err
	}
	params.ToTime, err = parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.ProxyAPIKeyFilterOptionsParams{}, err
	}
	params.Limit, err = parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		return statsdomain.ProxyAPIKeyFilterOptionsParams{}, err
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		params.Cursor = &cursor
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("selected_id")); raw != "" {
		selectedID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || selectedID <= 0 || selectedID > math.MaxInt32 {
			return statsdomain.ProxyAPIKeyFilterOptionsParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid selected_id"}
		}
		selected := int(selectedID)
		params.SelectedID = &selected
	}
	return params, nil
}

func (s *Service) handleGetRequestLog(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.SetPrivateNoStoreHeaders(w)
	rawRequestLogID := strings.TrimSpace(chi.URLParam(r, "request_id"))
	if rawRequestLogID == "" || !requestLogIDPattern.MatchString(rawRequestLogID) {
		writeDomainError(w, r, s.corsSnapshot(), &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "invalid_request_id", Detail: "request_id must be a positive decimal string"})
		return
	}
	requestLogID, err := strconv.ParseInt(rawRequestLogID, 10, 64)
	if err != nil || requestLogID <= 0 {
		writeDomainError(w, r, s.corsSnapshot(), &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "invalid_request_id", Detail: "request_id must be a positive decimal string"})
		return
	}
	type detailResult struct {
		response *statsdomain.RequestLogDetailResponse
		found    bool
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (detailResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return detailResult{}, err
		}
		response, found, err := statsdomain.GetRequestLogDetail(r.Context(), tx, profile.ID, requestLogID)
		if err != nil {
			return detailResult{}, err
		}
		if found && response != nil {
			source, sourceErr := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, "request_logs", s.nowUTC())
			if sourceErr != nil {
				return detailResult{}, sourceErr
			}
			if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
				return detailResult{}, &statsdomain.HTTPError{StatusCode: http.StatusServiceUnavailable, Code: "request_log_purge_in_progress", Detail: "request logs are temporarily unavailable while retention cleanup is publishing"}
			}
			floor := source.ConfiguredCutoff
			if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
				floor = source.PublishedFloor
			}
			if floor != nil && response.Summary.CreatedAt.Before(*floor) {
				found = false
				response = nil
			}
		}
		return detailResult{response: response, found: found}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !result.found {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Request log not found")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, result.response)
}
