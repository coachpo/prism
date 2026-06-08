package configbundle

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type Options struct {
	CORSOriginProvider    platformcors.OriginProvider
	Pool                  *pgxpool.Pool
	Now                   func() time.Time
	BundleSecretEncrypter func(string) (string, error)
	BundleSecretDecrypter func(string) (string, error)
	BundleSecretKeyID     string
}

type Service struct {
	pool                  *pgxpool.Pool
	ownsPool              bool
	now                   func() time.Time
	corsOriginProvider    platformcors.OriginProvider
	secretEncryptionKey   string
	bundleSecretKeyID     string
	previewTokenKey       string
	bundleSecretEncrypter func(string) (string, error)
	bundleSecretDecrypter func(string) (string, error)
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
		return nil, fmt.Errorf("config bundle database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	bundleKey := strings.TrimSpace(settings.ConfigBundleEncryptionKey)
	if bundleKey == "" {
		bundleKey = strings.TrimSpace(settings.SecretEncryptionKey)
	}

	bundleSecretKeyID := strings.TrimSpace(options.BundleSecretKeyID)
	if bundleSecretKeyID == "" && bundleKey != "" {
		resolvedKeyID, err := buildBundleSecretKeyID(bundleKey)
		if err != nil {
			return nil, err
		}
		bundleSecretKeyID = resolvedKeyID
	}

	bundleSecretEncrypter := options.BundleSecretEncrypter
	if bundleSecretEncrypter == nil {
		bundleSecretEncrypter = func(value string) (string, error) {
			return encryptBundleSecret(value, bundleKey, now)
		}
	}
	bundleSecretDecrypter := options.BundleSecretDecrypter
	if bundleSecretDecrypter == nil {
		bundleSecretDecrypter = func(value string) (string, error) {
			return decryptBundleSecret(value, bundleKey)
		}
	}

	return &Service{
		pool:                  pool,
		ownsPool:              ownsPool,
		now:                   now,
		corsOriginProvider:    corsOriginProvider,
		secretEncryptionKey:   settings.SecretEncryptionKey,
		bundleSecretKeyID:     bundleSecretKeyID,
		previewTokenKey:       bundleKey,
		bundleSecretEncrypter: bundleSecretEncrypter,
		bundleSecretDecrypter: bundleSecretDecrypter,
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
	api.Get("/config/profile/export", s.handleExportProfileBundle)
	api.Post("/config/profile/export/with-secrets", s.handleExportProfileBundleWithSecrets)
	api.Post("/config/profile/import/preview", s.handlePreviewProfileImport)
	api.Post("/config/profile/import", s.handleImportProfileBundle)
	api.Get("/config/vendors/export", s.handleExportVendorCatalog)
	api.Post("/config/vendors/import/preview", s.handlePreviewVendorCatalogImport)
	api.Post("/config/vendors/import", s.handleImportVendorCatalog)
}

func (s *Service) resolvedBundleSecretKeyID() (string, error) {
	if strings.TrimSpace(s.bundleSecretKeyID) == "" {
		return "", fmt.Errorf("config bundle encryption key is required")
	}
	return s.bundleSecretKeyID, nil
}

func (s *Service) resolvedPreviewTokenKey() (string, error) {
	if strings.TrimSpace(s.previewTokenKey) == "" {
		return "", fmt.Errorf("config bundle encryption key is required")
	}
	return s.previewTokenKey, nil
}
