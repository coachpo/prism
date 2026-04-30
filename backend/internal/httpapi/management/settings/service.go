package settings

import (
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
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
		return nil, fmt.Errorf("settings database pool is required")
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
	api.Get("/settings/costing", s.handleGetCostingSettings)
	api.Put("/settings/costing", s.handlePutCostingSettings)
	api.Get("/settings/timezone", s.handleGetTimezonePreference)
	api.Put("/settings/timezone", s.handlePutTimezonePreference)
	api.Get("/settings/retention", s.handleGetRetentionSettings)
	api.Put("/settings/retention", s.handlePutRetentionSettings)
}
