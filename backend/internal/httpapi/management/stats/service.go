package stats

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type Options struct {
	CORSOriginProvider  platformcors.OriginProvider
	Pool                *pgxpool.Pool
	Now                 func() time.Time
	DashboardSnapshots  *statsdomain.DashboardAggregateStore
	SideEffects         *managementsideeffects.Dispatcher
	SecretEncryptionKey string
}

type Service struct {
	pool                *pgxpool.Pool
	ownsPool            bool
	now                 func() time.Time
	corsOriginProvider  platformcors.OriginProvider
	dashboardSnapshots  *statsdomain.DashboardAggregateStore
	sideEffects         *managementsideeffects.Dispatcher
	secretEncryptionKey string
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("stats database pool is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}
	dashboardSnapshots := options.DashboardSnapshots
	if dashboardSnapshots == nil {
		dashboardSnapshots = statsdomain.NewDashboardAggregateStore()
	}
	service := &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, dashboardSnapshots: dashboardSnapshots, sideEffects: options.SideEffects, secretEncryptionKey: options.SecretEncryptionKey}
	if service.sideEffects != nil {
		service.sideEffects.RegisterHandler(managementsideeffects.EventDashboardSnapshotInvalidate, service.handleDashboardSnapshotInvalidation)
	}
	return service, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) resolveEffectiveProfile(ctx context.Context, request *http.Request) (profiledomain.Profile, error) {
	return profiledomain.ResolveEffectiveProfile(ctx, s.pool, request.Header.Get(profiledomain.ProfileIDHeader))
}

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func parseOptionalUnpricedReason(r *http.Request, key string) (*string, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "PRICING_DISABLED", "MISSING_TOKEN_USAGE", "STREAM_USAGE_UNAVAILABLE", "MISSING_PRICE_DATA":
		return &value, nil
	default:
		return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid " + key}
	}
}
