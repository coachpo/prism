package loadbalance

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

// handleListCurrentState serves the global configured-target current state
// projection: no internal row-id input, optional object/state filters, stable
// configuration-identity cursor, process-local completeness.
func (s *Service) handleListCurrentState(w http.ResponseWriter, r *http.Request) {
	modelID := optionalTrimmedQuery(r, "model_id")
	endpointID, err := parseOptionalPositiveIntQuery(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	terminalTargetID, err := parseOptionalPositiveIntQuery(r, "terminal_target_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	states, err := parseRepeatableQuery(r, "state")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parsePositiveIntQueryWithDefault(r, "limit", 50)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if limit > 100 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "limit must be between 1 and 100")
		return
	}
	rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.GlobalCurrentStateResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.GlobalCurrentStateResponse{}, err
		}
		generation, err := readProfilePlanningGeneration(r.Context(), tx, profile.ID)
		if err != nil {
			return loadbalancedomain.GlobalCurrentStateResponse{}, err
		}
		filters := loadbalancedomain.CurrentStateFilters{
			ModelID:          modelID,
			States:           states,
			EndpointID:       endpointID,
			TerminalTargetID: terminalTargetID,
		}
		var cursor *loadbalancedomain.CurrentStateCursor
		if rawCursor != "" {
			decoded, err := loadbalancedomain.DecodeCurrentStateCursor(rawCursor)
			if err != nil {
				return loadbalancedomain.GlobalCurrentStateResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "cursor_scope_mismatch", Detail: "invalid current state cursor"}
			}
			if decoded.ProfileID != profile.ID || decoded.Limit != limit ||
				!loadbalancedomain.NullableStringPtrEqual(decoded.ModelID, modelID) || !loadbalancedomain.NullableIntPtrEqual(decoded.EndpointID, endpointID) || !loadbalancedomain.NullableIntPtrEqual(decoded.TerminalTargetID, terminalTargetID) {
				return loadbalancedomain.GlobalCurrentStateResponse{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "cursor_scope_mismatch", Detail: "current state cursor scope mismatch"}
			}
			if decoded.ConfigurationRevision != generation {
				return loadbalancedomain.GlobalCurrentStateResponse{}, &domainError{StatusCode: http.StatusConflict, Code: "current_state_cursor_stale", Detail: "current state configuration changed; reload from the first page"}
			}
			cursor = &decoded
		}
		return loadbalancedomain.ListGlobalCurrentState(r.Context(), tx, s.runtimeState, profile.ID, s.instanceID, filters, limit, cursor, generation, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleResetCurrentState(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.CurrentStateResetResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.CurrentStateResetResponse{}, err
		}
		return loadbalancedomain.ResetCurrentState(r.Context(), tx, s.runtimeState, profile.ID, connectionID, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
