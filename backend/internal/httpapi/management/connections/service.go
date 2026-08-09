package connections

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
	pool                *pgxpool.Pool
	ownsPool            bool
	now                 func() time.Time
	corsOriginProvider  platformcors.OriginProvider
	secretEncryptionKey string
}

// DomainError is the HTTP-neutral management error type used across the
// connections package. Callers in other packages can errors.As into it to
// preserve status/detail without depending on this package's routes.
type DomainError struct {
	StatusCode int
	Detail     any
}

func (err *DomainError) Error() string {
	if message, ok := err.Detail.(string); ok {
		return message
	}
	return fmt.Sprint(err.Detail)
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("connection database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	return &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		now:                 now,
		corsOriginProvider:  corsOriginProvider,
		secretEncryptionKey: settings.SecretEncryptionKey,
	}, nil
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
	api.Post("/models/connections/batch", s.handleListConnectionsBatch)
	api.Get("/models/{model_config_id}/connections", s.handleListModelConnections)
	api.Post("/models/{model_config_id}/connections", s.handleCreateModelConnection)
	api.Patch("/models/{model_config_id}/connections/{connection_id}", s.handleUpdateModelConnection)
	api.Post("/models/{model_config_id}/connections/{connection_id}/copies", s.handleCreateConnectionCopies)
	api.Put("/models/{model_config_id}/connections/{connection_id}", s.handleRejectModelConnectionLegacyMutation)
	api.Delete("/models/{model_config_id}/connections/{connection_id}", s.handleDeleteModelConnection)
	api.Put("/models/{model_config_id}/connections/{connection_id}/pricing-template", s.handleRejectModelConnectionLegacyMutation)
	api.Patch("/models/{model_config_id}/connections/{connection_id}/priority", s.handleRejectModelConnectionLegacyMutation)

	api.Get("/connections", s.handleListConnections)
	api.Post("/connections", s.handleCreateConnection)
	api.Get("/connections/{connection_id}", s.handleGetConnection)
	api.Put("/connections/{connection_id}", s.handleUpdateConnection)
	api.Patch("/connections/{connection_id}", s.handleUpdateConnection)
	api.Put("/connections/{connection_id}/pricing-template", s.handleSetConnectionPricingTemplate)
	api.Delete("/connections/{connection_id}", s.handleDeleteConnection)
	api.Get("/connections/{connection_id}/references", s.handleListConnectionReferences)
	api.Route("/pricing-templates", func(router chi.Router) {
		router.Get("/", s.handleListPricingTemplates)
		router.Post("/", s.handleCreatePricingTemplate)
		router.Post("/import", s.handleImportPricingTemplates)
		router.Get("/{template_id}", s.handleGetPricingTemplate)
		router.Put("/{template_id}", s.handleUpdatePricingTemplate)
		router.Delete("/{template_id}", s.handleDeletePricingTemplate)
		router.Get("/{template_id}/connections", s.handleListPricingTemplateConnections)
	})
}
