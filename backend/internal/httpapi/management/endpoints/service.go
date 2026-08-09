package endpoints

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
	attemptTimeout      time.Duration
	verifySlots         chan struct{}
}

func (s *Service) acquireVerifySlot() bool {
	select {
	case s.verifySlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Service) releaseVerifySlot() {
	<-s.verifySlots
}

type domainError struct {
	StatusCode int
	Detail     any
	Fields     map[string]any
}

func (err *domainError) Error() string {
	if message, ok := err.Detail.(string); ok {
		return message
	}
	return fmt.Sprint(err.Detail)
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("endpoint database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	attemptTimeout := settings.RuntimeSideEffects().AttemptTimeout
	if attemptTimeout <= 0 {
		attemptTimeout = 10 * time.Second
	}
	return &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		now:                 now,
		corsOriginProvider:  corsOriginProvider,
		secretEncryptionKey: settings.SecretEncryptionKey,
		attemptTimeout:      attemptTimeout,
		verifySlots:         make(chan struct{}, verifyConcurrencyLimit),
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
	api.Get("/endpoints/connections", s.handleListEndpointConnections)
	api.Get("/endpoints", s.handleListEndpoints)
	api.Post("/endpoints", s.handleCreateEndpoint)
	api.Post("/endpoints/references/batch", s.handleEndpointReferencesBatch)
	api.Get("/endpoints/{endpoint_id}/references", s.handleEndpointReferencesDetail)
	api.Put("/endpoints/{endpoint_id}", s.handleUpdateEndpoint)
	api.Post("/endpoints/{endpoint_id}/duplicate", s.handleDuplicateEndpoint)
	api.Post("/endpoints/{endpoint_id}/verify", s.handleEndpointVerify)
	api.Delete("/endpoints/{endpoint_id}/orphan-connections/{connection_id}", s.handleOrphanConnectionCleanup)
	api.Delete("/endpoints/{endpoint_id}", s.handleDeleteEndpoint)
}
