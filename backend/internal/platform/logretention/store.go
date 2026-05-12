package logretention

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	publicSchema = "public"
	horizonDays  = 15
)

var ErrUnknownManagedTable = errors.New("unknown managed log table")

type Options struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

type Partition struct {
	TableName     string
	PartitionName string
	Start         time.Time
	End           time.Time
}

type RetentionSummary struct {
	TableName           string
	DroppedPartitions   []string
	BoundaryPartition   string
	BoundaryRowsDeleted int64
	DeleteAll           bool
}

type managedTable struct {
	name string
}

var managedTableOrder = []string{
	"request_logs",
	"audit_logs",
	"usage_request_events",
	"loadbalance_events",
	"sidecar_watchdog_actions",
}

var managedTables = map[string]managedTable{
	"request_logs":             {name: "request_logs"},
	"audit_logs":               {name: "audit_logs"},
	"usage_request_events":     {name: "usage_request_events"},
	"loadbalance_events":       {name: "loadbalance_events"},
	"sidecar_watchdog_actions": {name: "sidecar_watchdog_actions"},
}

func NewStore(options Options) *Store {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Store{pool: options.Pool, now: now}
}

func ManagedTables() []string {
	return append([]string(nil), managedTableOrder...)
}

func HorizonDays() int {
	return horizonDays
}

func (s *Store) EnsurePartitionHorizon(ctx context.Context) error {
	for _, tableName := range managedTableOrder {
		if err := s.EnsurePartitionHorizonForTable(ctx, tableName); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EnsurePartitionHorizonForTable(ctx context.Context, tableName string) error {
	if _, err := lookupManagedTable(tableName); err != nil {
		return err
	}
	if s == nil || s.pool == nil {
		return fmt.Errorf("log retention store pool is required")
	}
	start := utcDay(s.now())
	for offset := range horizonDays {
		if err := s.EnsurePartitionForTime(ctx, tableName, start.AddDate(0, 0, offset)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EnsurePartitionForTime(ctx context.Context, tableName string, timestamp time.Time) error {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return err
	}
	if s == nil || s.pool == nil {
		return fmt.Errorf("log retention store pool is required")
	}
	day := utcDay(timestamp)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin partition creation for %s: %w", table.name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryLockID(table.name, day)); err != nil {
		return fmt.Errorf("lock partition creation for %s: %w", table.name, err)
	}
	if err := createPartition(ctx, tx, table, day); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit partition creation for %s: %w", table.name, err)
	}
	return nil
}

func (s *Store) ListPartitions(ctx context.Context, tableName string) ([]Partition, error) {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("log retention store pool is required")
	}
	partitions, err := listPartitions(ctx, s.pool, table)
	if err != nil {
		return nil, err
	}
	return partitions, nil
}

func (s *Store) RunRetention(ctx context.Context, tableName string, cutoff *time.Time, deleteAll bool) (RetentionSummary, error) {
	if deleteAll {
		dropped, err := s.DropAllPartitions(ctx, tableName)
		if err != nil {
			return RetentionSummary{}, err
		}
		if err := s.EnsurePartitionHorizonForTable(ctx, tableName); err != nil {
			return RetentionSummary{}, err
		}
		return RetentionSummary{TableName: tableName, DroppedPartitions: partitionNames(dropped), DeleteAll: true}, nil
	}
	if cutoff == nil {
		return RetentionSummary{}, fmt.Errorf("retention cutoff is required")
	}
	cutoffUTC := cutoff.UTC()
	dropped, err := s.DropExpiredPartitions(ctx, tableName, cutoffUTC)
	if err != nil {
		return RetentionSummary{}, err
	}
	summary := RetentionSummary{TableName: tableName, DroppedPartitions: partitionNames(dropped)}
	boundary, ok, err := s.BoundaryPartitionForCutoff(ctx, tableName, cutoffUTC)
	if err != nil {
		return RetentionSummary{}, err
	}
	if !ok {
		return summary, nil
	}
	deleted, err := s.DeleteBoundaryRows(ctx, tableName, cutoffUTC)
	if err != nil {
		return RetentionSummary{}, err
	}
	summary.BoundaryPartition = boundary.PartitionName
	summary.BoundaryRowsDeleted = deleted
	if err := s.VacuumAnalyzePartition(ctx, tableName, boundary.PartitionName); err != nil {
		return RetentionSummary{}, err
	}
	return summary, nil
}

func (s *Store) DropExpiredPartitions(ctx context.Context, tableName string, cutoff time.Time) ([]Partition, error) {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("log retention store pool is required")
	}
	partitions, err := listPartitions(ctx, s.pool, table)
	if err != nil {
		return nil, err
	}
	cutoff = cutoff.UTC()
	dropped := make([]Partition, 0)
	for _, partition := range partitions {
		if partition.End.After(cutoff) {
			continue
		}
		if _, err := s.pool.Exec(ctx, `DROP TABLE IF EXISTS `+quoteQualified(publicSchema, partition.PartitionName)); err != nil {
			return dropped, fmt.Errorf("drop expired partition %s: %w", partition.PartitionName, err)
		}
		dropped = append(dropped, partition)
	}
	return dropped, nil
}

func (s *Store) DropAllPartitions(ctx context.Context, tableName string) ([]Partition, error) {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("log retention store pool is required")
	}
	partitions, err := listPartitions(ctx, s.pool, table)
	if err != nil {
		return nil, err
	}
	dropped := make([]Partition, 0, len(partitions))
	for _, partition := range partitions {
		if _, err := s.pool.Exec(ctx, `DROP TABLE IF EXISTS `+quoteQualified(publicSchema, partition.PartitionName)); err != nil {
			return dropped, fmt.Errorf("drop partition %s: %w", partition.PartitionName, err)
		}
		dropped = append(dropped, partition)
	}
	return dropped, nil
}

func (s *Store) BoundaryPartitionForCutoff(ctx context.Context, tableName string, cutoff time.Time) (Partition, bool, error) {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return Partition{}, false, err
	}
	if s == nil || s.pool == nil {
		return Partition{}, false, fmt.Errorf("log retention store pool is required")
	}
	partitions, err := listPartitions(ctx, s.pool, table)
	if err != nil {
		return Partition{}, false, err
	}
	cutoff = cutoff.UTC()
	for _, partition := range partitions {
		if !cutoff.Before(partition.Start) && cutoff.Before(partition.End) {
			return partition, true, nil
		}
	}
	return Partition{}, false, nil
}

func (s *Store) DeleteBoundaryRows(ctx context.Context, tableName string, cutoff time.Time) (int64, error) {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return 0, err
	}
	if s == nil || s.pool == nil {
		return 0, fmt.Errorf("log retention store pool is required")
	}
	partitions, err := listPartitions(ctx, s.pool, table)
	if err != nil {
		return 0, err
	}
	cutoff = cutoff.UTC()
	for _, partition := range partitions {
		if cutoff.Before(partition.Start) || !cutoff.Before(partition.End) {
			continue
		}
		commandTag, err := s.pool.Exec(ctx, `DELETE FROM `+quoteQualified(publicSchema, partition.PartitionName)+` WHERE created_at < $1`, cutoff)
		if err != nil {
			return 0, fmt.Errorf("delete boundary rows from %s: %w", partition.PartitionName, err)
		}
		return commandTag.RowsAffected(), nil
	}
	return 0, nil
}

func (s *Store) VacuumAnalyzePartition(ctx context.Context, tableName string, partitionName string) error {
	table, err := lookupManagedTable(tableName)
	if err != nil {
		return err
	}
	if s == nil || s.pool == nil {
		return fmt.Errorf("log retention store pool is required")
	}
	partitions, err := listPartitions(ctx, s.pool, table)
	if err != nil {
		return err
	}
	for _, partition := range partitions {
		if partition.PartitionName != partitionName {
			continue
		}
		if _, err := s.pool.Exec(ctx, `VACUUM (ANALYZE, PROCESS_TOAST TRUE) `+quoteQualified(publicSchema, partition.PartitionName)); err != nil {
			return fmt.Errorf("vacuum analyze partition %s: %w", partition.PartitionName, err)
		}
		return nil
	}
	return fmt.Errorf("partition %q is not a child of managed table %s", partitionName, table.name)
}

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func createPartition(ctx context.Context, exec queryExecutor, table managedTable, day time.Time) error {
	start := utcDay(day)
	end := start.AddDate(0, 0, 1)
	query := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM (%s) TO (%s) WITH (`+
			`autovacuum_vacuum_scale_factor = 0.02, `+
			`autovacuum_vacuum_threshold = 10000, `+
			`toast.autovacuum_vacuum_scale_factor = 0.02, `+
			`toast.autovacuum_vacuum_threshold = 10000)`,
		quoteQualified(publicSchema, partitionNameForDay(table.name, start)),
		quoteQualified(publicSchema, table.name),
		quoteLiteral(timestampLiteral(start)),
		quoteLiteral(timestampLiteral(end)),
	)
	if _, err := exec.Exec(ctx, query); err != nil {
		return fmt.Errorf("create partition %s for %s: %w", partitionNameForDay(table.name, start), table.name, err)
	}
	return nil
}

func listPartitions(ctx context.Context, exec queryExecutor, table managedTable) ([]Partition, error) {
	rows, err := exec.Query(ctx, `
		SELECT child.relname
		FROM pg_inherits inheritance
		JOIN pg_class parent ON parent.oid = inheritance.inhparent
		JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
		JOIN pg_class child ON child.oid = inheritance.inhrelid
		JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
		WHERE parent_ns.nspname = $1 AND parent.relname = $2 AND child_ns.nspname = $1
		ORDER BY child.relname`, publicSchema, table.name)
	if err != nil {
		return nil, fmt.Errorf("list partitions for %s: %w", table.name, err)
	}
	defer rows.Close()

	partitions := make([]Partition, 0)
	for rows.Next() {
		var childName string
		if err := rows.Scan(&childName); err != nil {
			return nil, fmt.Errorf("scan partition for %s: %w", table.name, err)
		}
		partition, err := partitionFromName(table.name, childName)
		if err != nil {
			return nil, err
		}
		partitions = append(partitions, partition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate partitions for %s: %w", table.name, err)
	}
	return partitions, nil
}

func lookupManagedTable(tableName string) (managedTable, error) {
	table, ok := managedTables[tableName]
	if !ok {
		return managedTable{}, fmt.Errorf("%w: %q", ErrUnknownManagedTable, tableName)
	}
	return table, nil
}

func partitionNames(partitions []Partition) []string {
	names := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		names = append(names, partition.PartitionName)
	}
	return names
}

func partitionFromName(tableName string, childName string) (Partition, error) {
	prefix := tableName + "_p"
	if !strings.HasPrefix(childName, prefix) {
		return Partition{}, fmt.Errorf("unexpected partition %q for managed table %s", childName, tableName)
	}
	day, err := time.ParseInLocation("20060102", strings.TrimPrefix(childName, prefix), time.UTC)
	if err != nil {
		return Partition{}, fmt.Errorf("parse partition date from %q: %w", childName, err)
	}
	start := utcDay(day)
	return Partition{TableName: tableName, PartitionName: childName, Start: start, End: start.AddDate(0, 0, 1)}, nil
}

func partitionNameForDay(tableName string, day time.Time) string {
	return tableName + "_p" + utcDay(day).Format("20060102")
}

func utcDay(timestamp time.Time) time.Time {
	utc := timestamp.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func timestampLiteral(timestamp time.Time) string {
	return utcDay(timestamp).Format("2006-01-02 15:04:05-07")
}

func quoteQualified(schema string, relation string) string {
	return pgx.Identifier{schema, relation}.Sanitize()
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func advisoryLockID(tableName string, day time.Time) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("logretention:"))
	_, _ = hasher.Write([]byte(tableName))
	_, _ = hasher.Write([]byte(":"))
	_, _ = hasher.Write([]byte(utcDay(day).Format("20060102")))
	return int64(hasher.Sum64())
}
