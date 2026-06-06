package models

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
	Detail     string
	Fields     map[string]any
}

const (
	contextOverflowPromotionTargetField                 = "context_overflow_promotion_target_id"
	promotionTargetValidationCodeUnknown                = "unknown_target"
	promotionTargetValidationCodeSelf                   = "self_target"
	promotionTargetValidationCodeDisabled               = "disabled_target"
	promotionTargetValidationCodeFacade                 = "facade_target"
	promotionTargetValidationCodeCrossProfile           = "cross_profile_target"
	promotionTargetValidationCodeSameTerminal           = "same_terminal_target"
	promotionTargetValidationCodeAPIFamilyMismatch      = "api_family_mismatch"
	promotionTargetValidationCodeContextWindowNotLarger = "context_window_not_larger"
)

func (err *domainError) Error() string {
	return err.Detail
}

func (s *Service) validateContextOverflowPromotionTarget(ctx context.Context, exec queryExecutor, profileID int, source modelRecord) error {
	return validateConfiguredPromotionTarget(ctx, exec, profileID, source)
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
	api.Get("/models", s.handleListModels)
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && strings.TrimSpace(pgErr.ConstraintName) == constraint
}
