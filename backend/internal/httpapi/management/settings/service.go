package settings

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
)

type Options struct {
	CORSOriginProvider platformcors.OriginProvider
	Pool               *pgxpool.Pool
	Now                func() time.Time
	Jobs               *managementjobs.Store
}

type Service struct {
	pool               *pgxpool.Pool
	ownsPool           bool
	now                func() time.Time
	corsOriginProvider platformcors.OriginProvider
	jobs               *managementjobs.Store
	currencyCursorKey  []byte
}

type domainError struct {
	StatusCode int
	Detail     any
}

// costingSettingsChangedDetail keeps the authoritative owner timestamp
// attached to a Pricing CAS conflict without putting it in prose. The
// settings problem adapter emits this value in the registered details object.
type costingSettingsChangedDetail struct {
	CurrentUpdatedAt string
}

func (detail costingSettingsChangedDetail) Error() string { return "costing_settings_changed" }

func (err *domainError) Error() string {
	if detail, ok := err.Detail.(string); ok {
		return detail
	}
	return fmt.Sprintf("%v", err.Detail)
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("settings database pool is required")
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}

	jobs := options.Jobs
	if jobs == nil {
		jobs = managementjobs.NewStore(managementjobs.Options{Pool: pool, Now: now, CursorSigningKey: settings.SecretEncryptionKey})
	}

	cursorSeed := settings.SecretEncryptionKey
	if cursorSeed == "" {
		cursorSeed = "settings-currency-cursor"
	}
	cursorHash := sha256.Sum256([]byte("prism.settings.currency.cursor.v1:" + cursorSeed))
	return &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, jobs: jobs, currencyCursorKey: cursorHash[:]}, nil
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
	api.Get("/settings/audit", s.handleGetAuditSettings)
	api.Put("/settings/audit", s.handlePutAuditSettings)
	api.Get("/settings/audit/storage-summary", s.handleGetAuditStorageSummary)
	api.Get("/settings/costing", s.handleGetCostingSettings)
	api.Put("/settings/costing", s.handlePutCostingSettings)
	api.Get("/settings/costing/pricing-migration-inventories/{inventory_id}/templates", s.handleListCurrencyMigrationInventoryTemplates)
	api.Get("/settings/costing/pricing-migration-inventories/{inventory_id}/fx-evidence", s.handleListCurrencyMigrationInventoryFXEvidence)
	api.Post("/settings/costing/currency-migration-drafts", s.handleCreateCurrencyMigrationDraft)
	api.Get("/settings/costing/currency-migration-drafts/{draft_id}", s.handleGetCurrencyMigrationDraft)
	api.Get("/settings/costing/currency-migration-drafts/{draft_id}/chunks", s.handleListCurrencyMigrationDraftChunks)
	api.Put("/settings/costing/currency-migration-drafts/{draft_id}/chunks/{ordinal}", s.handlePutCurrencyMigrationDraftChunk)
	api.Post("/settings/costing/currency-migration-drafts/{draft_id}/seal", s.handleSealCurrencyMigrationDraft)
	api.Get("/settings/costing/currency-migration-drafts/{draft_id}/items", s.handleListCurrencyMigrationDraftItems)
	api.Post("/settings/costing/currency-migrations/preview", s.handleCurrencyMigrationDraftPreview)
	api.Get("/settings/costing/currency-migration-drafts/{draft_id}/preview-items", s.handleListCurrencyMigrationDraftPreviewItems)
	api.Post("/settings/costing/currency-migrations/commit", s.handleCurrencyMigrationDraftCommit)

	retention := &retentionService{pool: s.pool, now: s.now, jobs: s.jobs}
	api.Get("/settings/log-retention", func(w http.ResponseWriter, r *http.Request) {
		retention.handleGetRetentionSettings(w, r, s.corsSnapshot())
	})
	api.Put("/settings/log-retention", func(w http.ResponseWriter, r *http.Request) {
		retention.handlePutRetentionSettings(w, r, s.corsSnapshot())
	})
	api.Post("/settings/log-retention/owner-drift-archive", func(w http.ResponseWriter, r *http.Request) {
		retention.handleArchiveOwnerDrift(w, r, s.corsSnapshot())
	})
	api.Post("/maintenance/log-retention/preflights", func(w http.ResponseWriter, r *http.Request) {
		retention.handleCreatePreflight(w, r, s.corsSnapshot())
	})
	api.Post("/maintenance/log-retention/jobs", func(w http.ResponseWriter, r *http.Request) {
		retention.handleCreateManualRetentionJob(w, r, s.corsSnapshot())
	})
}
