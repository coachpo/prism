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

func (s *Service) handleListEvents(w http.ResponseWriter, r *http.Request) {
	rawContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
	if rawContext == "" {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "query_context is required")
		return
	}
	modelID := optionalTrimmedQuery(r, "model_id")
	eventTypes, err := parseRepeatableQuery(r, "event_type")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	eventTypes, err = loadbalancedomain.NormalizeEventTypeValues(eventTypes)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	failureKinds, err := parseRepeatableQuery(r, "failure_kind")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	failureKinds, err = loadbalancedomain.NormalizeFailureKindValues(failureKinds)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	admissionReasons, err := parseRepeatableQuery(r, "admission_reason")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	admissionReasons, err = loadbalancedomain.NormalizeAdmissionReasonValues(admissionReasons)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
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
	sortOrder := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	if sortOrder == "" {
		sortOrder = loadbalancedomain.EventSortDesc
	}
	if sortOrder != loadbalancedomain.EventSortDesc && sortOrder != loadbalancedomain.EventSortAsc {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "sort_order must be 'desc' or 'asc'")
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

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (loadbalancedomain.EventListEnvelope, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return loadbalancedomain.EventListEnvelope{}, err
		}
		contextPayload, err := s.eventsContextCodec.validate(rawContext, profile.ID)
		if err != nil {
			return loadbalancedomain.EventListEnvelope{}, err
		}
		if err := validateEventsRetentionSource(r.Context(), tx, contextPayload, s.nowUTC()); err != nil {
			return loadbalancedomain.EventListEnvelope{}, err
		}
		generation, err := readProfilePlanningGeneration(r.Context(), tx, profile.ID)
		if err != nil {
			return loadbalancedomain.EventListEnvelope{}, err
		}
		bounds := loadbalancedomain.EventQueryBounds{FromTime: contextPayload.FromTime, ToTime: contextPayload.ToTime}
		filters := loadbalancedomain.EventQueryFilters{
			ModelID:          modelID,
			EventTypes:       eventTypes,
			FailureKinds:     failureKinds,
			AdmissionReasons: admissionReasons,
			EndpointID:       endpointID,
			TerminalTargetID: terminalTargetID,
		}
		var cursor *loadbalancedomain.EventCursor
		if rawCursor != "" {
			decoded, err := loadbalancedomain.DecodeEventCursor(rawCursor)
			if err != nil {
				return loadbalancedomain.EventListEnvelope{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "cursor_scope_mismatch", Detail: "invalid event cursor"}
			}
			if !decoded.MatchesScope(profile.ID, bounds, generation, filters, sortOrder, limit) {
				return loadbalancedomain.EventListEnvelope{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "cursor_scope_mismatch", Detail: "event cursor scope mismatch"}
			}
			cursor = &decoded
		}
		return loadbalancedomain.ListEvents(r.Context(), tx, profile.ID, filters, bounds, sortOrder, limit, cursor, generation, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := routeInt64(r, "event_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	rawContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
	if rawContext == "" {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "query_context is required")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (*loadbalancedomain.EventListItem, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		contextPayload, err := s.eventsContextCodec.validate(rawContext, profile.ID)
		if err != nil {
			return nil, err
		}
		if err := validateEventsRetentionSource(r.Context(), tx, contextPayload, s.nowUTC()); err != nil {
			return nil, err
		}
		bounds := loadbalancedomain.EventQueryBounds{FromTime: contextPayload.FromTime, ToTime: contextPayload.ToTime}
		item, err := loadbalancedomain.GetEvent(r.Context(), tx, profile.ID, eventID, bounds, s.nowUTC())
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Loadbalance event not found in the current window"}
		}
		return item, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
