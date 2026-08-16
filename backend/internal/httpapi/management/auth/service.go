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
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type Options struct {
	CORSOriginProvider        platformcors.OriginProvider
	AuthRuntimeConfigProvider RuntimeAuthConfigProvider
	Now                       func() time.Time
	Pool                      *pgxpool.Pool
	ProxyKeyUsagePool         *pgxpool.Pool
	RuntimeCache              *RuntimeCache
	Scheduler                 *background.Scheduler
}

type Service struct {
	pool                      *pgxpool.Pool
	ownsPool                  bool
	now                       func() time.Time
	authJWTSecret             string
	staticAuthRuntimeConfig   RuntimeAuthConfigSnapshot
	authRuntimeConfigProvider RuntimeAuthConfigProvider
	corsOriginProvider        platformcors.OriginProvider
	proxyKeyPreviewSize       int
	runtimeCache              runtimeAuthCache
	proxyKeyUsagePool         *pgxpool.Pool
	proxyKeyUsageWriter       *proxyAPIKeyUsageWriter
	authSettingsSnapshotMu    sync.RWMutex
	authSettingsSnapshot      *AppAuthSettingsSnapshot
	passwordVerifier          passwordVerifier
}

// passwordVerifier is the injectable same-cost password compare seam: the
// production implementation is the bcrypt compare, and tests substitute a
// counting verifier to prove exactly one compare per login attempt
// (Requests/Audit SPEC §7.1 anti-enumeration contract).
type passwordVerifier interface {
	Verify(password string, passwordHash string) bool
}

type bcryptPasswordVerifier struct{}

func (bcryptPasswordVerifier) Verify(password string, passwordHash string) bool {
	return verifyPassword(password, passwordHash)
}

// runtimeAuthCache is the runtime-branch surface the auth service needs for
// permissive/enforced proxy key attribution. *RuntimeCache implements it; tests
// substitute a fake to cover the decision matrix without a database.
type runtimeAuthCache interface {
	LoadFreshRuntimeAuthSettings(ctx context.Context) (RuntimeAuthSettingsSnapshot, error)
	LoadFreshRuntimeProxyKeyDecision(ctx context.Context, now time.Time, rawKey string) (RuntimeProxyKeyDecision, error)
	Invalidate()
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

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	proxyKeyUsagePool := options.ProxyKeyUsagePool

	service := &Service{
		pool:                      pool,
		ownsPool:                  ownsPool,
		now:                       now,
		authJWTSecret:             settings.AuthJWTSecret,
		staticAuthRuntimeConfig:   runtimeAuthConfigSnapshotFromSettings(settings),
		authRuntimeConfigProvider: options.AuthRuntimeConfigProvider,
		corsOriginProvider:        corsOriginProvider,
		proxyKeyPreviewSize:       4,
		runtimeCache:              options.RuntimeCache,
		proxyKeyUsagePool:         proxyKeyUsagePool,
		passwordVerifier:          bcryptPasswordVerifier{},
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

func (s *Service) DrainSideEffects() {
	if s == nil || s.proxyKeyUsageWriter == nil {
		return
	}
	s.proxyKeyUsageWriter.Close()
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.DrainSideEffects()
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

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/auth", func(router chi.Router) {
		router.Get("/status", s.handleGetPublicAuthStatusV2)
		router.Get("/public-bootstrap", s.handleGetPublicBootstrap)
		router.Post("/login", s.handleLogin)
		router.Post("/logout", s.handleLogout)
		router.Post("/refresh", s.handleRefresh)
		router.Get("/session", s.handleGetSession)
		// This is the only public operation-status projection. It validates the
		// raw path/query before the auth middleware exemption and returns the
		// bounded transition view backed by the effective auth pointer.
		router.Get("/operations/{operation_id}/status", s.handleGetAuthOperationStatus)
	})

	api.Route("/settings", func(router chi.Router) {
		router.Get("/auth", s.handleGetAuthSettingsV2)
		router.Put("/auth", s.handlePutAuthSettingsV2)
		router.Get("/auth/operations/{operation_id}", s.handleGetAuthOperation)
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

// verifyPasswordOnce performs exactly one password compare through the
// injectable verifier seam (production: one bcrypt compare).
func (s *Service) verifyPasswordOnce(password string, passwordHash string) bool {
	verifier := s.passwordVerifier
	if verifier == nil {
		verifier = bcryptPasswordVerifier{}
	}
	return verifier.Verify(password, passwordHash)
}
