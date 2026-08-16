package loadbalance

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
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

type eventsQueryContextResponse struct {
	QueryContext    string                              `json:"query_context"`
	RequestedPreset string                              `json:"requested_preset"`
	EventBounds     eventsQueryContextBounds            `json:"event_bounds"`
	Coverage        loadbalancedomain.EventCoverage     `json:"coverage"`
	SourceStatus    loadbalancedomain.EventSourceStatus `json:"source_status"`
	GeneratedAt     time.Time                           `json:"generated_at"`
}

type eventsQueryContextBounds struct {
	FromTime *time.Time `json:"from_time"`
	ToTime   *time.Time `json:"to_time"`
}

type eventsQueryContextRequest struct {
	RequestedPreset string `json:"requested_preset"`
	CustomFromTime  string `json:"custom_from_time"`
	CustomToTime    string `json:"custom_to_time"`
}

// handleIssueEventsQueryContext issues a signed events query context with
// frozen half-open event bounds resolved from a preset or a custom window.
func (s *Service) handleIssueEventsQueryContext(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var requestBody eventsQueryContextRequest
	if err := decoder.Decode(&requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, responseutil.SanitizeDecodeError(err).Error())
		return
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "loadbalance", func(tx pgx.Tx) (eventsQueryContextResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return eventsQueryContextResponse{}, err
		}
		nowAt := s.nowUTC()
		preset := strings.ToLower(strings.TrimSpace(requestBody.RequestedPreset))
		var fromTime *time.Time
		var toTime *time.Time
		switch preset {
		case EventsPreset1h:
			from := nowAt.Add(-1 * time.Hour)
			fromTime = &from
			to := nowAt
			toTime = &to
		case EventsPreset6h:
			from := nowAt.Add(-6 * time.Hour)
			fromTime = &from
			to := nowAt
			toTime = &to
		case EventsPreset24h:
			from := nowAt.Add(-24 * time.Hour)
			fromTime = &from
			to := nowAt
			toTime = &to
		case EventsPreset7d:
			from := nowAt.Add(-7 * 24 * time.Hour)
			fromTime = &from
			to := nowAt
			toTime = &to
		case EventsPreset30d:
			from := nowAt.Add(-30 * 24 * time.Hour)
			fromTime = &from
			to := nowAt
			toTime = &to
		case EventsPresetAll:
			floor, err := loadbalancedomain.LoadEventsRetentionFloor(r.Context(), tx, nowAt)
			if err != nil {
				return eventsQueryContextResponse{}, err
			}
			from := floor
			fromTime = &from
			to := nowAt
			toTime = &to
		case EventsPresetCustom:
			fromValue, err := time.Parse(time.RFC3339, requestBody.CustomFromTime)
			if err != nil {
				return eventsQueryContextResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "custom_from_time must be a valid RFC3339 time"}
			}
			toValue, err := time.Parse(time.RFC3339, requestBody.CustomToTime)
			if err != nil {
				return eventsQueryContextResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "custom_to_time must be a valid RFC3339 time"}
			}
			fromUTC := fromValue.UTC()
			toUTC := toValue.UTC()
			if !toUTC.After(fromUTC) {
				return eventsQueryContextResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "custom window must be a positive half-open interval"}
			}
			if toUTC.Sub(fromUTC) > 30*24*time.Hour {
				return eventsQueryContextResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "custom window must not exceed 30 days"}
			}
			fromTime = &fromUTC
			toTime = &toUTC
		default:
			return eventsQueryContextResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "requested_preset must be one of '1h', '6h', '24h', '7d', '30d', 'all', or 'custom'"}
		}
		source, err := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, "loadbalance_events", nowAt)
		if err != nil {
			return eventsQueryContextResponse{}, err
		}
		if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
			return eventsQueryContextResponse{}, &domainError{StatusCode: http.StatusServiceUnavailable, Code: "event_purge_in_progress", Detail: "events are temporarily unavailable while retention cleanup is publishing"}
		}
		token, err := s.eventsContextCodec.issue(profile.ID, preset, fromTime, toTime, source.RetentionEpoch, 15*time.Minute)
		if err != nil {
			return eventsQueryContextResponse{}, err
		}
		return eventsQueryContextResponse{
			QueryContext:    token,
			RequestedPreset: preset,
			EventBounds:     eventsQueryContextBounds{FromTime: fromTime, ToTime: toTime},
			Coverage: loadbalancedomain.EventCoverage{
				Complete: true, Gaps: []loadbalancedomain.EventCoverageGap{},
				RetentionEpoch: source.RetentionEpoch, RetentionGeneration: source.RetentionGeneration,
				PurgeState: source.PurgeState, SourceRevision: source.SourceRevision,
			},
			SourceStatus: loadbalancedomain.EventSourceStatus{Delivery: "best_effort", TransitionLedgerComplete: false},
			GeneratedAt:  nowAt,
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

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

func validateEventsRetentionSource(ctx context.Context, tx pgx.Tx, payload eventsQueryContextPayload, now time.Time) error {
	if strings.TrimSpace(payload.RetentionEpoch) == "" {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_query_context", Detail: "invalid query context"}
	}
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, "loadbalance_events", now.UTC())
	if err != nil {
		return err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Code: "event_purge_in_progress", Detail: "events are temporarily unavailable while retention cleanup is publishing"}
	}
	if source.RetentionEpoch != payload.RetentionEpoch {
		return &domainError{StatusCode: http.StatusGone, Code: "dataset_snapshot_revoked", Detail: "events query context snapshot has been revoked"}
	}
	return nil
}

func parseRepeatableQuery(r *http.Request, key string) ([]string, error) {
	rawValues, ok := r.URL.Query()[key]
	if !ok || len(rawValues) == 0 {
		return nil, nil
	}
	items := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				items = append(items, trimmed)
			}
		}
	}
	return items, nil
}

func optionalTrimmedQuery(r *http.Request, key string) *string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil
	}
	return &value
}

func parseRequiredPositiveIntQuery(r *http.Request, key string) (int, error) {
	parsed, err := parseOptionalPositiveIntQuery(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return 0, fmt.Errorf("%s is required", key)
	}
	return *parsed, nil
}

func parseOptionalPositiveIntQuery(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	resolved := parsed
	return &resolved, nil
}

func parsePositiveIntQueryWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalPositiveIntQuery(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	return *parsed, nil
}

func routeInt64(request *http.Request, name string) (int64, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func randomInstanceID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "unknown-instance"
	}
	return hex.EncodeToString(buffer)
}
