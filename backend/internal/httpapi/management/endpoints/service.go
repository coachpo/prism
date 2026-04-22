package endpoints

import (
	"context"
	"fmt"
	"strings"
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
	pool                *pgxpool.Pool
	ownsPool            bool
	now                 func() time.Time
	allowedOrigins      map[string]struct{}
	secretEncryptionKey string
}

type domainError struct {
	StatusCode int
	Detail     any
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
		if strings.TrimSpace(settings.DatabaseURL) == "" {
			return nil, fmt.Errorf("database URL is required")
		}
		createdPool, err := pgxpool.New(context.Background(), settings.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("create endpoint database pool: %w", err)
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

	return &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		now:                 now,
		allowedOrigins:      allowedOrigins,
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

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Get("/endpoints/connections", s.handleListEndpointConnections)
	api.Get("/endpoints", s.handleListEndpoints)
	api.Post("/endpoints", s.handleCreateEndpoint)
	api.Put("/endpoints/{endpoint_id}", s.handleUpdateEndpoint)
	api.Patch("/endpoints/{endpoint_id}/position", s.handleMoveEndpointPosition)
	api.Post("/endpoints/{endpoint_id}/duplicate", s.handleDuplicateEndpoint)
	api.Delete("/endpoints/{endpoint_id}", s.handleDeleteEndpoint)
}
