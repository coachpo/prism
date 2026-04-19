package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type DashboardUpdatePublisher interface {
	PublishDashboardUpdate(context.Context, int, int) (bool, error)
}

type Options struct {
	Pool             *pgxpool.Pool
	HTTPClient       *http.Client
	Now              func() time.Time
	DashboardUpdates DashboardUpdatePublisher
}

type Service struct {
	pool                *pgxpool.Pool
	ownsPool            bool
	httpClient          *http.Client
	now                 func() time.Time
	secretEncryptionKey string
	dashboardUpdates    DashboardUpdatePublisher
}

type domainError struct {
	StatusCode int
	Detail     string
}

func (err *domainError) Error() string {
	return err.Detail
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
			return nil, fmt.Errorf("create runtime database pool: %w", err)
		}
		pool = createdPool
		ownsPool = true
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				DisableCompression: true,
			},
		}
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		httpClient:          client,
		now:                 now,
		secretEncryptionKey: settings.SecretEncryptionKey,
		dashboardUpdates:    options.DashboardUpdates,
	}, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(s.handleProxy)
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer r.Body.Close()
	}
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(rawBody) == 0 {
		rawBody = nil
	}

	plan, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (requestPlan, error) {
		return s.buildRequestPlan(r.Context(), tx, r, rawBody)
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	startedAt := s.nowUTC()
	execution, err := s.executeRequest(r.Context(), r.Method, plan, r.URL.RawQuery)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	defer execution.Response.Body.Close()

	copyResponseHeaders(w.Header(), execution.Response.Header)
	w.WriteHeader(execution.Response.StatusCode)

	var responseBody []byte
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/event-stream") {
		_, _ = io.Copy(w, execution.Response.Body)
		s.recordRuntimeActivity(r.Context(), plan, execution, r, startedAt, nil)
		return
	}
	responseBody, err = io.ReadAll(execution.Response.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to read upstream response")
		return
	}
	_, _ = io.Copy(w, bytes.NewReader(responseBody))
	s.recordRuntimeActivity(r.Context(), plan, execution, r, startedAt, responseBody)
}

func withTxValue[T any](ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, fmt.Errorf("begin runtime transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	value, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit runtime transaction: %w", err)
	}
	return value, nil
}

func writeDomainError(w http.ResponseWriter, err error) {
	var runtimeErr *domainError
	if errors.As(err, &runtimeErr) {
		writeError(w, runtimeErr.StatusCode, runtimeErr.Detail)
		return
	}
	writeError(w, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, statusCode int, detail string) {
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
