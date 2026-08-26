package loadbalance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

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
