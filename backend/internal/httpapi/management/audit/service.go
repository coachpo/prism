package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/platform/config"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type Options struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

type Service struct {
	pool           *pgxpool.Pool
	ownsPool       bool
	now            func() time.Time
	allowedOrigins map[string]struct{}
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		if strings.TrimSpace(settings.DatabaseURL) == "" {
			return nil, fmt.Errorf("database URL is required")
		}
		createdPool, err := pgxpool.New(context.Background(), settings.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("create audit database pool: %w", err)
		}
		pool = createdPool
		ownsPool = true
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}
	return &Service{pool: pool, ownsPool: ownsPool, now: now, allowedOrigins: allowedOrigins}, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/audit", func(router chi.Router) {
		router.Get("/logs", s.handleListLogs)
		router.Get("/logs/{log_id}", s.handleGetLog)
		router.Delete("/logs", s.handleDeleteLogs)
	})
}

func (s *Service) handleListLogs(w http.ResponseWriter, r *http.Request) {
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (auditdomain.AuditLogListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return auditdomain.AuditLogListResponse{}, err
		}
		params, err := parseListParams(r, profile.ID)
		if err != nil {
			return auditdomain.AuditLogListResponse{}, err
		}
		return auditdomain.ListLogs(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetLog(w http.ResponseWriter, r *http.Request) {
	logID, err := routeInt(r, "log_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (*auditdomain.AuditLogDetail, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		return auditdomain.GetLog(r.Context(), tx, profile.ID, logID)
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	if response == nil {
		writeError(w, r, s.allowedOrigins, http.StatusNotFound, "Audit log not found")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteLogs(w http.ResponseWriter, r *http.Request) {
	_, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (struct{}, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return struct{}{}, err
		}
		before, err := parseOptionalTime(r, "before")
		if err != nil {
			return struct{}{}, err
		}
		olderThanDays, err := parseOptionalInt(r, "older_than_days")
		if err != nil {
			return struct{}{}, err
		}
		deleteAll, err := parseOptionalBool(r, "delete_all")
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, auditdomain.DeleteLogs(r.Context(), tx, auditdomain.DeleteParams{ProfileID: profile.ID, Before: before, OlderThanDays: olderThanDays, DeleteAll: deleteAll, ReferenceNow: s.nowUTC()})
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func parseListParams(r *http.Request, profileID int) (auditdomain.ListParams, error) {
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
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	limit, err := parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	offset, err := parseNonNegativeIntWithDefault(r, "offset", 0)
	if err != nil {
		return auditdomain.ListParams{}, err
	}
	return auditdomain.ListParams{ProfileID: profileID, RequestLogID: requestLogID, VendorID: vendorID, ModelID: normalizedQueryString(r, "model_id"), StatusCode: statusCode, EndpointID: endpointID, ConnectionID: connectionID, FromTime: fromTime, ToTime: toTime, Limit: limit, Offset: offset}, nil
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

func parseNonNegativeIntWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalInt(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	if *parsed < 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return *parsed, nil
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

func withTxValue[T any](ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, fmt.Errorf("begin audit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	value, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit audit transaction: %w", err)
	}
	return value, nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		writeError(w, r, allowedOrigins, profileErr.StatusCode, profileErr.Detail)
		return
	}
	var auditErr *auditdomain.HTTPError
	if errors.As(err, &auditErr) {
		writeError(w, r, allowedOrigins, auditErr.StatusCode, auditErr.Detail)
		return
	}
	writeError(w, r, allowedOrigins, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail string) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
