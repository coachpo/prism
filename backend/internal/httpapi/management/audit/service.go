package audit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type Options struct {
	CORSOriginProvider platformcors.OriginProvider
	Pool               *pgxpool.Pool
	Now                func() time.Time
	Jobs               *managementjobs.Store
}

type Service struct {
	pool               *pgxpool.Pool
	ownsPool           bool
	now                func() time.Time
	corsOriginProvider platformcors.OriginProvider
	jobs               *managementjobs.Store
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("audit database pool is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}
	jobs := options.Jobs
	if jobs == nil {
		jobs = managementjobs.NewStore(managementjobs.Options{Pool: pool, Now: now, CursorSigningKey: settings.SecretEncryptionKey})
	}
	return &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, jobs: jobs}, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/audit", func(router chi.Router) {
		router.Get("/logs", s.handleListLogs)
		router.Get("/logs/{log_id}", s.handleGetLog)
		router.Get("/logs/{log_id}/body/request", s.handleRawRequestBody)
		router.Get("/logs/{log_id}/body/response", s.handleRawResponseBody)
	})
	api.Get("/management/jobs", s.handleListJobs)
	api.Get("/management/jobs/{job_id}", s.handleGetJob)
	api.Get("/management/jobs/{job_id}/checkpoints", s.handleGetJobCheckpoints)
	api.Get("/management/jobs/{job_id}/partitions", s.handleGetJobPartitions)
	api.Post("/management/jobs/{job_id}/cancel", s.handleCancelJob)
}

func (s *Service) handleListLogs(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "audit", func(tx pgx.Tx) (auditdomain.AuditLogListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return auditdomain.AuditLogListResponse{}, err
		}
		params, err := parseListParams(r, profile.ID)
		if err != nil {
			return auditdomain.AuditLogListResponse{}, err
		}
		params.ReferenceNow = s.now().UTC()
		if params.RequestLogID != nil {
			auditEnabledAtRequest, exists, err := loadRequestLogAuditCaptureState(r.Context(), tx, profile.ID, *params.RequestLogID)
			if err != nil {
				return auditdomain.AuditLogListResponse{}, err
			}
			if exists && !auditEnabledAtRequest {
				return auditdomain.AuditLogListResponse{}, &auditdomain.HTTPError{StatusCode: http.StatusConflict, Detail: "Audit capture unavailable for this request"}
			}
		}
		return auditdomain.ListLogs(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetLog(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	logID, err := routeInt(r, "log_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "audit", func(tx pgx.Tx) (*auditdomain.AuditLogDetail, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		return auditdomain.GetLog(r.Context(), tx, profile.ID, logID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if response == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Audit log not found")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleRawRequestBody streams the exact stored request BYTEA prefix with
// safe attachment headers (Requests SPEC §5.4).
func (s *Service) handleRawRequestBody(w http.ResponseWriter, r *http.Request) {
	s.handleRawAuditBody(w, r, auditdomain.RawBodyDirectionRequest)
}

// handleRawResponseBody streams the exact stored response BYTEA prefix.
func (s *Service) handleRawResponseBody(w http.ResponseWriter, r *http.Request) {
	s.handleRawAuditBody(w, r, auditdomain.RawBodyDirectionResponse)
}

func (s *Service) handleRawAuditBody(w http.ResponseWriter, r *http.Request, direction auditdomain.RawBodyDirection) {
	responseutil.SetPrivateNoStoreHeaders(w)
	logID, err := routeInt(r, "log_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var found bool
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "audit", func(tx pgx.Tx) (*auditdomain.RawBodyResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		result, loaded, err := auditdomain.LoadRawAuditBody(r.Context(), tx, profile.ID, logID, direction)
		if err != nil {
			return nil, err
		}
		found = loaded
		return result, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if result == nil || !found {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Audit log not found")
		return
	}
	if !result.AuditEnabledAtRequest {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusConflict, "Audit capture unavailable for this request")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"audit-%d-%s.bin\"", logID, string(direction)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.Body)))
	if result.Truncated {
		w.Header().Set("X-Prism-Body-Truncated", "true")
	}
	if result.BytesObserved != nil {
		w.Header().Set("X-Prism-Body-Bytes-Observed", fmt.Sprintf("%d", *result.BytesObserved))
	}
	if result.BytesStored != nil {
		w.Header().Set("X-Prism-Body-Bytes-Stored", fmt.Sprintf("%d", *result.BytesStored))
	}
	if result.CaptureEndState != nil {
		w.Header().Set("X-Prism-Body-Capture-End-State", *result.CaptureEndState)
	}
	if _, err := w.Write(result.Body); err != nil {
		slog.Warn("audit raw body stream interrupted", "error", err)
	}
}

func parseListParams(r *http.Request, profileID int) (auditdomain.ListParams, error) {
	if err := rejectUnsupportedListFilters(r); err != nil {
		return auditdomain.ListParams{}, err
	}
	requestLogID, err := parseOptionalBigInt(r, "request_log_id")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	statusCode, err := parseOptionalInt(r, "status_code")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	connectionID, err := parseOptionalInt(r, "connection_id")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	anchorAuditID, err := parseOptionalBigInt(r, "anchor_id")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	fromTime, err := parseRequiredTime(r, "from")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	toTime, err := parseRequiredTime(r, "to")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	if !fromTime.Before(*toTime) {
		return auditdomain.ListParams{}, &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_window_invalid", Detail: "Audit window from must be before to."}
	}
	maxWindow := 7 * 24 * time.Hour
	if toTime.Sub(*fromTime) > maxWindow {
		return auditdomain.ListParams{}, &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_window_too_large", Detail: "Audit event windows may not exceed 7 days.", Details: map[string]any{"max_window_seconds": int(maxWindow.Seconds())}}
	}
	limit, err := parseCappedPositiveIntWithDefault(r, "limit", 50, 200)
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	sortOrder := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("sort")))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "desc" {
		return auditdomain.ListParams{}, &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_sort_unsupported", Detail: "Only descending audit sort is supported."}
	}
	return auditdomain.ListParams{ProfileID: profileID, RequestLogID: requestLogID, ModelID: normalizedQueryString(r, "model_id"), StatusCode: statusCode, EndpointID: endpointID, ConnectionID: connectionID, FromTime: fromTime, ToTime: toTime, Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")), Sort: sortOrder, AnchorAuditID: anchorAuditID}, nil
}

func rejectUnsupportedListFilters(r *http.Request) error {
	allowed := []string{"request_log_id", "model_id", "status_code", "endpoint_id", "connection_id", "from", "to", "limit", "cursor", "sort", "anchor_id"}
	for key := range r.URL.Query() {
		if !slices.Contains(allowed, key) {
			return &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_filter_unsupported", Detail: fmt.Sprintf("Unsupported audit filter %q.", key)}
		}
	}
	return nil
}

func parseRequiredTime(r *http.Request, key string) (*time.Time, error) {
	parsed, err := parseOptionalTime(r, key)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_window_required", Detail: "Audit list requires from and to query parameters."}
	}
	return parsed, nil
}

func invalidQueryParameter(key string, reason string) error {
	return &auditdomain.HTTPError{
		StatusCode: http.StatusUnprocessableEntity,
		Code:       "invalid_query_parameter",
		Detail:     fmt.Sprintf("%s %s", key, reason),
		Details:    map[string]any{"parameter": key},
	}
}

func parseOptionalTime(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, invalidQueryParameter(key, "must be an RFC3339 timestamp")
		}
	}
	resolved := parsed.UTC()
	return &resolved, nil
}

func parseOptionalInt(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil, invalidQueryParameter(key, "must be an integer")
	}
	if parsed > math.MaxInt32 || parsed < math.MinInt32 {
		return nil, invalidQueryParameter(key, fmt.Sprintf("must be within [%d, %d]", math.MinInt32, math.MaxInt32))
	}
	resolved := parsed
	return &resolved, nil
}

// parseOptionalBigInt is the unbounded variant for bigint identifiers
// (request_log_id, anchor_id): audit rows carry BIGINT request-log ids, so an
// int32 bound would reject legitimate values past 2^31.
func parseOptionalBigInt(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil, invalidQueryParameter(key, "must be an integer")
	}
	resolved := parsed
	return &resolved, nil
}

func parsePositiveIntWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalInt(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	if *parsed <= 0 {
		return 0, invalidQueryParameter(key, "must be >= 1")
	}
	return *parsed, nil
}

func parseCappedPositiveIntWithDefault(r *http.Request, key string, defaultValue int, maxValue int) (int, error) {
	value, err := parsePositiveIntWithDefault(r, key, defaultValue)
	if err != nil {
		return 0, err
	}
	if value > maxValue {
		return maxValue, nil
	}
	return value, nil
}

func normalizedQueryString(r *http.Request, key string) *string {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	return &raw
}

func routeInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, invalidQueryParameter(name, "must be a positive integer")
	}
	return parsed, nil
}

func loadRequestLogAuditCaptureState(ctx context.Context, tx pgx.Tx, profileID int, requestLogID int) (bool, bool, error) {
	var auditEnabledAtRequest bool
	if err := tx.QueryRow(ctx, `SELECT audit_enabled_at_request FROM request_logs WHERE profile_id = $1 AND id = $2 ORDER BY created_at DESC LIMIT 1`, profileID, requestLogID).Scan(&auditEnabledAtRequest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("load request-log %d audit snapshot for profile %d: %w", requestLogID, profileID, err)
	}
	return auditEnabledAtRequest, true, nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	var auditErr *auditdomain.HTTPError
	if errors.As(err, &auditErr) {
		if auditErr.Code != "" {
			writeStructuredError(w, r, corsSnapshot, auditErr)
			return
		}
		responseutil.WriteError(w, r, corsSnapshot, auditErr.StatusCode, auditErr.Detail)
		return
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeStructuredError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *auditdomain.HTTPError) {
	var details any
	if len(err.Details) > 0 {
		details = err.Details
	}
	responseutil.WriteProblem(w, r, corsSnapshot, err.StatusCode, err.Code, err.Detail, map[string]any{}, details)
}

func writeCORSHeaders(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
}
