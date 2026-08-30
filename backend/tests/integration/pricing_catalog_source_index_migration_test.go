package integrationtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// preUpgradeCatalogRevision is the evidence token retained on a seeded
// catalog-import revision. It must survive the 000029 upgrade byte-for-byte.
const preUpgradeCatalogRevision = `"pre-upgrade-catalog-revision"`

// TestPricingCatalogSourceIndexUpgradePreservesRetainedData proves
// 000029_pricing_catalog_source_live_uniqueness is data-preserving: it rewrites
// only the source-offering uniqueness guard, every retained template, revision,
// and card survives untouched, and it shows the retired 000024 shape is what
// actually blocked re-importing a deleted offering.
func TestPricingCatalogSourceIndexUpgradePreservesRetainedData(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "pricing_catalog_source_index")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("run full migration set: %v", err)
	}

	// Re-create the exact pre-000029 retained shape: the uniqueness guard only
	// required both coordinates to be present, so a soft-deleted template kept
	// claiming its offering forever.
	if _, err := conn.Exec(testContext, `DELETE FROM prism_schema_migrations WHERE version = '000029_pricing_catalog_source_live_uniqueness'`); err != nil {
		t.Fatalf("un-stamp 000029: %v", err)
	}
	if _, err := conn.Exec(testContext, `DROP INDEX uq_pricing_templates_catalog_offering`); err != nil {
		t.Fatalf("drop live-uniqueness index: %v", err)
	}
	if _, err := conn.Exec(testContext, `CREATE UNIQUE INDEX uq_pricing_templates_catalog_offering ON public.pricing_templates (catalog_provider_id, catalog_model_id) WHERE catalog_provider_id IS NOT NULL AND catalog_model_id IS NOT NULL`); err != nil {
		t.Fatalf("recreate pre-upgrade index: %v", err)
	}

	now := time.Now().UTC()
	profileID, deletedTemplateID, deletedRevisionID, liveTemplateID := seedPricingCatalogSourceHistory(t, testContext, conn, now)

	// The pre-upgrade guard really is the blocker: re-importing the retired
	// offering as a new live row violates it.
	probeResult, err := conn.Exec(testContext, `INSERT INTO pricing_templates (profile_id, name, catalog_provider_id, catalog_model_id, created_at, updated_at) VALUES ($1, 'openai/gpt-retired reimport', 'openai', 'gpt-retired', $2, $2)`, profileID, now)
	if err == nil {
		_, _ = conn.Exec(testContext, `DELETE FROM pricing_templates WHERE name = 'openai/gpt-retired reimport'`)
		t.Fatalf("pre-upgrade index must block re-importing a retired offering, rows=%v", probeResult.RowsAffected())
	}
	var uniqueErr *pgconn.PgError
	if !errors.As(err, &uniqueErr) || uniqueErr.Code != "23505" || uniqueErr.ConstraintName != "uq_pricing_templates_catalog_offering" {
		t.Fatalf("pre-upgrade rejection must come from the source-offering unique index: %v", err)
	}

	upgradeResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000029 upgrade: %v", err)
	}
	if upgradeResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected 000029 to apply, got %q", upgradeResult.Outcome)
	}

	// The guard now scopes uniqueness to live rows only.
	var indexDefinition string
	if err := conn.QueryRow(testContext, `SELECT indexdef FROM pg_indexes WHERE indexname = 'uq_pricing_templates_catalog_offering'`).Scan(&indexDefinition); err != nil {
		t.Fatalf("read upgraded index definition: %v", err)
	}
	if !strings.Contains(indexDefinition, "deleted_at IS NULL") {
		t.Fatalf("upgraded index must exclude deleted rows: %s", indexDefinition)
	}

	// Every retained row survived the upgrade untouched.
	var count int
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM pricing_templates WHERE id = $1 AND deleted_at IS NOT NULL AND catalog_provider_id = 'openai' AND catalog_model_id = 'gpt-retired'`, deletedTemplateID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retired template must survive with its provenance: count=%d err=%v", count, err)
	}
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM pricing_template_revisions WHERE id = $1 AND template_id = $2 AND revision_source = 'catalog' AND catalog_revision = $3`, deletedRevisionID, deletedTemplateID, preUpgradeCatalogRevision).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retired template revision must survive: count=%d err=%v", count, err)
	}
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM pricing_template_cards WHERE revision_id = $1 AND cached_input_price = '0'`, deletedRevisionID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("explicit zero card must survive: count=%d err=%v", count, err)
	}
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM pricing_templates WHERE id = $1 AND catalog_provider_id IS NULL AND catalog_model_id IS NULL AND deleted_at IS NULL`, liveTemplateID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unrelated manual template must survive: count=%d err=%v", count, err)
	}

	// After the upgrade the same offering can be imported as a new live
	// template, while the retired row keeps its history.
	var newTemplateID int
	if err := conn.QueryRow(testContext, `INSERT INTO pricing_templates (profile_id, name, catalog_provider_id, catalog_model_id, created_at, updated_at) VALUES ($1, 'openai/gpt-retired', 'openai', 'gpt-retired', $2, $2) RETURNING id`, profileID, now).Scan(&newTemplateID); err != nil {
		t.Fatalf("post-upgrade re-import must succeed: %v", err)
	}
	if newTemplateID == deletedTemplateID {
		t.Fatalf("re-import must create a new row")
	}
	if err := conn.QueryRow(testContext, `SELECT COUNT(*) FROM pricing_templates WHERE catalog_provider_id = 'openai' AND catalog_model_id = 'gpt-retired'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("both the retired and the live template must coexist: count=%d err=%v", count, err)
	}
	// Two live rows for one offering is still impossible.
	if _, err := conn.Exec(testContext, `INSERT INTO pricing_templates (profile_id, name, catalog_provider_id, catalog_model_id, created_at, updated_at) VALUES ($1, 'duplicate live', 'openai', 'gpt-retired', $2, $2)`, profileID, now); err == nil {
		t.Fatalf("live uniqueness must still hold for one offering")
	}

	// A second full run stays noop.
	noopResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("re-run migrations: %v", err)
	}
	if noopResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected a second run to be noop, got %q", noopResult.Outcome)
	}
}

// seedPricingCatalogSourceHistory writes the retained pricing state the upgrade
// must carry forward. The pricing shape guards are DEFERRABLE INITIALLY
// DEFERRED, so a revision and its cards have to be inserted inside one
// transaction that closes after both exist.
func seedPricingCatalogSourceHistory(t *testing.T, ctx context.Context, conn *pgx.Conn, now time.Time) (profileID, deletedTemplateID int, deletedRevisionID int64, liveTemplateID int) {
	t.Helper()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ('pricing-source-index', NULL, TRUE, TRUE, TRUE, 1, NULL, $1, $1) RETURNING id`, now).Scan(&profileID); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	var epochID int64
	if err := tx.QueryRow(ctx, `INSERT INTO reporting_currency_epochs (profile_id, epoch, currency_code, currency_symbol, effective_at, superseded_at, created_at, updated_at) VALUES ($1, 1, 'USD', '$', NULL, NULL, $2, $2) RETURNING id`, profileID, now).Scan(&epochID); err != nil {
		t.Fatalf("seed currency epoch: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_settings (profile_id, report_currency_code, report_currency_symbol, timezone_preference, current_reporting_currency_epoch_id, pricing_migration_state, pricing_template_generation, pricing_reference_generation, created_at, updated_at) VALUES ($1, 'USD', '$', NULL, $2, 'ready', 0, 0, $3, $3)`, profileID, epochID, now); err != nil {
		t.Fatalf("seed user settings: %v", err)
	}

	seedTemplate := func(name, provider, model string, deleted bool) (templateID int, revisionID int64) {
		t.Helper()
		var providerArg, modelArg, deletedAtArg any
		if provider != "" {
			providerArg, modelArg = provider, model
		}
		if deleted {
			deletedAtArg = now
		}
		// revision_source and catalog_revision are derived here so each bound
		// parameter keeps one deducible type; the storage CHECK still owns the
		// manual/catalog pairing.
		revisionSource := "manual"
		var catalogRevisionArg any
		if provider != "" {
			revisionSource = "catalog"
			catalogRevisionArg = preUpgradeCatalogRevision
		}
		if err := tx.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, catalog_provider_id, catalog_model_id, current_revision_id, created_at, updated_at, deleted_at) VALUES ($1, $2, NULL, $3, $4, NULL, $5, $5, $6) RETURNING id`, profileID, name, providerArg, modelArg, now, deletedAtArg).Scan(&templateID); err != nil {
			t.Fatalf("seed template %q: %v", name, err)
		}
		// Retained history is seeded as legacy_backfill, the one creation kind
		// that legally carries no operation id.
		if err := tx.QueryRow(ctx, `INSERT INTO pricing_template_revisions (template_id, version, pricing_unit, currency_code, reporting_currency_epoch_id, reporting_currency_epoch, currency_attribution, template_kind, effective_at, created_at, created_by_kind, revision_source, catalog_revision) VALUES ($1, 1, 'PER_1M', 'USD', $2, 1, 'active_epoch', 'standard', $3, $3, 'legacy_backfill', $4, $5) RETURNING id`, templateID, epochID, now, revisionSource, catalogRevisionArg).Scan(&revisionID); err != nil {
			t.Fatalf("seed revision for %q: %v", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pricing_template_cards (revision_id, template_kind, card_role, input_price, output_price, cached_input_price) VALUES ($1, 'standard', 'standard', '2.5', '10', '0')`, revisionID); err != nil {
			t.Fatalf("seed card for %q: %v", name, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE pricing_templates SET current_revision_id = $1 WHERE id = $2`, revisionID, templateID); err != nil {
			t.Fatalf("close template pointer for %q: %v", name, err)
		}
		return templateID, revisionID
	}

	deletedTemplateID, deletedRevisionID = seedTemplate("openai/gpt-retired", "openai", "gpt-retired", true)
	liveTemplateID, _ = seedTemplate("Manual Template", "", "", false)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed transaction: %v", err)
	}
	return profileID, deletedTemplateID, deletedRevisionID, liveTemplateID
}
