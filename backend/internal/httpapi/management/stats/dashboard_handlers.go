package stats

import (
	"net/http"
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	referenceNow := s.nowUTC()
	snapshot, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats dashboard", func(tx pgx.Tx) (statsdomain.DashboardSnapshot, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.DashboardSnapshot{}, err
		}
		aggregate, err := s.loadOrBuildDashboardAggregateSnapshotInTx(r.Context(), tx, profile.ID, referenceNow)
		if err != nil {
			return statsdomain.DashboardSnapshot{}, err
		}
		return statsdomain.NewDashboardSnapshot(aggregate, referenceNow), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, snapshot)
}

func (s *Service) handleDashboardRecentActivity(w http.ResponseWriter, r *http.Request) {
	generatedAt := s.nowUTC()
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats dashboard recent activity", func(tx pgx.Tx) (statsdomain.DashboardRecentActivityResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.DashboardRecentActivityResponse{}, err
		}
		limit, err := parseDashboardRecentActivityLimit(r)
		if err != nil {
			return statsdomain.DashboardRecentActivityResponse{}, err
		}
		return statsdomain.GetDashboardRecentActivity(r.Context(), tx, profile.ID, limit, generatedAt)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func matchesDashboardSummarySnapshotRequest(params statsdomain.StatsSummaryParams, referenceNow time.Time) bool {
	if params.ModelID != nil || params.APIFamily != nil || params.EndpointID != nil || params.ConnectionID != nil {
		return false
	}
	if !matchesDashboardSummaryWindow(params.FromTime, params.ToTime, referenceNow) {
		return false
	}
	if params.GroupBy == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(*params.GroupBy), "api_family")
}

func matchesDashboardSummaryWindow(fromTime *time.Time, toTime *time.Time, referenceNow time.Time) bool {
	if fromTime == nil || toTime != nil {
		return false
	}
	window := referenceNow.UTC().Sub(fromTime.UTC())
	return withinDashboardTolerance(window, 24*time.Hour)
}

func matchesDashboardThroughputSnapshotRequest(params statsdomain.ThroughputParams, referenceNow time.Time) bool {
	if params.ModelID != nil || params.APIFamily != nil || params.EndpointID != nil || params.ConnectionID != nil {
		return false
	}
	if params.FromTime == nil || params.ToTime == nil {
		return false
	}
	if !withinDashboardTolerance(params.ToTime.UTC().Sub(params.FromTime.UTC()), 24*time.Hour) {
		return false
	}
	return absDuration(referenceNow.UTC().Sub(params.ToTime.UTC())) <= dashboardSnapshotWindowTolerance
}

func matchesDashboardUsageSnapshotRequest(preset string) bool {
	return strings.EqualFold(strings.TrimSpace(preset), "1h")
}

func parseDashboardRecentActivityLimit(r *http.Request) (int, error) {
	limit, err := parseOptionalInt(r, "limit")
	if err != nil {
		return 0, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid limit"}
	}
	if limit == nil {
		return 12, nil
	}
	if *limit <= 0 {
		return 0, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid limit"}
	}
	if *limit > 50 {
		return 50, nil
	}
	return *limit, nil
}
