package managementjobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

// errManualPreflightStale is deliberately private and non-diagnostic.  It is
// used to distinguish a sealed-intent mismatch from an infrastructure error so
// the worker can terminalize the job without retrying a destructive operation.
var errManualPreflightStale = errors.New("preflight_stale_before_execution")

type manualPreflightDomain struct {
	Dataset       string          `json:"dataset"`
	OwnerSnapshot json.RawMessage `json:"owner_snapshot"`
	Impact        struct {
		SemanticFactsComplete bool `json:"semantic_facts_complete"`
	} `json:"impact"`
}

// validateManualPreflightBeforeFence rechecks the sealed owner snapshot while
// the dataset resource row is already locked and before purge_state enters
// running.  This closes the worker-side gap between route acceptance and the
// irreversible execution fence: policy/floor/epoch/fence/materializer changes
// require a new fresh preflight, while append-only evidence may advance.
func (s *Store) validateManualPreflightBeforeFence(ctx context.Context, tx pgx.Tx, job retentionJobRow) error {
	if job.PreflightID == nil || strings.TrimSpace(*job.PreflightID) == "" {
		// Store-level compatibility callers and legacy tests can create a job
		// directly.  The Settings route always supplies a sealed preflight id.
		return nil
	}
	var affectedRaw []byte
	if err := tx.QueryRow(ctx, `SELECT affected_domains
		FROM log_retention_preflights WHERE id = $1`, *job.PreflightID).Scan(&affectedRaw); err != nil {
		return fmt.Errorf("%w: load sealed preflight", errManualPreflightStale)
	}
	var domains []manualPreflightDomain
	if err := json.Unmarshal(affectedRaw, &domains); err != nil || len(domains) != 1 {
		return fmt.Errorf("%w: invalid sealed preflight scope", errManualPreflightStale)
	}
	domain := domains[0]
	if domain.Dataset != job.Scope.Table || !domain.Impact.SemanticFactsComplete || len(domain.OwnerSnapshot) == 0 || string(domain.OwnerSnapshot) == "null" {
		return fmt.Errorf("%w: sealed preflight scope changed", errManualPreflightStale)
	}
	var sealed any
	if err := json.Unmarshal(domain.OwnerSnapshot, &sealed); err != nil {
		return fmt.Errorf("%w: invalid sealed owner snapshot", errManualPreflightStale)
	}
	current, err := s.currentManualOwnerSnapshot(ctx, tx, job.Scope.Table, s.now().UTC())
	if err != nil {
		return fmt.Errorf("%w: current owner snapshot unavailable", errManualPreflightStale)
	}
	if canonicalManualPreflightSemanticHash(sealed) != canonicalManualPreflightSemanticHash(current) {
		return fmt.Errorf("%w: sealed owner snapshot changed", errManualPreflightStale)
	}
	return nil
}

func (s *Store) currentManualOwnerSnapshot(ctx context.Context, tx pgx.Tx, dataset string, now time.Time) (any, error) {
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return nil, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return nil, fmt.Errorf("retention owner is fenced")
	}
	coverage, err := statsdomain.LoadActualCoverageProjection(ctx, tx, source)
	if err != nil || coverage.Revision == "" || coverage.Hash == "" || coverage.GeneratedAt == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("retention coverage owner identity unavailable")
	}
	coverageSnapshot := map[string]any{
		"domain":            coverage.Domain,
		"earliest":          manualFormatTimePtr(coverage.Earliest),
		"latest":            manualFormatTimePtr(coverage.Latest),
		"coverage_revision": coverage.Revision,
		"coverage_hash":     coverage.Hash,
		"generated_at":      manualFormatTimePtr(coverage.GeneratedAt),
		"source":            coverage.Source,
		"precision":         coverage.Precision,
		"complete":          coverage.Complete,
		"freshness":         coverage.Freshness,
		"gaps":              coverage.Gaps,
		"gap_reason":        coverage.GapReason,
	}
	if dataset == "audit_logs" {
		protection, err := auditdomain.LoadAuditFenceMaterializerProjection(ctx, tx, now)
		if err != nil {
			return nil, err
		}
		storageEvidence, err := manualAuditStorageFactEvidence(ctx, tx, source.SourceRevision)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":                  "audit",
			"dataset":               dataset,
			"contract_version":      3,
			"retention_source":      manualRetentionOwnerSourceMap(source),
			"audit_protection":      manualAuditProtectionMap(protection),
			"actual_coverage":       coverageSnapshot,
			"storage_fact_evidence": storageEvidence,
		}, nil
	}
	if !manualMaterializationCutValid(dataset, coverage.MaterializationCut) {
		return nil, fmt.Errorf("retention materialization cut unavailable")
	}
	return map[string]any{
		"kind":                       "observe",
		"contract_version":           1,
		"dataset":                    dataset,
		"policy_generation":          source.RetentionGeneration,
		"retention_revocation_epoch": source.RetentionEpoch,
		"configured_cutoff":          manualFormatTimePtr(source.ConfiguredCutoff),
		"published_floor":            manualFormatTimePtr(source.PublishedFloor),
		"fence_generation":           source.FenceGeneration,
		"purge_state":                manualNormalizedPurgeState(source.PurgeState),
		"coverage_revision":          coverage.Revision,
		"coverage_hash":              coverage.Hash,
		"generated_at":               manualFormatTimePtr(coverage.GeneratedAt),
		"actual_coverage":            coverageSnapshot,
		"materialization_cut":        coverage.MaterializationCut,
	}, nil
}

func manualRetentionOwnerSourceMap(source statsdomain.RetentionFloorEpochSource) map[string]any {
	return map[string]any{
		"contract_version":       source.ContractVersion,
		"domain":                 source.Domain,
		"source_revision":        source.SourceRevision,
		"retention_epoch":        source.RetentionEpoch,
		"retention_generation":   source.RetentionGeneration,
		"fence_generation":       source.FenceGeneration,
		"configured_cutoff":      manualFormatTimePtr(source.ConfiguredCutoff),
		"published_floor":        manualFormatTimePtr(source.PublishedFloor),
		"purge_state":            manualNormalizedPurgeState(source.PurgeState),
		"physical_reclaim_state": source.PhysicalReclaimState,
		"desired_work_identity":  source.DesiredWorkIdentity,
		"updated_at":             source.UpdatedAt.UTC().Format(time.RFC3339),
		"generated_at":           source.GeneratedAt.UTC().Format(time.RFC3339),
	}
}

func manualAuditProtectionMap(projection auditdomain.AuditFenceMaterializerProjection) map[string]any {
	return map[string]any{
		"contract_version":        projection.ContractVersion,
		"fence_generation":        projection.FenceGeneration,
		"reader_fence_state":      projection.ReaderFenceState,
		"materializer_generation": projection.MaterializerGeneration,
		"materializer_state":      projection.MaterializerState,
		"generated_at":            projection.GeneratedAt,
	}
}

func manualAuditStorageFactEvidence(ctx context.Context, tx pgx.Tx, sourceRevision string) (map[string]any, error) {
	var generation *string
	var complete bool
	if err := tx.QueryRow(ctx, `SELECT current_generation, facts_complete
		FROM audit_storage_fact_state WHERE id = 1`).Scan(&generation, &complete); err != nil {
		return nil, err
	}
	if generation == nil || !complete {
		return map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}, nil
	}
	var count int64
	var mismatched bool
	if err := tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(bool_or(observe_source_revision <> $2), FALSE)
		FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`, *generation, sourceRevision).Scan(&count, &mismatched); err != nil {
		return nil, err
	}
	if count == 0 {
		return map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}, nil
	}
	if mismatched {
		return map[string]any{"state": "unavailable", "reason_code": "source_revision_mismatch"}, nil
	}
	return map[string]any{"state": "bound", "generation": *generation}, nil
}

func manualNormalizedPurgeState(state string) string {
	if state == "published" || state == "rolled_back" {
		return "idle"
	}
	return state
}

func manualFormatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func manualMaterializationCutValid(dataset string, value map[string]any) bool {
	kind, ok := value["kind"].(string)
	if !ok {
		return false
	}
	validInstant := func(field string) bool {
		text, ok := value[field].(string)
		if !ok || strings.TrimSpace(text) == "" || !strings.HasSuffix(text, "Z") {
			return false
		}
		_, err := time.Parse(time.RFC3339Nano, text)
		return err == nil
	}
	optionalPair := func(left, right string) bool {
		leftValue, leftPresent := value[left]
		rightValue, rightPresent := value[right]
		if !leftPresent || !rightPresent {
			return false
		}
		if leftValue == nil || rightValue == nil {
			return leftValue == nil && rightValue == nil
		}
		_, leftOK := leftValue.(string)
		_, rightOK := rightValue.(string)
		return leftOK && rightOK
	}
	switch dataset {
	case "request_logs":
		return kind == "request_visibility_cut" && len(value) == 2 && validInstant("request_committed_cut")
	case "usage_request_events":
		return kind == "usage_hybrid_cut" && len(value) == 4 && validInstant("raw_committed_cut") && optionalPair("rollup_manifest_cut", "build_revision")
	case "loadbalance_events":
		return kind == "event_hybrid_cut" && len(value) == 4 && validInstant("raw_committed_cut") && optionalPair("rollup_manifest_cut", "build_revision")
	default:
		return false
	}
}

func canonicalManualPreflightSemanticHash(value any) string {
	raw, err := json.Marshal(stripManualPreflightPreviewEvidence(value))
	if err != nil {
		sum := sha256.Sum256([]byte(fmt.Sprintf("marshal-error:%T", value)))
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func stripManualPreflightPreviewEvidence(value any) any {
	switch item := value.(type) {
	case map[string]any:
		copyObject := make(map[string]any, len(item))
		for key, child := range item {
			switch key {
			case "generated_at", "updated_at", "coverage_revision", "coverage_hash", "materialization_cut", "earliest", "latest", "gaps":
				continue
			}
			if key == "actual_coverage" {
				if coverage, ok := child.(map[string]any); ok {
					semanticCoverage := map[string]any{}
					for coverageKey, coverageValue := range coverage {
						switch coverageKey {
						case "complete", "freshness", "precision", "source", "gap_reason":
							semanticCoverage[coverageKey] = stripManualPreflightPreviewEvidence(coverageValue)
						}
					}
					copyObject[key] = semanticCoverage
					continue
				}
			}
			if key == "storage_fact_evidence" {
				if evidence, ok := child.(map[string]any); ok {
					bounded := map[string]any{"state": evidence["state"]}
					if evidence["state"] == "bound" {
						bounded["generation"] = evidence["generation"]
					}
					copyObject[key] = bounded
					continue
				}
			}
			copyObject[key] = stripManualPreflightPreviewEvidence(child)
		}
		return copyObject
	case []any:
		copyArray := make([]any, len(item))
		for index, child := range item {
			copyArray[index] = stripManualPreflightPreviewEvidence(child)
		}
		return copyArray
	default:
		return value
	}
}

// failManualPreflightBeforeExecution is terminal by design.  Retrying a job
// whose sealed owner state changed would turn a missing confirmation into a
// moving destructive target; the operator must obtain a new preflight.
func (s *Store) failManualPreflightBeforeExecution(ctx context.Context, job retentionJobRow) error {
	_, err := s.pool.Exec(ctx, `UPDATE management_jobs SET
		state = 'failed', terminal_disposition = 'failed', stage = 'finished',
		error_code = 'preflight_stale_before_execution',
		error_message = 'sealed owner state changed; a new preflight is required',
		finished_at = now(), locked_by = NULL, locked_until = NULL,
		last_heartbeat_at = now(), updated_at = now()
		WHERE id = $1 AND state IN ('queued','running')`, job.ID)
	if err != nil {
		return fmt.Errorf("terminalize stale retention preflight: %w", err)
	}
	_ = s.appendEvent(ctx, job.ID, "preflight_stale_before_execution", "sealed owner state changed; new preflight required", 0)
	return fmt.Errorf("%w: sealed owner state changed", errManualPreflightStale)
}
