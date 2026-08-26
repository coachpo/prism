package loadbalance

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	limit, err := parsePositiveIntQueryWithDefault(r, "limit", 50)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	sinceHours, err := parsePositiveIntQueryWithDefault(r, "since_hours", 24)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.IncidentListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.IncidentListResponse{}, err
		}
		return loadbalancedomain.ListIncidents(r.Context(), tx, s.runtimeState, profile.ID, limit, sinceHours, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
