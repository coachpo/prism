package models

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type Options struct {
	CORSOriginProvider  platformcors.OriginProvider
	Pool                *pgxpool.Pool
	Now                 func() time.Time
	SecretEncryptionKey string
}

type Service struct {
	pool                  *pgxpool.Pool
	ownsPool              bool
	now                   func() time.Time
	corsOriginProvider    platformcors.OriginProvider
	terminalTargetCreator TerminalTargetCreator
	secretEncryptionKey   string
}

// TerminalTargetCreator is implemented by the connections management service
// and lets the model composite-create flow create an owner-scoped Terminal
// Target inside the same transaction without duplicating validators.
type TerminalTargetCreator interface {
	CreateOwnerScopedConnectionTx(ctx context.Context, tx pgx.Tx, profileID int, input connections.OwnerScopedConnectionCreateInput) (connections.OwnerConnectionCreateResult, error)
}

// SetTerminalTargetCreator wires the owner-scoped connection creator after
// service construction (the connections service is built after models).
func (s *Service) SetTerminalTargetCreator(creator TerminalTargetCreator) {
	s.terminalTargetCreator = creator
}

type domainError struct {
	StatusCode int
	Detail     string
	Fields     map[string]any
}

func (err *domainError) Error() string {
	return err.Detail
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("model database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	return &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, secretEncryptionKey: settings.SecretEncryptionKey}, nil
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
	api.Post("/models/by-endpoints", s.handleModelsByEndpoints)
	api.Get("/models/{model_config_id}/targets", s.handleListModelTargets)
	api.Post("/models/{model_config_id}/targets", s.handleCreateModelTarget)
	api.Put("/models/{model_config_id}/targets/{target_id}", s.handleUpdateModelTarget)
	api.Patch("/models/{model_config_id}/targets/{target_id}", s.handleUpdateModelTarget)
	api.Patch("/models/{model_config_id}/targets/{target_id}/position", s.handleMoveModelTargetPosition)
	api.Delete("/models/{model_config_id}/targets/{target_id}", s.handleDeleteModelTarget)
	api.Get("/models/{model_config_id}", s.handleGetModel)
	api.Post("/models", s.handleCreateModel)
	api.Put("/models/{model_config_id}", s.handleUpdateModel)
	api.Delete("/models/{model_config_id}", s.handleDeleteModel)
	api.Get("/models/by-endpoint/{endpoint_id}", s.handleModelsByEndpoint)
	api.Get("/models/{model_config_id}/routing-diagnostics", s.handleGetRoutingDiagnostics)
	api.Get("/models", s.handleListModels)
	api.Get("/models/route-witnesses", s.handleGetRouteWitnesses)
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && strings.TrimSpace(pgErr.ConstraintName) == constraint
}
