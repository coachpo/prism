package settings

import (
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
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
	api.Get("/settings/audit", s.handleGetAuditSettings)
	api.Put("/settings/audit", s.handlePutAuditSettings)
	api.Get("/settings/costing", s.handleGetCostingSettings)
	api.Put("/settings/costing", s.handlePutCostingSettings)
	api.Get("/settings/timezone", s.handleGetTimezonePreference)
	api.Put("/settings/timezone", s.handlePutTimezonePreference)
	api.Get("/settings/log-retention", s.handleGetRetentionSettings)
	api.Put("/settings/log-retention", s.handlePutRetentionSettings)
	api.Post("/maintenance/log-retention/jobs", s.handleCreateLogRetentionJob)
}
