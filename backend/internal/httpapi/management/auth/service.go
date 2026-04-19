package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type Mailer interface {
	SendEmailVerificationOTP(context.Context, string, string) error
	SendPasswordResetEmail(context.Context, string, string) error
}

type Options struct {
	Mailer Mailer
	Now    func() time.Time
	Pool   *pgxpool.Pool
}

type Service struct {
	pool                *pgxpool.Pool
	ownsPool            bool
	mailer              Mailer
	now                 func() time.Time
	authJWTSecret       string
	accessTokenTTL      time.Duration
	refreshTokenTTL     time.Duration
	resetCodeTTL        time.Duration
	accessCookieName    string
	refreshCookieName   string
	cookieSecure        bool
	allowedOrigins      map[string]struct{}
	proxyKeyPreviewSize int
}

type authSubject struct {
	ID           int
	TokenVersion int
	Username     string
}

type runtimeProxyKey struct {
	ID   int
	Name string
}

type authSubjectContextKey struct{}
type runtimeProxyKeyContextKey struct{}

type noopMailer struct{}

func (noopMailer) SendEmailVerificationOTP(context.Context, string, string) error {
	return nil
}

func (noopMailer) SendPasswordResetEmail(context.Context, string, string) error {
	return nil
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
			return nil, fmt.Errorf("create auth database pool: %w", err)
		}
		pool = createdPool
		ownsPool = true
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	mailer := options.Mailer
	if mailer == nil {
		mailer = noopMailer{}
	}

	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}

	return &Service{
		pool:                pool,
		ownsPool:            ownsPool,
		mailer:              mailer,
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

func runtimeProxyKeyFromRequest(request *http.Request) (*runtimeProxyKey, bool) {
	proxyKeyValue := request.Context().Value(runtimeProxyKeyContextKey{})
	proxyKey, ok := proxyKeyValue.(runtimeProxyKey)
	if !ok {
		return nil, false
	}
	return &proxyKey, true
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
