package auth

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/email/outbox"
)

type Mailer interface {
	SendEmailVerificationOTP(context.Context, string, string) error
	SendPasswordResetEmail(context.Context, string, string) error
}

type Options struct {
	Mailer            Mailer
	EmailOutbox       *outbox.Store
	Now               func() time.Time
	Pool              *pgxpool.Pool
	ProxyKeyUsagePool *pgxpool.Pool
	RuntimeCache      *RuntimeCache
	Scheduler         *background.Scheduler
}

type Service struct {
	pool                   *pgxpool.Pool
	ownsPool               bool
	emailOutbox            *outbox.Store
	now                    func() time.Time
	authJWTSecret          string
	accessTokenTTL         time.Duration
	refreshTokenTTL        time.Duration
	resetCodeTTL           time.Duration
	accessCookieName       string
	refreshCookieName      string
	cookieSecure           bool
	allowedOrigins         map[string]struct{}
	proxyKeyPreviewSize    int
	runtimeCache           *RuntimeCache
	proxyKeyUsagePool      *pgxpool.Pool
	proxyKeyUsageWriter    *proxyAPIKeyUsageWriter
	authSettingsSnapshotMu sync.RWMutex
	authSettingsSnapshot   *AppAuthSettingsSnapshot
}

type authSubject struct {
	ID           int
	TokenVersion int
	Username     string
}

type authSubjectContextKey struct{}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("auth database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}

	proxyKeyUsagePool := options.ProxyKeyUsagePool

	service := &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		emailOutbox:         options.EmailOutbox,
		now:                 now,
		authJWTSecret:       settings.AuthJWTSecret,
		accessTokenTTL:      time.Duration(settings.AuthAccessTokenTTLSeconds) * time.Second,
		refreshTokenTTL:     time.Duration(settings.AuthRefreshTokenTTLSeconds) * time.Second,
		resetCodeTTL:        time.Duration(settings.AuthResetCodeTTLSeconds) * time.Second,
		accessCookieName:    settings.AuthCookieName,
		refreshCookieName:   settings.AuthRefreshCookieName,
		cookieSecure:        settings.AuthCookieSecure,
		allowedOrigins:      allowedOrigins,
		proxyKeyPreviewSize: 4,
		runtimeCache:        options.RuntimeCache,
		proxyKeyUsagePool:   proxyKeyUsagePool,
	}
	if proxyKeyUsagePool != nil {
		service.proxyKeyUsageWriter = newProxyAPIKeyUsageWriter(service.recordProxyAPIKeyUsage, options.Scheduler)
	}
	return service, nil
}

func (s *Service) RegisterBackgroundWorkers(scheduler *background.Scheduler) error {
	if s == nil || s.proxyKeyUsageWriter == nil {
		return nil
	}
	return s.proxyKeyUsageWriter.RegisterBackgroundWorker(scheduler)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	if s.proxyKeyUsageWriter != nil {
		s.proxyKeyUsageWriter.Close()
	}
	if s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) loadAppAuthSettingsSnapshot(ctx context.Context) (AppAuthSettingsSnapshot, error) {
	if s == nil {
		return AppAuthSettingsSnapshot{}, fmt.Errorf("auth service unavailable")
	}
	s.authSettingsSnapshotMu.RLock()
	cached := s.authSettingsSnapshot
	s.authSettingsSnapshotMu.RUnlock()
	if cached != nil {
		return *cached, nil
	}
	settingsRow, err := s.loadOrCreateAppAuthSettings(ctx, s.pool)
	if err != nil {
		return AppAuthSettingsSnapshot{}, err
	}
	snapshot := appAuthSettingsSnapshotFromRow(settingsRow)
	s.authSettingsSnapshotMu.Lock()
	s.authSettingsSnapshot = &snapshot
	s.authSettingsSnapshotMu.Unlock()
	return snapshot, nil
}

func (s *Service) invalidateAppAuthSettingsSnapshot() {
	if s == nil {
		return
	}
	s.authSettingsSnapshotMu.Lock()
	defer s.authSettingsSnapshotMu.Unlock()
	s.authSettingsSnapshot = nil
}

func (s *Service) loadRuntimeAuthSettings(ctx context.Context) (RuntimeAuthSettingsSnapshot, error) {
	if s.runtimeCache == nil {
		return RuntimeAuthSettingsSnapshot{}, runtimeSnapshotUnavailableError()
	}
	return s.runtimeCache.LoadFreshRuntimeAuthSettings(ctx)
}

func (s *Service) InvalidateRuntimeCache() {
	if s == nil || s.runtimeCache == nil {
		return
	}
	s.runtimeCache.Invalidate()
}

func (s *Service) InvalidateAppAuthSettingsSnapshot() {
	s.invalidateAppAuthSettingsSnapshot()
}

func (s *Service) enqueueProxyAPIKeyUsage(keyID int, lastUsedAt time.Time, lastUsedIP string) error {
	if s == nil || s.proxyKeyUsageWriter == nil {
		return fmt.Errorf("proxy api key usage writer unavailable")
	}
	return s.proxyKeyUsageWriter.Enqueue(keyID, lastUsedAt, lastUsedIP)
}

func authSubjectFromRequest(request *http.Request) (*authSubject, bool) {
	authSubjectValue := request.Context().Value(authSubjectContextKey{})
	authSubject, ok := authSubjectValue.(authSubject)
	if !ok {
		return nil, false
	}
	return &authSubject, true
}

func authSubjectIDFromRequest(request *http.Request) *int {
	authSubject, ok := authSubjectFromRequest(request)
	if !ok {
		return nil
	}
	return &authSubject.ID
}

func runtimeProxyKeyFromRequest(request *http.Request) (*requestcontext.RuntimeProxyKeySnapshot, bool) {
	return requestcontext.RuntimeProxyKeyFromContext(request.Context())
}

func (s *Service) ManagementMiddleware(next http.Handler) http.Handler {
	return s.managementMiddleware(next)
}

func (s *Service) RuntimeMiddleware(next http.Handler) http.Handler {
	return s.runtimeMiddleware(next)
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/auth", func(router chi.Router) {
		router.Get("/status", s.handleGetAuthStatus)
		router.Get("/public-bootstrap", s.handleGetPublicBootstrap)
		router.Post("/login", s.handleLogin)
		router.Post("/logout", s.handleLogout)
		router.Post("/refresh", s.handleRefresh)
		router.Get("/session", s.handleGetSession)
		router.Post("/password-reset/request", s.handlePasswordResetRequest)
		router.Post("/password-reset/confirm", s.handlePasswordResetConfirm)
	})

	api.Route("/settings", func(router chi.Router) {
		router.Get("/auth", s.handleGetAuthSettings)
		router.Put("/auth", s.handlePutAuthSettings)
		router.Post("/auth/email-verification/request", s.handleEmailVerificationRequest)
		router.Post("/auth/email-verification/confirm", s.handleEmailVerificationConfirm)
		router.Get("/auth/proxy-keys", s.handleListProxyKeys)
		router.Post("/auth/proxy-keys", s.handleCreateProxyKey)
		router.Patch("/auth/proxy-keys/{key_id}", s.handleUpdateProxyKey)
		router.Post("/auth/proxy-keys/{key_id}/rotate", s.handleRotateProxyKey)
		router.Delete("/auth/proxy-keys/{key_id}", s.handleDeleteProxyKey)
	})
}

func (s *Service) RuntimeProbeRouter() http.Handler {
	router := chi.NewRouter()
	router.HandleFunc("/", s.handleRuntimeProbe)
	router.HandleFunc("/*", s.handleRuntimeProbe)
	return router
}
