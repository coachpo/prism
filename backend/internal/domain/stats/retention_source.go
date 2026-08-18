package stats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RetentionFloorEpochSource is the shared request/audit retention source
// projection (Observe SPEC §3.1.1, exposed by the owner read models and
// materialized in the shared retention resource row). It is the
// single source consumed by query contexts, ordinary Requests, Audit, manual
// purge final publish and the Settings projection; consumers never recompute
// a floor from policy days or MIN(created_at).
type RetentionFloorEpochSource struct {
	ContractVersion      int        `json:"contract_version"`
	Domain               string     `json:"domain"`
	SourceRevision       string     `json:"source_revision"`
	RetentionEpoch       string     `json:"retention_epoch"`
	RetentionGeneration  string     `json:"retention_generation"`
	FenceGeneration      string     `json:"fence_generation"`
	ConfiguredCutoff     *time.Time `json:"configured_cutoff"`
	PublishedFloor       *time.Time `json:"published_floor"`
	RevocationEpoch      int64      `json:"revocation_epoch"`
	PurgeState           string     `json:"purge_state"`
	PhysicalReclaimState string     `json:"physical_reclaim_state"`
	DesiredWorkIdentity  *string    `json:"desired_work_identity,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
	GeneratedAt          time.Time  `json:"generated_at"`
}

// ActualCoverageProjection is the owning read-model bound used by Settings.
// It is deliberately separate from the policy/resource source: a policy
// cutoff is not evidence that rows exist, and an empty read model is not
// represented as a fabricated zero-day interval.
type ActualCoverageProjection struct {
	Domain             string
	Earliest           *time.Time
	Latest             *time.Time
	Revision           string
	Hash               string
	GeneratedAt        *time.Time
	Source             string
	Precision          string
	Complete           bool
	Freshness          string
	Gaps               []map[string]any
	GapReason          *string
	MaterializationCut map[string]any
}

type retentionSourceExecutor interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RetentionSourceWriter is the small owner-side SQL seam used by retention
// projections. pgx.Tx, pgxpool.Pool and the load-balance domain's executor all
// satisfy it; keeping the seam here prevents consumers from inventing a
// second coverage writer.
type RetentionSourceWriter interface {
	retentionSourceExecutor
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type retentionSourceWriter = RetentionSourceWriter

// LoadActualCoverageProjection reads the bounded owner read model for one
// retention domain in the caller's snapshot. It never scans an event table or
// turns policy days into a coverage claim. The owner refreshes this projection
// after writes and before retention publication; a dirty projection is
// explicitly stale and incomplete.
func LoadActualCoverageProjection(ctx context.Context, exec retentionSourceExecutor, source RetentionFloorEpochSource) (ActualCoverageProjection, error) {
	var owner string
	switch source.Domain {
	case "request_logs":
		owner = "Requests"
	case "usage_request_events":
		owner = "Observe"
	case "loadbalance_events":
		owner = "Observe"
	case "audit_logs":
		owner = "Requests/Audit"
	default:
		return ActualCoverageProjection{}, fmt.Errorf("unsupported retention coverage domain %q", source.Domain)
	}
	var row struct {
		Earliest           *time.Time
		Latest             *time.Time
		Revision           string
		Hash               string
		SourceRevision     string
		GeneratedAt        *time.Time
		Gaps               []byte
		MaterializationCut []byte
		Precision          string
		Complete           bool
		Freshness          string
		Dirty              bool
	}
	if err := exec.QueryRow(ctx, `SELECT earliest_retained_at, latest_retained_at,
		coverage_revision, coverage_hash, source_revision, materialized_at, gaps, materialization_cut, precision, complete, freshness, dirty
		FROM retention_coverage_read_models WHERE dataset = $1`, source.Domain).
		Scan(&row.Earliest, &row.Latest, &row.Revision, &row.Hash, &row.SourceRevision, &row.GeneratedAt, &row.Gaps, &row.MaterializationCut, &row.Precision, &row.Complete, &row.Freshness, &row.Dirty); err != nil {
		return ActualCoverageProjection{}, fmt.Errorf("load %s actual coverage owner model: %w", source.Domain, err)
	}
	gaps := []map[string]any{}
	if len(row.Gaps) > 0 && string(row.Gaps) != "null" {
		if err := json.Unmarshal(row.Gaps, &gaps); err != nil {
			return ActualCoverageProjection{}, fmt.Errorf("decode %s actual coverage gaps: %w", source.Domain, err)
		}
	}
	materializationCut := map[string]any{}
	if len(row.MaterializationCut) > 0 && string(row.MaterializationCut) != "null" {
		if err := json.Unmarshal(row.MaterializationCut, &materializationCut); err != nil {
			return ActualCoverageProjection{}, fmt.Errorf("decode %s materialization cut: %w", source.Domain, err)
		}
	}
	projection := ActualCoverageProjection{
		Domain:             source.Domain,
		Earliest:           row.Earliest,
		Latest:             row.Latest,
		Revision:           row.Revision,
		Hash:               row.Hash,
		GeneratedAt:        row.GeneratedAt,
		Source:             owner + ".actual_coverage",
		Precision:          row.Precision,
		Complete:           row.Complete && !row.Dirty && source.PurgeState != "running" && source.PurgeState != "recovery_required",
		Freshness:          row.Freshness,
		Gaps:               gaps,
		MaterializationCut: materializationCut,
	}
	coverageIdentityReady := projection.Revision != "" && projection.Hash != "" && projection.GeneratedAt != nil
	if !coverageIdentityReady {
		projection.Complete = false
		if projection.Freshness == "fresh" {
			projection.Freshness = "unavailable"
		}
	}
	if row.SourceRevision != "" && row.SourceRevision != source.SourceRevision {
		// A policy/floor/fence transition changes the owner source revision even
		// when no base-table row changed. The previous read model is still useful
		// as bounded diagnostic evidence, but it cannot authorize a new context
		// or destructive preflight under the newer owner generation.
		projection.Complete = false
		projection.Freshness = "stale"
		projection.Precision = "unavailable"
		reason := "coverage_source_revision_changed"
		projection.GapReason = &reason
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		projection.Freshness = "stale"
		projection.Complete = false
	}
	if row.Dirty && projection.Freshness == "fresh" {
		projection.Freshness = "stale"
	}
	if row.Earliest == nil || row.Latest == nil {
		if !projection.Complete || projection.Freshness != "fresh" {
			projection.Precision = "unavailable"
			if projection.Freshness != "stale" {
				projection.Freshness = "unavailable"
			}
		}
		reason := "no_retained_intersection"
		projection.GapReason = &reason
	}
	return projection, nil
}

// RefreshActualCoverageProjection is the owner maintenance operation for the
// read model above. It is called by the retention owner inside the same
// transaction that publishes a new floor/epoch. The bounded table aggregate
// lives here, at the owner boundary; Settings and query consumers only read
// the resulting projection.
func RefreshActualCoverageProjection(ctx context.Context, exec retentionSourceWriter, source RetentionFloorEpochSource, now time.Time) error {
	var table string
	switch source.Domain {
	case "request_logs":
		table = "request_logs"
	case "usage_request_events":
		table = "usage_request_events"
	case "loadbalance_events":
		table = "loadbalance_events"
	case "audit_logs":
		table = "audit_logs"
	default:
		return fmt.Errorf("unsupported retention coverage refresh domain %q", source.Domain)
	}
	effectiveFloor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (effectiveFloor == nil || source.PublishedFloor.After(*effectiveFloor)) {
		effectiveFloor = source.PublishedFloor
	}
	var earliest, latest *time.Time
	var committedCut *time.Time
	if err := exec.QueryRow(ctx, fmt.Sprintf(`SELECT MAX(created_at) FROM %s`, table)).Scan(&committedCut); err != nil {
		return fmt.Errorf("refresh %s materialization cut: %w", source.Domain, err)
	}
	query := fmt.Sprintf(`SELECT MIN(created_at), MAX(created_at) FROM %s
		WHERE created_at >= COALESCE($1::timestamptz, '-infinity'::timestamptz)`, table)
	if err := exec.QueryRow(ctx, query, effectiveFloor).Scan(&earliest, &latest); err != nil {
		return fmt.Errorf("refresh %s actual coverage: %w", source.Domain, err)
	}
	precision := "owner_bounds"
	freshness := "fresh"
	complete := earliest != nil && latest != nil && source.PurgeState != "running" && source.PurgeState != "recovery_required"
	gaps := []map[string]any{}
	if earliest == nil || latest == nil {
		// An owner aggregate that ran to completion can prove an empty
		// retained set. Keep that distinct from the initial dirty/unavailable
		// projection, so a fresh empty database does not fabricate a range but
		// can still pass the semantic-facts gate.
		complete = source.PurgeState != "running" && source.PurgeState != "recovery_required"
	}
	if earliest == nil || latest == nil {
		gaps = append(gaps, map[string]any{"from_time": nil, "to_time": nil, "reason": "no_retained_intersection"})
	}
	cut := now.UTC()
	if committedCut != nil {
		cut = committedCut.UTC()
	}
	materializationCut := map[string]any{}
	cutValue := cut.Format(time.RFC3339Nano)
	if source.Domain == "request_logs" {
		materializationCut = map[string]any{
			"kind":                  "request_visibility_cut",
			"request_committed_cut": cutValue,
		}
	} else {
		kind := "event_hybrid_cut"
		if source.Domain == "usage_request_events" {
			kind = "usage_hybrid_cut"
		}
		materializationCut = map[string]any{
			"kind":                kind,
			"raw_committed_cut":   cutValue,
			"rollup_manifest_cut": nil,
			"build_revision":      nil,
		}
	}
	materializationCutJSON, err := json.Marshal(materializationCut)
	if err != nil {
		return fmt.Errorf("encode %s materialization cut: %w", source.Domain, err)
	}
	gapsJSON, err := json.Marshal(gaps)
	if err != nil {
		return fmt.Errorf("encode %s actual coverage gaps: %w", source.Domain, err)
	}
	canonical := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s", source.SourceRevision, source.FenceGeneration,
		formatCoverageTime(earliest), formatCoverageTime(latest), precision, freshness, string(gapsJSON), string(materializationCutJSON))
	hash := sha256.Sum256([]byte(canonical))
	coverageRevision := hex.EncodeToString(hash[:])
	if _, err := exec.Exec(ctx, `UPDATE retention_coverage_read_models SET
		earliest_retained_at = $2, latest_retained_at = $3, precision = $4,
		coverage_revision = $8, coverage_hash = $9, gaps = $5::jsonb, complete = $6, freshness = $7,
		source_revision = $10, retention_generation = $11, dirty = false,
		materialization_cut = $12::jsonb, materialized_at = $13, updated_at = $13
		WHERE dataset = $1`, source.Domain, earliest, latest, precision, gapsJSON, complete, freshness,
		coverageRevision, coverageRevision, source.SourceRevision, parseGeneration(source.RetentionGeneration), materializationCutJSON, now.UTC()); err != nil {
		return fmt.Errorf("publish %s actual coverage: %w", source.Domain, err)
	}
	return nil
}

// RecordActualCoverageAppend advances an already-complete owner projection
// for an append-only write. The trigger on each retained table deliberately
// marks the projection dirty first; this method is the owning writer's
// same-transaction hand-off that clears that dirtiness only when the previous
// projection was complete and its source revision still matches. Unknown
// prior mutations remain dirty and are refreshed by the owner worker, so this
// helper cannot turn an incomplete read model into a fabricated range.
func RecordActualCoverageAppend(ctx context.Context, exec RetentionSourceWriter, domain string, appended []time.Time, now time.Time) error {
	if len(appended) == 0 {
		return nil
	}
	switch domain {
	case "request_logs", "audit_logs", "usage_request_events", "loadbalance_events":
	default:
		return fmt.Errorf("unsupported retention coverage append domain %q", domain)
	}
	if _, ok := exec.(pgx.Tx); !ok {
		if _, err := exec.Exec(ctx, `UPDATE retention_coverage_read_models SET
			freshness = 'stale', dirty = TRUE, updated_at = $2
			WHERE dataset = $1 AND dirty`, domain, now.UTC()); err != nil {
			return fmt.Errorf("defer non-transactional %s append coverage handoff: %w", domain, err)
		}
		return fmt.Errorf("%s append coverage handoff requires a database transaction", domain)
	}
	source, err := LoadRetentionSourceProjection(ctx, exec, domain, now.UTC())
	if err != nil {
		return err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		// The finalizer owns a stronger materialization cut while a purge is in
		// flight. Leave the model dirty and let that owner publish it.
		return nil
	}

	var row struct {
		Earliest       *time.Time
		Latest         *time.Time
		CoverageRev    string
		CoverageHash   string
		SourceRevision string
		Gaps           []byte
		Complete       bool
		Freshness      string
		Dirty          bool
		MutationInTx   bool
	}
	if err := exec.QueryRow(ctx, `SELECT earliest_retained_at, latest_retained_at,
		coverage_revision, coverage_hash, source_revision, gaps, complete, freshness, dirty,
		xmin = pg_current_xact_id()::xid
		FROM retention_coverage_read_models
		WHERE dataset = $1 FOR UPDATE`, domain).Scan(
		&row.Earliest, &row.Latest, &row.CoverageRev, &row.CoverageHash,
		&row.SourceRevision, &row.Gaps, &row.Complete, &row.Freshness, &row.Dirty, &row.MutationInTx); err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return fmt.Errorf("load %s append coverage owner model: %w", domain, err)
	}
	// A trigger sets dirty=true for the current append while leaving a prior
	// complete bit intact. A prior incomplete model is never repaired by an
	// append-only hint; its owner must rescan/materialize it.
	if !row.Dirty || !row.MutationInTx || !row.Complete || row.SourceRevision == "" || row.SourceRevision != source.SourceRevision || row.CoverageRev == "" || row.CoverageRev != row.CoverageHash || row.Freshness != "fresh" {
		return nil
	}

	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	var candidateEarliest, candidateLatest, rawLatest *time.Time
	for _, value := range appended {
		value = value.UTC()
		if rawLatest == nil || value.After(*rawLatest) {
			copyValue := value
			rawLatest = &copyValue
		}
		if floor != nil && value.Before(*floor) {
			continue
		}
		if candidateEarliest == nil || value.Before(*candidateEarliest) {
			copyValue := value
			candidateEarliest = &copyValue
		}
		if candidateLatest == nil || value.After(*candidateLatest) {
			copyValue := value
			candidateLatest = &copyValue
		}
	}
	earliest := minCoverageTime(row.Earliest, candidateEarliest)
	latest := maxCoverageTime(row.Latest, candidateLatest)
	cut := maxCoverageTime(row.Latest, rawLatest)
	if cut == nil {
		// An append entirely below the configured floor leaves an already
		// complete empty projection empty, but still records the owner cut.
		cutValue := now.UTC()
		cut = &cutValue
	}

	gaps := []map[string]any{}
	if len(row.Gaps) > 0 && string(row.Gaps) != "null" {
		if err := json.Unmarshal(row.Gaps, &gaps); err != nil {
			return fmt.Errorf("decode %s append coverage gaps: %w", domain, err)
		}
	}
	if earliest != nil && latest != nil {
		gaps = removeEmptyCoverageSentinel(gaps)
	}
	cutValue := cut.UTC().Format(time.RFC3339Nano)
	materializationCut := map[string]any{}
	if domain == "request_logs" {
		materializationCut = map[string]any{
			"kind":                  "request_visibility_cut",
			"request_committed_cut": cutValue,
		}
	} else {
		kind := "event_hybrid_cut"
		if domain == "usage_request_events" {
			kind = "usage_hybrid_cut"
		}
		materializationCut = map[string]any{
			"kind":                kind,
			"raw_committed_cut":   cutValue,
			"rollup_manifest_cut": nil,
			"build_revision":      nil,
		}
	}
	materializationCutJSON, err := json.Marshal(materializationCut)
	if err != nil {
		return fmt.Errorf("encode %s append materialization cut: %w", domain, err)
	}
	gapsJSON, err := json.Marshal(gaps)
	if err != nil {
		return fmt.Errorf("encode %s append coverage gaps: %w", domain, err)
	}
	canonical := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s", source.SourceRevision, source.FenceGeneration,
		formatCoverageTime(earliest), formatCoverageTime(latest), "owner_bounds", "fresh", string(gapsJSON), string(materializationCutJSON))
	hash := sha256.Sum256([]byte(canonical))
	coverageRevision := hex.EncodeToString(hash[:])
	if _, err := exec.Exec(ctx, `UPDATE retention_coverage_read_models SET
		earliest_retained_at = $2, latest_retained_at = $3, precision = 'owner_bounds',
		coverage_revision = $4, coverage_hash = $5, gaps = $6::jsonb, complete = TRUE,
		freshness = 'fresh', source_revision = $7, retention_generation = $8,
		dirty = FALSE, materialization_cut = $9::jsonb, materialized_at = $10, updated_at = $10
		WHERE dataset = $1`, domain, earliest, latest, coverageRevision, coverageRevision,
		gapsJSON, source.SourceRevision, parseGeneration(source.RetentionGeneration), materializationCutJSON, now.UTC()); err != nil {
		return fmt.Errorf("publish %s append coverage: %w", domain, err)
	}
	return nil
}

func removeEmptyCoverageSentinel(gaps []map[string]any) []map[string]any {
	retained := gaps[:0]
	for _, gap := range gaps {
		from, hasFrom := gap["from_time"]
		to, hasTo := gap["to_time"]
		if gap["reason"] == "no_retained_intersection" && hasFrom && from == nil && hasTo && to == nil {
			continue
		}
		retained = append(retained, gap)
	}
	return retained
}

func minCoverageTime(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right == nil || !right.Before(*left) {
		return left
	}
	return right
}

func maxCoverageTime(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right == nil || !right.After(*left) {
		return left
	}
	return right
}

func formatCoverageTime(value *time.Time) string {
	if value == nil {
		return "null"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseGeneration(value string) int64 {
	var generation int64
	if _, err := fmt.Sscan(value, &generation); err != nil || generation < 1 {
		return 1
	}
	return generation
}

// LoadRetentionSourceProjection reads the domain's retention source in the
// caller's snapshot (the caller must hold a shared fence / RR snapshot when
// the projection is used to authorize destructive work).
func LoadRetentionSourceProjection(ctx context.Context, exec retentionSourceExecutor, domain string, now time.Time) (RetentionFloorEpochSource, error) {
	var resource struct {
		PolicyGeneration         int64
		FenceGeneration          int64
		SettingsRevision         int64
		ConfiguredLogicalCutoff  *time.Time
		PublishedRetentionFloor  *time.Time
		RetentionRevocationEpoch int64
		PurgeState               string
		PhysicalReclaimState     string
		DesiredWorkIdentity      *string
		UpdatedAt                time.Time
	}
	err := exec.QueryRow(ctx, `SELECT policy_generation, fence_generation, settings_revision, configured_logical_cutoff,
		published_retention_floor, retention_revocation_epoch, purge_state,
		physical_reclaim_state, desired_work_identity, updated_at
		FROM log_retention_policy_resources WHERE dataset = $1`, domain).Scan(
		&resource.PolicyGeneration, &resource.FenceGeneration, &resource.SettingsRevision, &resource.ConfiguredLogicalCutoff,
		&resource.PublishedRetentionFloor, &resource.RetentionRevocationEpoch, &resource.PurgeState,
		&resource.PhysicalReclaimState, &resource.DesiredWorkIdentity, &resource.UpdatedAt)
	if err != nil {
		return RetentionFloorEpochSource{}, fmt.Errorf("load retention source %s: %w", domain, err)
	}

	cutoffFormatted := "null"
	if resource.ConfiguredLogicalCutoff != nil {
		cutoffFormatted = resource.ConfiguredLogicalCutoff.UTC().Format(time.RFC3339Nano)
	}
	floorFormatted := "null"
	if resource.PublishedRetentionFloor != nil {
		floorFormatted = resource.PublishedRetentionFloor.UTC().Format(time.RFC3339Nano)
	}
	desiredWork := "null"
	if resource.DesiredWorkIdentity != nil {
		desiredWork = *resource.DesiredWorkIdentity
	}
	canonical := fmt.Sprintf("%s|%d|%d|%d|%s|%s|%d|%s|%s|%s|%s",
		domain, resource.PolicyGeneration, resource.FenceGeneration, resource.SettingsRevision, cutoffFormatted,
		floorFormatted, resource.RetentionRevocationEpoch, resource.PurgeState,
		resource.PhysicalReclaimState, desiredWork, resource.UpdatedAt.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(canonical))

	return RetentionFloorEpochSource{
		ContractVersion:      1,
		Domain:               domain,
		SourceRevision:       hex.EncodeToString(sum[:]),
		RetentionEpoch:       fmt.Sprintf("%d", resource.RetentionRevocationEpoch),
		RetentionGeneration:  fmt.Sprintf("%d", resource.PolicyGeneration),
		FenceGeneration:      fmt.Sprintf("%d", resource.FenceGeneration),
		ConfiguredCutoff:     resource.ConfiguredLogicalCutoff,
		PublishedFloor:       resource.PublishedRetentionFloor,
		RevocationEpoch:      resource.RetentionRevocationEpoch,
		PurgeState:           resource.PurgeState,
		PhysicalReclaimState: resource.PhysicalReclaimState,
		DesiredWorkIdentity:  resource.DesiredWorkIdentity,
		UpdatedAt:            resource.UpdatedAt,
		GeneratedAt:          now.UTC(),
	}, nil
}
