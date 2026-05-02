package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
		jobs = managementjobs.NewStore(managementjobs.Options{Pool: pool, Now: now})
	}
	return &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, jobs: jobs}, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
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
		router.Delete("/logs", s.handleDeleteLogs)
		router.Post("/logs/delete-jobs", s.handleCreateDeleteJob)
	})
	api.Get("/management/jobs", s.handleListJobs)
	api.Get("/management/jobs/{job_id}", s.handleGetJob)
	api.Post("/management/jobs/{job_id}/cancel", s.handleCancelJob)
}

func (s *Service) handleListLogs(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "audit", func(tx pgx.Tx) (auditdomain.AuditLogListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return auditdomain.AuditLogListResponse{}, err
		}
		params, err := parseListParams(r, profile.ID)
		if err != nil {
			return auditdomain.AuditLogListResponse{}, err
		}
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
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetLog(w http.ResponseWriter, r *http.Request) {
	logID, err := routeInt(r, "log_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "Audit log not found")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteLogs(w http.ResponseWriter, r *http.Request) {
	job, err := s.createAuditDeleteJobFromQuery(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), auditJobError(err))
		return
	}
	w.Header().Set("Location", "/api/management/jobs/"+job.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "state": job.State, "status_url": "/api/management/jobs/" + job.ID})
}

func (s *Service) handleCreateDeleteJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.createAuditDeleteJobFromBody(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), auditJobError(err))
		return
	}
	w.Header().Set("Location", "/api/management/jobs/"+job.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "state": job.State, "status_url": "/api/management/jobs/" + job.ID})
}

func auditJobError(err error) error {
	if err == nil {
		return nil
	}
	switch err.Error() {
	case "delete_reason_required":
		return &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "delete_reason_required", Detail: "Delete job reason is required."}
	case "delete_scope_required":
		return &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "delete_scope_required", Detail: "Delete job scope is required."}
	case "delete_idempotency_required":
		return &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "delete_idempotency_required", Detail: "Idempotency-Key is required."}
	default:
		return err
	}
}

func (s *Service) createAuditDeleteJobFromQuery(r *http.Request) (managementjobs.Job, error) {
	return pgxutil.InTxValue(r.Context(), s.pool, "audit", func(tx pgx.Tx) (managementjobs.Job, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return managementjobs.Job{}, err
		}
		before, err := parseOptionalTime(r, "before")
		if err != nil {
			return managementjobs.Job{}, err
		}
		olderThanDays, err := parseOptionalInt(r, "older_than_days")
		if err != nil {
			return managementjobs.Job{}, err
		}
		deleteAll, err := parseOptionalBool(r, "delete_all")
		if err != nil {
			return managementjobs.Job{}, err
		}
		if before == nil && olderThanDays == nil && !deleteAll {
			olderThanDays, err = loadAuditLogRetentionDays(r.Context(), tx, profile.ID)
			if err != nil {
				return managementjobs.Job{}, err
			}
			if olderThanDays == nil {
				return managementjobs.Job{}, &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "delete_scope_required", Detail: "No audit log retention policy configured; provide 'before', 'older_than_days', or 'delete_all=true', or configure audit_logs_retention_days in /api/settings/retention"}
			}
		}
		if olderThanDays != nil {
			cutoff := s.nowUTC().Add(-time.Duration(*olderThanDays) * 24 * time.Hour)
			before = &cutoff
		}
		key := r.Header.Get("Idempotency-Key")
		if strings.TrimSpace(key) == "" {
			key = fmt.Sprintf("legacy-audit-delete:%d:%v:%v", profile.ID, deleteAll, before)
		}
		return s.jobs.CreateAuditDeleteJob(r.Context(), managementjobs.CreateAuditDeleteJobRequest{ProfileID: profile.ID, RequestedBy: fmt.Sprintf("profile:%d", profile.ID), IdempotencyKey: key, Reason: "audit retention delete", Scope: managementjobs.AuditDeleteScope{Before: before, DeleteAll: deleteAll}})
	})
}

type auditDeleteJobBody struct {
	Scope  managementjobs.AuditDeleteScope `json:"scope"`
	Reason string                          `json:"reason"`
}

func (s *Service) createAuditDeleteJobFromBody(r *http.Request) (managementjobs.Job, error) {
	defer func() { _ = r.Body.Close() }()
	var body auditDeleteJobBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return managementjobs.Job{}, err
	}
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		return managementjobs.Job{}, err
	}
	return s.jobs.CreateAuditDeleteJob(r.Context(), managementjobs.CreateAuditDeleteJobRequest{ProfileID: profile.ID, RequestedBy: fmt.Sprintf("profile:%d", profile.ID), IdempotencyKey: r.Header.Get("Idempotency-Key"), Reason: body.Reason, Scope: body.Scope})
}

func (s *Service) handleGetJob(w http.ResponseWriter, r *http.Request) {
	s.withJob(w, r, func(job managementjobs.Job) { writeJSON(w, http.StatusOK, job) })
}
func (s *Service) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	job, err := s.jobs.CancelJob(r.Context(), chi.URLParam(r, "job_id"), profile.ID)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
func (s *Service) handleListJobs(w http.ResponseWriter, r *http.Request) {
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := s.jobs.ListJobs(r.Context(), profile.ID, 50)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}
func (s *Service) withJob(w http.ResponseWriter, r *http.Request, fn func(managementjobs.Job)) {
	profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), s.pool, r.Header.Get(profiledomain.ProfileIDHeader))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	job, err := s.jobs.GetJob(r.Context(), chi.URLParam(r, "job_id"), profile.ID)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "Job not found")
		return
	}
	fn(job)
}

func loadAuditLogRetentionDays(ctx context.Context, tx pgx.Tx, profileID int) (*int, error) {
	var auditLogsRetentionDays sql.NullInt32
	if err := tx.QueryRow(ctx, `SELECT audit_logs_retention_days FROM user_settings WHERE profile_id = $1 LIMIT 1`, profileID).Scan(&auditLogsRetentionDays); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load audit retention settings for profile %d: %w", profileID, err)
	}
	if !auditLogsRetentionDays.Valid {
		return nil, nil
	}
	resolved := int(auditLogsRetentionDays.Int32)
	return &resolved, nil
}

func parseListParams(r *http.Request, profileID int) (auditdomain.ListParams, error) {
	if err := rejectUnsupportedListFilters(r); err != nil {
		return auditdomain.ListParams{}, err
	}
	requestLogID, err := parseOptionalInt(r, "request_log_id")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	vendorID, err := parseOptionalInt(r, "vendor_id")
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
	fromTime, err := parseRequiredTimeAlias(r, "from", "from_time")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	toTime, err := parseRequiredTimeAlias(r, "to", "to_time")
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
	return auditdomain.ListParams{ProfileID: profileID, RequestLogID: requestLogID, VendorID: vendorID, ModelID: normalizedQueryString(r, "model_id"), StatusCode: statusCode, EndpointID: endpointID, ConnectionID: connectionID, FromTime: fromTime, ToTime: toTime, Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")), Sort: sortOrder}, nil
}

func rejectUnsupportedListFilters(r *http.Request) error {
	allowed := []string{"request_log_id", "vendor_id", "model_id", "status_code", "endpoint_id", "connection_id", "from", "to", "from_time", "to_time", "limit", "cursor", "sort"}
	for key := range r.URL.Query() {
		if !slices.Contains(allowed, key) {
			return &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_filter_unsupported", Detail: fmt.Sprintf("Unsupported audit filter %q.", key)}
		}
	}
	return nil
}

func parseRequiredTimeAlias(r *http.Request, primary string, legacy string) (*time.Time, error) {
	parsed, err := parseOptionalTime(r, primary)
	if err != nil {
		return nil, err
	}
	if parsed != nil {
		return parsed, nil
	}
	parsed, err = parseOptionalTime(r, legacy)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, &auditdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "audit_window_required", Detail: "Audit list requires from and to query parameters."}
	}
	return parsed, nil
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
			return nil, fmt.Errorf("invalid %s", key)
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
		return nil, fmt.Errorf("invalid %s", key)
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
		return 0, fmt.Errorf("invalid %s", key)
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

func parseOptionalBool(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
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
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func loadRequestLogAuditCaptureState(ctx context.Context, tx pgx.Tx, profileID int, requestLogID int) (bool, bool, error) {
	var auditEnabledAtRequest bool
	if err := tx.QueryRow(ctx, `SELECT audit_enabled_at_request FROM request_logs WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, requestLogID).Scan(&auditEnabledAtRequest); err != nil {
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
		writeError(w, r, corsSnapshot, auditErr.StatusCode, auditErr.Detail)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	writeCORSHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func writeStructuredError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *auditdomain.HTTPError) {
	writeCORSHeaders(w, r, corsSnapshot)
	payload := map[string]any{"error": map[string]any{"code": err.Code, "message": err.Detail}}
	if len(err.Details) > 0 {
		payload["error"].(map[string]any)["details"] = err.Details
	}
	writeJSON(w, err.StatusCode, payload)
}

func writeCORSHeaders(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
