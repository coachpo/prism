package configrules

import (
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type Options struct {
	CORSOriginProvider platformcors.OriginProvider
	Pool               *pgxpool.Pool
	Now                func() time.Time
}

type Service struct {
	pool               *pgxpool.Pool
	ownsPool           bool
	now                func() time.Time
	corsOriginProvider platformcors.OriginProvider
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
		return nil, fmt.Errorf("config rules database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	return &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider}, nil
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
	api.Route("/config", func(router chi.Router) {
		router.Get("/header-blocklist-rules", s.handleListHeaderBlocklistRules)
		router.Get("/header-blocklist-rules/{rule_id}", s.handleGetHeaderBlocklistRule)
		router.Post("/header-blocklist-rules", s.handleCreateHeaderBlocklistRule)
		router.Patch("/header-blocklist-rules/{rule_id}", s.handleUpdateHeaderBlocklistRule)
		router.Delete("/header-blocklist-rules/{rule_id}", s.handleDeleteHeaderBlocklistRule)

		router.Get("/user-agent-client-rules", s.handleListUserAgentClientRules)
		router.Get("/user-agent-client-rules/{rule_id}", s.handleGetUserAgentClientRule)
		router.Post("/user-agent-client-rules", s.handleCreateUserAgentClientRule)
		router.Patch("/user-agent-client-rules/{rule_id}", s.handleUpdateUserAgentClientRule)
		router.Delete("/user-agent-client-rules/{rule_id}", s.handleDeleteUserAgentClientRule)
	})
}
