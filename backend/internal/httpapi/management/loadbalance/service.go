package loadbalance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
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

type domainError struct {
	StatusCode int
	Detail     any
}

func (err *domainError) Error() string {
	if detail, ok := err.Detail.(string); ok {
		return detail
	}
	return fmt.Sprintf("%v", err.Detail)
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
			return nil, fmt.Errorf("create loadbalance database pool: %w", err)
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
	api.Route("/loadbalance", func(router chi.Router) {
		router.Get("/strategies", s.handleListStrategies)
		router.Post("/strategies", s.handleCreateStrategy)
		router.Post("/strategies/defaults", s.handleCreateStrategyDefaults)
		router.Get("/strategies/{strategy_id}", s.handleGetStrategy)
		router.Put("/strategies/{strategy_id}", s.handleUpdateStrategy)
		router.Delete("/strategies/{strategy_id}", s.handleDeleteStrategy)
		router.Get("/current-state", s.handleListCurrentState)
		router.Post("/current-state/{connection_id}/reset", s.handleResetCurrentState)
		router.Get("/events", s.handleListEvents)
		router.Get("/events/{event_id}", s.handleGetEvent)
		router.Delete("/events", s.handleDeleteEvents)
	})
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && strings.TrimSpace(pgErr.ConstraintName) == constraint
}
