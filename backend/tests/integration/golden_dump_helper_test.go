package integrationtest

// Scratch helper: dumps the normalized migrated schema so the golden file can
// be regenerated when the guarded PRISM_UPDATE_MIGRATION_SCHEMA_GOLDEN flag is
// unavailable. Run: go test ./tests/integration -run TestDumpMigratedSchema -v
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

func TestDumpMigratedSchema(t *testing.T) {
	if os.Getenv("PRISM_DUMP_SCHEMA_TO") == "" {
		t.Skip("PRISM_DUMP_SCHEMA_TO not set")
	}
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	databaseName := "golden_dump"
	conn := harness.openEmptyDatabase(t, testContext, databaseName)
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected apply, got %q", result.Outcome)
	}
	if err := conn.Close(testContext); err != nil {
		t.Fatalf("close conn: %v", err)
	}

	actual := normalizeSchemaDump(runDockerCommandOrFail(
		t,
		testContext,
		"exec",
		"-e",
		"PGPASSWORD=prism",
		harness.dumpContainerName(),
		"pg_dump",
		"--host=127.0.0.1",
		"--username=prism",
		"--dbname="+databaseName,
		"--schema=public",
		"--schema-only",
		"--no-comments",
		"--no-owner",
		"--no-privileges",
		"--no-security-labels",
		"--no-tablespaces",
	))

	path := os.Getenv("PRISM_DUMP_SCHEMA_TO")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(actual+"\n"), 0o644); err != nil {
		t.Fatalf("write dump: %v", err)
	}
	_ = path
	fmt.Printf("wrote %d bytes to %s\n", len(actual), path)
}
