package migrate

import (
	"path/filepath"
	"runtime"
)

const (
	LegacyAlembicVersionTable = "alembic_version"
	HistoryTable              = "prism_schema_migrations"
	DefaultBaselineVersion    = "000001_baseline"
)

func DefaultMigrationsDir() string {
	return packageRelativePath("..", "..", "..", "migrations")
}

func DefaultCutoverSchemaPath() string {
	return packageRelativePath("..", "..", "..", "testdata", "schema", "cutover-live.sql")
}

func packageRelativePath(parts ...string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join(parts...)
	}

	baseParts := append([]string{filepath.Dir(file)}, parts...)
	return filepath.Clean(filepath.Join(baseParts...))
}
