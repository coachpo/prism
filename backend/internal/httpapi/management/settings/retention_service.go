package settings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
)

type corsSnapshot = platformcors.Snapshot

// retentionService implements the retention policy, destructive preflight,
// owner-drift archive and manual job surfaces (Settings SPEC §5/§6).
type retentionService struct {
	pool *pgxpool.Pool
	now  func() time.Time
	jobs *managementjobs.Store
}

type retentionRow struct {
	RequestLogsRetentionDays       *int
	AuditLogsRetentionDays         *int
	StatisticsRetentionDays        *int
	LoadbalanceEventsRetentionDays *int
	Revision                       int64
	UpdatedAt                      time.Time
}

type policyResourceRow struct {
	PolicyGeneration        int64
	FenceGeneration         int64
	SettingsRevision        int64
	ConfiguredLogicalCutoff *time.Time
	PublishedRetentionFloor *time.Time
	RevocationEpoch         int64
	PurgeState              string
}

func loadRetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (retentionRow, error) {
	return scanRetentionRow(ctx, exec, false)
}

func loadRetentionRowForUpdate(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (retentionRow, error) {
	return scanRetentionRow(ctx, exec, true)
}

func scanRetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, forUpdate bool) (retentionRow, error) {
	var row retentionRow
	var requestLogs, auditLogs, statistics, loadbalance *int32
	query := `SELECT request_logs_retention_days, audit_logs_retention_days,
		statistics_retention_days, loadbalance_events_retention_days, revision, updated_at
		FROM log_retention_settings WHERE singleton_key = 'global'`
	if forUpdate {
		query += " FOR UPDATE"
	}
	err := exec.QueryRow(ctx, query).
		Scan(&requestLogs, &auditLogs, &statistics, &loadbalance, &row.Revision, &row.UpdatedAt)
	if err != nil {
		return retentionRow{}, err
	}
	row.RequestLogsRetentionDays = nullableInt32ToInt(requestLogs)
	row.AuditLogsRetentionDays = nullableInt32ToInt(auditLogs)
	row.StatisticsRetentionDays = nullableInt32ToInt(statistics)
	row.LoadbalanceEventsRetentionDays = nullableInt32ToInt(loadbalance)
	return row, nil
}

func nullableInt32ToInt(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func scanPolicyResources(ctx context.Context, exec interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[string]policyResourceRow, error) {
	rows, err := exec.Query(ctx, `SELECT dataset, policy_generation, fence_generation, settings_revision,
		configured_logical_cutoff, published_retention_floor, retention_revocation_epoch, purge_state
		FROM log_retention_policy_resources`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]policyResourceRow{}
	for rows.Next() {
		var dataset string
		var row policyResourceRow
		if err := rows.Scan(&dataset, &row.PolicyGeneration, &row.FenceGeneration, &row.SettingsRevision, &row.ConfiguredLogicalCutoff,
			&row.PublishedRetentionFloor, &row.RevocationEpoch, &row.PurgeState); err != nil {
			return nil, err
		}
		result[dataset] = row
	}
	return result, rows.Err()
}

// taggedPolicyValue renders a stored integer as the canonical tagged read
// union (valid value or repair_required with exact raw integer).
func taggedPolicyValue(value *int) retentionPolicyReadValue {
	if value == nil {
		return retentionPolicyReadValue{State: "valid", Value: nil}
	}
	if *value >= 1 && *value <= retentionMaxDays {
		return retentionPolicyReadValue{State: "valid", Value: value}
	}
	raw := fmt.Sprintf("%d", *value)
	issue := "retention_days_above_supported_max"
	return retentionPolicyReadValue{State: "repair_required", RawInteger: &raw, Issue: &issue}
}

// isDestructiveTransition classifies a before/after policy change (SPEC §5.2).
func isDestructiveTransition(before *int, after *int) bool {
	if before == nil && after != nil {
		return true // NULL -> N enables scheduled logical cleanup
	}
	if before != nil && after != nil && *after < *before {
		return true // shortening
	}
	return false
}

func policyFieldValue(policies retentionPolicies, dataset string) *int {
	switch dataset {
	case retentionDatasetRequestLogs:
		return policies.RequestLogsRetentionDays
	case retentionDatasetAuditLogs:
		return policies.AuditLogsRetentionDays
	case retentionDatasetUsageRequestEvents:
		return policies.StatisticsRetentionDays
	default:
		return policies.LoadbalanceEventsRetentionDays
	}
}

func policyFieldForRow(row retentionRow, dataset string) *int {
	switch dataset {
	case retentionDatasetRequestLogs:
		return row.RequestLogsRetentionDays
	case retentionDatasetAuditLogs:
		return row.AuditLogsRetentionDays
	case retentionDatasetUsageRequestEvents:
		return row.StatisticsRetentionDays
	default:
		return row.LoadbalanceEventsRetentionDays
	}
}

func (s *retentionService) buildSettingsResponse(ctx context.Context, tx pgx.Tx, serverNow time.Time) (logRetentionSettingsResponse, error) {
	row, err := loadRetentionRow(ctx, tx)
	if err != nil {
		return logRetentionSettingsResponse{}, err
	}
	resources, err := scanPolicyResources(ctx, tx)
	if err != nil {
		return logRetentionSettingsResponse{}, err
	}

	response := logRetentionSettingsResponse{
		State:     "ready",
		Scope:     "instance",
		Revision:  fmt.Sprintf("%d", row.Revision),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
		ServerNow: serverNow.UTC().Format(time.RFC3339),
		Policies: retentionPolicies{
			RequestLogsRetentionDays:       row.RequestLogsRetentionDays,
			AuditLogsRetentionDays:         row.AuditLogsRetentionDays,
			StatisticsRetentionDays:        row.StatisticsRetentionDays,
			LoadbalanceEventsRetentionDays: row.LoadbalanceEventsRetentionDays,
		},
		Recommendations:          []retentionRecommendation{balancedV1Recommendation()},
		PolicyGeneration:         map[string]string{},
		ConfiguredLogicalCutoffs: map[string]*string{},
		PublishedRetentionFloors: map[string]*string{},
		RetentionSourceRevision:  map[string]string{},
		ActualCoverage:           map[string]retentionCoverageSummary{},
		Protection:               map[string]any{},
	}

	ownerSources := map[string]statsdomain.RetentionFloorEpochSource{}
	ownerSourceErrors := map[string]error{}
	for index, dataset := range retentionDatasets {
		var source statsdomain.RetentionFloorEpochSource
		sourceErr := withSettingsReadSavepoint(ctx, tx, fmt.Sprintf("settings_owner_source_%d", index), func() error {
			var err error
			source, err = statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, serverNow)
			return err
		})
		if sourceErr != nil {
			ownerSourceErrors[dataset] = sourceErr
			continue
		}
		ownerSources[dataset] = source
	}

	// A legacy value above 36500 enters the repair union for its field.
	repair := false
	for _, dataset := range retentionDatasets {
		value := policyFieldForRow(row, dataset)
		if value != nil && (*value < 1 || *value > retentionMaxDays) {
			repair = true
		}
		resource, ok := resources[dataset]
		if !ok {
			return logRetentionSettingsResponse{}, fmt.Errorf("retention resource %s is unavailable", dataset)
		}
		response.PolicyGeneration[dataset] = fmt.Sprintf("%d", resource.PolicyGeneration)
		response.ConfiguredLogicalCutoffs[dataset] = formatTimePtr(resource.ConfiguredLogicalCutoff)
		response.PublishedRetentionFloors[dataset] = formatTimePtr(resource.PublishedRetentionFloor)
	}
	if repair {
		response.State = "repair_required"
		response.RepairPreflightURL = "/api/maintenance/log-retention/preflights"
		response.Policies = retentionPolicyReadPolicies{
			RequestLogsRetentionDays:       taggedPolicyValue(row.RequestLogsRetentionDays),
			AuditLogsRetentionDays:         taggedPolicyValue(row.AuditLogsRetentionDays),
			StatisticsRetentionDays:        taggedPolicyValue(row.StatisticsRetentionDays),
			LoadbalanceEventsRetentionDays: taggedPolicyValue(row.LoadbalanceEventsRetentionDays),
		}
	}

	// Retention source projections (Observe-owned, single source of truth).
	for index, domain := range retentionDatasets {
		if sourceErr := ownerSourceErrors[domain]; sourceErr != nil {
			code := "retention_source_unavailable"
			response.RetentionSourceRevision[domain] = ""
			response.ActualCoverage[domain] = retentionCoverageSummary{
				ToTime:           nil,
				CoverageRevision: "",
				CoverageHash:     "",
				Source:           "owner_unavailable",
				Precision:        "unavailable",
				Gaps:             []any{},
				Freshness:        "error",
				SourceRevision:   "",
				ErrorCode:        &code,
			}
			continue
		}
		source := ownerSources[domain]
		response.RetentionSourceRevision[domain] = source.SourceRevision
		var coverage statsdomain.ActualCoverageProjection
		coverageErr := withSettingsReadSavepoint(ctx, tx, fmt.Sprintf("settings_owner_coverage_%d", index), func() error {
			var err error
			coverage, err = statsdomain.LoadActualCoverageProjection(ctx, tx, source)
			return err
		})
		if coverageErr != nil {
			code := "coverage_unavailable"
			response.ActualCoverage[domain] = retentionCoverageSummary{
				ToTime:              nil,
				CoverageRevision:    coverage.Revision,
				CoverageHash:        coverage.Hash,
				GeneratedAt:         formatTimePtr(coverage.GeneratedAt),
				Source:              "owner_unavailable",
				Precision:           "unavailable",
				Gaps:                []any{},
				Freshness:           "error",
				SourceRevision:      source.SourceRevision,
				RetentionEpoch:      source.RetentionEpoch,
				RetentionGeneration: source.RetentionGeneration,
				PurgeState:          source.PurgeState,
				ErrorCode:           &code,
			}
			continue
		}
		gaps := make([]any, 0, len(coverage.Gaps)+1)
		for _, gap := range coverage.Gaps {
			gaps = append(gaps, gap)
		}
		if coverage.GapReason != nil {
			gaps = append(gaps, map[string]any{
				"from_time": coverage.Earliest,
				"to_time":   serverNow.UTC().Format(time.RFC3339),
				"reason":    *coverage.GapReason,
			})
		}
		response.ActualCoverage[domain] = retentionCoverageSummary{
			FromTime:            formatTimePtr(coverage.Earliest),
			ToTime:              formatTimeValue(coverage.Latest),
			CoverageRevision:    coverage.Revision,
			CoverageHash:        coverage.Hash,
			GeneratedAt:         formatTimePtr(coverage.GeneratedAt),
			Source:              coverage.Source,
			Precision:           coveragePrecision(coverage.Precision, coverage.Complete, coverage.Freshness),
			Gaps:                gaps,
			Complete:            coverage.Complete,
			Freshness:           coverage.Freshness,
			SourceRevision:      source.SourceRevision,
			RetentionEpoch:      source.RetentionEpoch,
			RetentionGeneration: source.RetentionGeneration,
			PurgeState:          source.PurgeState,
		}
	}

	// Protection projections: the three Observe domains carry the token TTL +
	// grace window; audit embeds its own fence projection with no fixed TTL.
	for _, dataset := range []string{retentionDatasetRequestLogs, retentionDatasetUsageRequestEvents, retentionDatasetLoadbalanceEvents} {
		if ownerSourceErrors[dataset] != nil {
			response.Protection[dataset] = map[string]any{"kind": "observe_query_token", "state": "unavailable", "reason_code": "retention_source_unavailable"}
			continue
		}
		source := ownerSources[dataset]
		var reclaimNotBefore *string
		_ = withSettingsReadSavepoint(ctx, tx, fmt.Sprintf("settings_owner_protection_%d", datasetProtectionSavepointIndex(dataset)), func() error {
			reclaimNotBefore = s.physicalReclaimNotBefore(ctx, tx, dataset)
			return nil
		})
		response.Protection[dataset] = observeTokenProtection{
			Kind:                     "observe_query_token",
			TokenTTLSeconds:          managementjobs.ObserveTokenTTLSeconds(),
			ExtraGraceSeconds:        managementjobs.ObserveProtectionGraceSeconds(),
			PhysicalReclaimNotBefore: reclaimNotBefore,
			SourceRevision:           source.SourceRevision,
			RetentionEpoch:           source.RetentionEpoch,
			RetentionGeneration:      source.RetentionGeneration,
			PurgeState:               source.PurgeState,
		}
	}
	auditSource := ownerSources[retentionDatasetAuditLogs]
	if ownerSourceErrors[retentionDatasetAuditLogs] != nil {
		response.Protection[retentionDatasetAuditLogs] = map[string]any{"kind": "audit_retention_fence", "state": "unavailable", "reason_code": "retention_source_unavailable"}
	} else {
		var protection auditdomain.AuditFenceMaterializerProjection
		protectionErr := withSettingsReadSavepoint(ctx, tx, "settings_audit_protection", func() error {
			var err error
			protection, err = auditProtectionProjection(ctx, tx, serverNow)
			return err
		})
		if protectionErr != nil {
			response.Protection[retentionDatasetAuditLogs] = map[string]any{"kind": "audit_retention_fence", "state": "unavailable", "reason_code": "audit_protection_unavailable"}
		} else {
			response.Protection[retentionDatasetAuditLogs] = auditFenceProtection{
				Kind:            "audit_retention_fence",
				ContractVersion: 3,
				RetentionSource: retentionSourceProjectionMap(auditSource),
				AuditProtection: protection,
			}
		}
	}

	// Owner-drift inventory (current heads only).
	inventory, err := s.loadOwnerDriftInventory(ctx, tx)
	if err != nil {
		return logRetentionSettingsResponse{}, err
	}
	response.OwnerDriftInventory = inventory
	return response, nil
}

func formatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func formatTimeValue(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func retentionSourceProjectionMap(source statsdomain.RetentionFloorEpochSource) map[string]any {
	return map[string]any{
		"contract_version":       source.ContractVersion,
		"domain":                 source.Domain,
		"source_revision":        source.SourceRevision,
		"retention_epoch":        source.RetentionEpoch,
		"retention_generation":   source.RetentionGeneration,
		"fence_generation":       source.FenceGeneration,
		"configured_cutoff":      formatTimePtr(source.ConfiguredCutoff),
		"published_floor":        formatTimePtr(source.PublishedFloor),
		"purge_state":            source.PurgeState,
		"physical_reclaim_state": source.PhysicalReclaimState,
		"desired_work_identity":  source.DesiredWorkIdentity,
		"updated_at":             source.UpdatedAt.UTC().Format(time.RFC3339),
		"generated_at":           source.GeneratedAt.UTC().Format(time.RFC3339),
	}
}

func retentionOwnerSourceProjectionMap(source statsdomain.RetentionFloorEpochSource) map[string]any {
	projection := retentionSourceProjectionMap(source)
	// `published` is the durable job result, not an in-flight reader state. The
	// Observe preflight union exposes the safe steady-state as idle while the
	// source revision/epoch/fence still carry the publication identity.
	if source.PurgeState == "published" || source.PurgeState == "rolled_back" {
		projection["purge_state"] = "idle"
	}
	return projection
}

func auditProtectionProjection(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, now time.Time) (auditdomain.AuditFenceMaterializerProjection, error) {
	return auditdomain.LoadAuditFenceMaterializerProjection(ctx, exec, now)
}

func auditStorageFactEvidence(ctx context.Context, tx pgx.Tx, sourceRevision string) map[string]any {
	result := map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}
	_ = withSettingsReadSavepoint(ctx, tx, "settings_audit_storage_facts", func() error {
		var err error
		result, err = auditStorageFactEvidenceRead(ctx, tx, sourceRevision)
		return err
	})
	return result
}

func auditStorageFactEvidenceRead(ctx context.Context, tx pgx.Tx, sourceRevision string) (map[string]any, error) {
	var generation *string
	var complete bool
	if err := tx.QueryRow(ctx, `SELECT current_generation, facts_complete
		FROM audit_storage_fact_state WHERE id = 1`).Scan(&generation, &complete); err != nil {
		return map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}, err
	}
	if generation == nil || !complete {
		return map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}, nil
	}
	var factCount int64
	var mismatched bool
	if err := tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(bool_or(observe_source_revision <> $2), FALSE)
		FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`, *generation, sourceRevision).
		Scan(&factCount, &mismatched); err != nil {
		return map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}, err
	}
	if factCount == 0 {
		return map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}, nil
	}
	if mismatched {
		return map[string]any{"state": "unavailable", "reason_code": "source_revision_mismatch"}, nil
	}
	return map[string]any{"state": "bound", "generation": *generation}, nil
}

// physicalReclaimNotBefore returns the persisted protection evidence for the
// newest nonterminal automatic job. It never derives a deadline from a UI
// request timestamp; rows without evidence stay unavailable.
func (s *retentionService) physicalReclaimNotBefore(ctx context.Context, tx pgx.Tx, dataset string) *string {
	var deadline *time.Time
	err := tx.QueryRow(ctx, `SELECT MAX(NULLIF(progress_json->'protection'->>'deadline', '')::timestamptz)
		FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'automatic'
		  AND resource_key = $1 AND state IN ('queued','running','cancel_requested')`, dataset).Scan(&deadline)
	if err != nil || deadline == nil {
		return nil
	}
	formatted := deadline.UTC().Format(time.RFC3339)
	return &formatted
}

// withSettingsReadSavepoint makes an owner projection genuinely optional. A
// transiently unavailable owner table/read model must become an explicit
// unavailable projection, not abort the surrounding Settings response
// transaction and turn a later diagnostic query into 25P02.
func withSettingsReadSavepoint(ctx context.Context, tx pgx.Tx, name string, fn func() error) error {
	if _, err := tx.Exec(ctx, "SAVEPOINT "+name); err != nil {
		return err
	}
	err := fn()
	if err != nil {
		if _, rollbackErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name); rollbackErr != nil {
			return fmt.Errorf("rollback settings read savepoint %s: %w (original: %v)", name, rollbackErr, err)
		}
		if _, releaseErr := tx.Exec(ctx, "RELEASE SAVEPOINT "+name); releaseErr != nil {
			return fmt.Errorf("release settings read savepoint %s: %w (original: %v)", name, releaseErr, err)
		}
		return err
	}
	_, err = tx.Exec(ctx, "RELEASE SAVEPOINT "+name)
	return err
}

func datasetProtectionSavepointIndex(dataset string) int {
	switch dataset {
	case retentionDatasetRequestLogs:
		return 0
	case retentionDatasetUsageRequestEvents:
		return 1
	case retentionDatasetLoadbalanceEvents:
		return 2
	default:
		return 99
	}
}

func (s *retentionService) loadOwnerDriftInventory(ctx context.Context, tx pgx.Tx) (*retentionOwnerDriftInventory, error) {
	var inventoryGeneration string
	var generatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT inventory_generation, updated_at FROM settings_owner_drift_inventory WHERE id = 1`).
		Scan(&inventoryGeneration, &generatedAt)
	if err != nil {
		return nil, fmt.Errorf("load owner drift inventory: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT head_id, lineage_generation, predecessor_head_id, field, evidence_hash,
		instance_value, legacy_copy_value, resolution_state, generated_at, resolved_at
		FROM settings_migration_evidence WHERE is_current ORDER BY field ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	heads := []retentionOwnerDriftHead{}
	state := "resolved"
	for rows.Next() {
		head := retentionOwnerDriftHead{}
		var instanceRaw, legacyRaw []byte
		var predecessor *string
		var generatedAt time.Time
		var resolvedAt *time.Time
		if err := rows.Scan(&head.HeadID, &head.LineageGeneration, &predecessor, &head.Field, &head.EvidenceHash,
			&instanceRaw, &legacyRaw, &head.ResolutionState, &generatedAt, &resolvedAt); err != nil {
			return nil, err
		}
		head.PredecessorHeadID = predecessor
		head.Resolution = "archive_legacy_copy_keep_instance_owner"
		head.GeneratedAt = generatedAt.UTC().Format(time.RFC3339)
		if resolvedAt != nil {
			formatted := resolvedAt.UTC().Format(time.RFC3339)
			head.ResolvedAt = &formatted
		}
		_ = json.Unmarshal(instanceRaw, &head.InstanceValue)
		_ = json.Unmarshal(legacyRaw, &head.LegacyCopyValue)
		if head.ResolutionState == "drift" {
			state = "action_required"
		}
		heads = append(heads, head)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &retentionOwnerDriftInventory{
		InventoryGeneration: inventoryGeneration,
		State:               state,
		CurrentHeads:        heads,
		GeneratedAt:         generatedAt.UTC().Format(time.RFC3339),
	}, nil
}

// ---- GET /api/settings/log-retention ----

func (s *retentionService) handleGetRetentionSettings(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "settings retention read", func(tx pgx.Tx) (logRetentionSettingsResponse, error) {
		return s.buildSettingsResponse(r.Context(), tx, s.now().UTC())
	})
	if err != nil {
		writeSettingsInternalError(w, r, corsSnapshot, err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

// ---- PUT /api/settings/log-retention ----

func (s *retentionService) handlePutRetentionSettings(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	var request putLogRetentionSettingsRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id is required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	violations := validateRetentionPolicies(request.Policies)
	if len(violations) > 0 {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "invalid_retention_policy", Detail: "invalid retention policy", Params: map[string]any{}, Details: map[string]any{"violations": violations}}, http.StatusUnprocessableEntity)
		return
	}

	var result putLogRetentionSettingsResult
	replayed := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings retention put", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		// Resolve the durable operation before touching a single-use preflight.
		// A lost response must be replayable after the original transaction has
		// consumed its token; checking the token first would turn a valid retry
		// into a false stale-preflight error.
		operation, operationErr := s.loadOperation(r.Context(), tx, "log_retention", request.OperationID)
		if operationErr == nil {
			hash := canonicalRequestHashForBody(request)
			if operation.RequestHash == hash {
				if operation.ResultJSON != nil {
					replayed = true
					return json.Unmarshal(operation.ResultJSON, &result)
				}
				return &settingsConflictError{code: "operation_outcome_unavailable", operationID: request.OperationID}
			}
			return &settingsConflictError{code: "operation_id_conflict", operationID: request.OperationID}
		}
		if operationErr != pgx.ErrNoRows {
			return operationErr
		}

		current, err := loadRetentionRowForUpdate(r.Context(), tx)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", current.Revision) != request.ExpectedRevision {
			return &settingsConflictError{code: "retention_settings_changed", currentRevision: fmt.Sprintf("%d", current.Revision)}
		}

		// Destructive classifier over the full four-field draft.
		destructiveDatasets := []string{}
		for _, dataset := range retentionDatasets {
			before := policyFieldForRow(current, dataset)
			after := policyFieldValue(request.Policies, dataset)
			if isDestructiveTransition(before, after) {
				destructiveDatasets = append(destructiveDatasets, dataset)
			}
		}
		if len(destructiveDatasets) > 0 {
			if request.PreflightToken == nil || strings.TrimSpace(*request.PreflightToken) == "" {
				return &settingsConflictError{code: "retention_preflight_required"}
			}
			if request.Confirmation == nil || strings.TrimSpace(request.Confirmation.Keyword) == "" {
				return &settingsConflictError{code: "retention_preflight_required"}
			}
			if err := s.consumePreflight(r.Context(), tx, *request.PreflightToken, request.OperationID, current.Revision, canonicalPolicyBindingHash(request.OperationID, request.ExpectedRevision, request.Policies), destructiveDatasets); err != nil {
				return err
			}
			if request.Confirmation.Keyword != "DELETE" {
				return &settingsConflictError{code: "retention_preflight_stale"}
			}
		}

		newRevision := current.Revision + 1
		now := s.now().UTC()

		// Owner-drift lineage: terminalize changed-field heads and append
		// post-commit successors (SPEC §14.1 item 12).
		if err := s.advanceOwnerDriftLineage(r.Context(), tx, current, request.Policies, now); err != nil {
			return err
		}

		// Atomic full replacement.
		if _, err := tx.Exec(r.Context(), `UPDATE log_retention_settings SET
			request_logs_retention_days = $1, audit_logs_retention_days = $2,
			statistics_retention_days = $3, loadbalance_events_retention_days = $4,
			revision = $5, updated_at = $6 WHERE singleton_key = 'global'`,
			request.Policies.RequestLogsRetentionDays, request.Policies.AuditLogsRetentionDays,
			request.Policies.StatisticsRetentionDays, request.Policies.LoadbalanceEventsRetentionDays,
			newRevision, now); err != nil {
			return err
		}

		// Advance policy resources + desired work for changed datasets.
		changes := []retentionChangeItem{}
		scheduledWork := []retentionScheduledWork{}
		for _, dataset := range retentionDatasets {
			before := policyFieldForRow(current, dataset)
			after := policyFieldValue(request.Policies, dataset)
			if intPtrsEqual(before, after) {
				continue
			}
			change := retentionChangeItem{
				Dataset:     dataset,
				Before:      taggedPolicyValue(before),
				AfterDays:   after,
				Destructive: isDestructiveTransition(before, after),
			}
			cutoff, work, err := s.applyPolicyResource(r.Context(), tx, dataset, before, after, newRevision, now)
			if err != nil {
				return err
			}
			change.LogicalCutoff = formatTimePtr(cutoff)
			if work != nil {
				scheduledWork = append(scheduledWork, *work)
			}
			changes = append(changes, change)
		}

		// Build the post-commit settings response.
		settingsResponse, err := s.buildSettingsResponse(r.Context(), tx, now)
		if err != nil {
			return err
		}
		result = putLogRetentionSettingsResult{
			Settings:      settingsResponse,
			Changes:       changes,
			ScheduledWork: scheduledWork,
			OperationID:   request.OperationID,
			Replayed:      false,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		hash := canonicalRequestHashForBody(request)
		return s.recordOperation(r.Context(), tx, "log_retention", request.OperationID, hash, raw)
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	result.Replayed = replayed
	writeSettingsJSON(w, http.StatusOK, result)
}

func validateRetentionPolicies(policies retentionPolicies) []FieldViolation {
	violations := []FieldViolation{}
	check := func(path string, value *int) {
		if value == nil {
			return
		}
		if *value < 1 || *value > retentionMaxDays {
			violations = append(violations, FieldViolation{Path: path, Reason: "must be an integer between 1 and 36500 or null", Limit: retentionMaxDays})
		}
	}
	check("policies.request_logs_retention_days", policies.RequestLogsRetentionDays)
	check("policies.audit_logs_retention_days", policies.AuditLogsRetentionDays)
	check("policies.statistics_retention_days", policies.StatisticsRetentionDays)
	check("policies.loadbalance_events_retention_days", policies.LoadbalanceEventsRetentionDays)
	return violations
}

func intPtrsEqual(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// advanceOwnerDriftLineage implements the per-field current-head lineage rule
// (SPEC §14.1 item 12): for every duplicated field whose authoritative
// instance value changes while the legacy column still exists, the old current
// head is terminalized with superseded_by_policy_change and exactly one
// successor is appended using the new instance value plus the unchanged legacy
// value; the inventory generation advances in the same commit. Unchanged-field
// heads remain current.
func (s *retentionService) advanceOwnerDriftLineage(ctx context.Context, tx pgx.Tx, current retentionRow, policies retentionPolicies, now time.Time) error {
	var inventoryGeneration string
	if err := tx.QueryRow(ctx, `SELECT inventory_generation FROM settings_owner_drift_inventory WHERE id = 1 FOR UPDATE`).Scan(&inventoryGeneration); err != nil {
		return err
	}
	for _, field := range []string{"request_logs_retention_days", "statistics_retention_days", "audit_logs_retention_days"} {
		var before *int
		var after *int
		switch field {
		case "request_logs_retention_days":
			before = current.RequestLogsRetentionDays
			after = policies.RequestLogsRetentionDays
		case "statistics_retention_days":
			before = current.StatisticsRetentionDays
			after = policies.StatisticsRetentionDays
		default:
			before = current.AuditLogsRetentionDays
			after = policies.AuditLogsRetentionDays
		}
		if intPtrsEqual(before, after) {
			continue
		}

		// Legacy copy value stays unchanged; instance value becomes `after`.
		var headID, evidenceHash, legacyRaw []byte
		if err := tx.QueryRow(ctx, `SELECT head_id, evidence_hash, legacy_copy_value
			FROM settings_migration_evidence WHERE field = $1 AND is_current FOR UPDATE`, field).
			Scan(&headID, &evidenceHash, &legacyRaw); err != nil {
			if err == pgx.ErrNoRows {
				continue
			}
			return err
		}
		instanceJSON, err := json.Marshal(taggedPolicyValue(after))
		if err != nil {
			return err
		}
		newGeneration := nextLineageGeneration(inventoryGeneration)
		newHeadID := canonicalHash("drift-head", field, newGeneration)
		newEvidenceHash := canonicalHash("gen", newGeneration, "field", field, "instance", string(instanceJSON), "legacy", string(legacyRaw))

		if _, err := tx.Exec(ctx, `UPDATE settings_migration_evidence SET
			is_current = FALSE, terminal_disposition = 'superseded_by_policy_change', resolved_at = $2
			WHERE field = $1 AND is_current`, field, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO settings_migration_evidence (
			head_id, lineage_generation, predecessor_head_id, field, evidence_hash,
			instance_value, legacy_copy_value, resolution_state, terminal_disposition,
			is_current, generated_at, resolved_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, TRUE, $9, NULL)`,
			newHeadID, newGeneration, string(headID), field, newEvidenceHash,
			instanceJSON, legacyRaw, equalTagged(instanceJSON, legacyRaw), now); err != nil {
			return err
		}
	}
	// Advance the inventory generation exactly once per policy commit.
	newGeneration := nextLineageGeneration(inventoryGeneration)
	if _, err := tx.Exec(ctx, `UPDATE settings_owner_drift_inventory SET
		inventory_generation = $1, updated_at = now() WHERE id = 1`, newGeneration); err != nil {
		return err
	}
	return nil
}

func nextLineageGeneration(current string) string {
	var number int
	_, _ = fmt.Sscanf(current, "%d", &number)
	return fmt.Sprintf("%d", number+1)
}

func equalTagged(instanceRaw, legacyRaw []byte) string {
	if string(instanceRaw) == string(legacyRaw) {
		return "converged"
	}
	return "drift"
}

// applyPolicyResource advances the per-dataset resource and creates durable
// desired work for destructive/enabling changes.
func (s *retentionService) applyPolicyResource(ctx context.Context, tx pgx.Tx, dataset string, before, after *int, settingsRevision int64, now time.Time) (*time.Time, *retentionScheduledWork, error) {
	var resource policyResourceRow
	err := tx.QueryRow(ctx, `SELECT policy_generation, fence_generation, settings_revision, configured_logical_cutoff,
		published_retention_floor, retention_revocation_epoch, purge_state
		FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, dataset).
		Scan(&resource.PolicyGeneration, &resource.FenceGeneration, &resource.SettingsRevision, &resource.ConfiguredLogicalCutoff,
			&resource.PublishedRetentionFloor, &resource.RevocationEpoch, &resource.PurgeState)
	if err != nil {
		return nil, nil, err
	}
	// A queued manual purge is already a sealed destructive reservation.  A
	// policy mutation must not invalidate that preflight between acceptance and
	// the worker's execution fence.  Running/recovery states are likewise
	// immutable until the owning job publishes or explicitly recovers them.
	if resource.PurgeState == "running" || resource.PurgeState == "recovery_required" {
		return nil, nil, &settingsConflictError{code: "retention_job_conflict"}
	}
	var manualReservation bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM management_jobs
		WHERE type = 'log_retention' AND contract_version = 2 AND profile_id = 0
		  AND origin = 'manual' AND resource_key = $1
		  AND state IN ('queued','running','cancel_requested')
	)`, dataset).Scan(&manualReservation); err != nil {
		return nil, nil, err
	}
	if manualReservation {
		return nil, nil, &settingsConflictError{code: "retention_job_conflict"}
	}
	var cutoff *time.Time
	if after != nil {
		value := utcDayCutoff(now, *after)
		cutoff = &value
	}
	newGeneration := resource.PolicyGeneration + 1
	if _, err := tx.Exec(ctx, `UPDATE log_retention_policy_resources SET
		policy_generation = $2, fence_generation = fence_generation + 1,
		settings_revision = $3, configured_logical_cutoff = $4, updated_at = now()
		WHERE dataset = $1`, dataset, newGeneration, settingsRevision, cutoff); err != nil {
		return nil, nil, err
	}
	// A policy/floor transition changes the owner source revision. Refresh its
	// actual-coverage materialization in this same transaction so a subsequent
	// preflight sees a coherent source/coverage pair rather than a transient
	// stale projection that Settings would have to synthesize or silently trust.
	ownerSource, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return nil, nil, err
	}
	if err := statsdomain.RefreshActualCoverageProjection(ctx, tx, ownerSource, now); err != nil {
		return nil, nil, err
	}

	// Only enabling cleanup (NULL -> N) or shortening (N -> smaller N) creates
	// destructive work. Extending or disabling a policy advances the owner
	// resource but must not manufacture a cleanup job for an older cutoff.
	work := (*retentionScheduledWork)(nil)
	// A newer policy generation supersedes every queued automatic intent for
	// this dataset before a replacement is created. Running work is only
	// cancel-requested; its worker must re-check the same generation fence at
	// its next irreversible checkpoint.
	if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
		state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE state END,
		terminal_disposition = CASE WHEN state = 'queued' THEN 'cancelled' ELSE NULL END,
		cancel_requested = TRUE,
		finished_at = CASE WHEN state = 'queued' THEN now() ELSE finished_at END,
		updated_at = now()
		WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'automatic'
		  AND resource_key = $1 AND state IN ('queued','running','cancel_requested')
		  AND COALESCE(policy_generation, 0) < $2`, dataset, newGeneration); err != nil {
		return nil, nil, err
	}
	if isDestructiveTransition(before, after) && after != nil && cutoff != nil {
		jobID, err := s.createScheduledJob(ctx, tx, dataset, *cutoff, settingsRevision, newGeneration, now)
		if err != nil {
			return nil, nil, err
		}
		work = &retentionScheduledWork{
			Dataset:          dataset,
			PolicyGeneration: fmt.Sprintf("%d", newGeneration),
			Disposition:      "created",
			JobID:            jobID,
		}
	}
	return cutoff, work, nil
}

func utcDayCutoff(now time.Time, retentionDays int) time.Time {
	utc := now.UTC()
	dayStart := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return dayStart.AddDate(0, 0, -retentionDays)
}

func (s *retentionService) createScheduledJob(ctx context.Context, tx pgx.Tx, dataset string, cutoff time.Time, settingsRevision int64, policyGeneration int64, now time.Time) (string, error) {
	jobID, err := s.jobs.CreateAutomaticRetentionJobTx(ctx, tx, dataset, cutoff, settingsRevision, policyGeneration, now)
	if err != nil {
		return "", err
	}
	return jobID, nil
}

// consumePreflight validates and consumes a single-use preflight token for a
// destructive mutation (SPEC §6.3): scope/revision/request binding plus the
// exact listed affected-domain subset.
func (s *retentionService) consumePreflight(ctx context.Context, tx pgx.Tx, token string, operationID string, settingsRevision int64, requestHash string, destructiveDatasets []string) error {
	tokenHash := hashToken(token)
	var preflight retentionPreflightRow
	err := tx.QueryRow(ctx, `SELECT id, kind, operation_id, preflight_attempt_id, token_hash, request_hash,
		settings_revision, principal_generation, affected_domains, expires_at, consumed_at
		FROM log_retention_preflights WHERE token_hash = $1 FOR UPDATE`, tokenHash).Scan(
		&preflight.ID, &preflight.Kind, &preflight.OperationID, &preflight.PreflightAttemptID, &preflight.TokenHash,
		&preflight.RequestHash, &preflight.SettingsRevision, &preflight.PrincipalGeneration, &preflight.AffectedDomains, &preflight.ExpiresAt, &preflight.ConsumedAt)
	if err == pgx.ErrNoRows {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	if err != nil {
		return err
	}
	if preflight.OperationID != operationID {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	if preflight.Kind != "policy_change" || preflight.RequestHash != requestHash {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	if preflight.SettingsRevision == nil || *preflight.SettingsRevision != settingsRevision {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	if preflight.PrincipalGeneration == nil || *preflight.PrincipalGeneration != managementPrincipalGenerationFromContext(ctx) {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	if preflight.ExpiresAt.Before(s.now().UTC()) {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	if preflight.ConsumedAt != nil {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	// The listed affected domains must match the destructive subset exactly.
	var affected []retentionAffectedDomain
	if err := json.Unmarshal(preflight.AffectedDomains, &affected); err != nil {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	domains := make([]string, 0, len(affected))
	for _, item := range affected {
		if !item.Impact.SemanticFactsComplete {
			// An unavailable owner projection may still be shown in a diagnostic
			// preview, but it can never authorize a destructive commit. Counts and
			// bytes are allowed to be unavailable; the owner semantic fence is not.
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		domains = append(domains, item.Dataset)
	}
	if !sameStringSet(domains, destructiveDatasets) {
		return &settingsConflictError{code: "retention_preflight_stale"}
	}
	// The preflight is also bound to the owner snapshots that explain the
	// affected scope. Re-read each owner under the same transaction before
	// consuming the token. Volatile preview timestamps are deliberately
	// excluded from the comparison; source/generation/fence/coverage facts
	// remain part of the binding and a changed owner forces a fresh preview.
	for _, affectedDomain := range affected {
		currentSnapshot, snapshotErr := s.ownerSnapshotFor(ctx, tx, affectedDomain.Dataset, s.now().UTC())
		if snapshotErr != nil {
			return snapshotErr
		}
		if canonicalOwnerSemanticSnapshotHash(affectedDomain.OwnerSnapshot) != canonicalOwnerSemanticSnapshotHash(currentSnapshot) {
			return &settingsConflictError{
				code:               "retention_preflight_stale",
				currentRevision:    fmt.Sprintf("%d", settingsRevision),
				currentGenerations: map[string]string{affectedDomain.Dataset: ownerSnapshotGeneration(currentSnapshot)},
			}
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE log_retention_preflights SET consumed_at = now(),
		consumed_operation_id = $2 WHERE id = $1`, preflight.ID, operationID); err != nil {
		return err
	}
	return nil
}

type retentionPreflightRow struct {
	ID                  string
	Kind                string
	OperationID         string
	PreflightAttemptID  string
	TokenHash           string
	RequestHash         string
	SettingsRevision    *int64
	PrincipalGeneration *string
	AffectedDomains     []byte
	ExpiresAt           time.Time
	ConsumedAt          *time.Time
}

func managementPrincipalGeneration(r *http.Request) string {
	return managementPrincipalGenerationFromContext(r.Context())
}

func managementPrincipalGenerationFromContext(ctx context.Context) string {
	snapshot, ok := requestcontext.ManagementPrincipalSnapshotFromContext(ctx)
	if !ok {
		// Auth-disabled management has no principal/session to bind. A mounted
		// auth-enabled request always carries the middleware snapshot above;
		// treating an absent snapshot as a distinct anonymous generation keeps a
		// direct handler invocation from replaying an authenticated preview.
		return "auth_disabled"
	}
	return canonicalHash("management-principal", snapshot.SubjectID, snapshot.TokenVersion, snapshot.AuthGeneration)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftMap := map[string]struct{}{}
	for _, item := range left {
		if _, exists := leftMap[item]; exists {
			return false
		}
		leftMap[item] = struct{}{}
	}
	rightMap := map[string]struct{}{}
	for _, item := range right {
		if _, exists := rightMap[item]; exists {
			return false
		}
		rightMap[item] = struct{}{}
		if _, ok := leftMap[item]; !ok {
			return false
		}
	}
	return true
}

// ---- owner-drift archive ----

func (s *retentionService) handleArchiveOwnerDrift(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	var request archiveRetentionOwnerDriftRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id is required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	if request.Acknowledgement != "keep_instance_policy_and_archive_legacy_copy" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "invalid acknowledgement", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}

	var result map[string]any
	replayed := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings owner drift archive", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		// Resolve the durable outcome before checking the mutable revision. A
		// response-loss retry must replay even after another safe Settings
		// mutation has advanced the revision.
		operation, operationErr := s.loadOperation(r.Context(), tx, "owner_drift_archive", request.OperationID)
		if operationErr == nil {
			hash := canonicalArchiveHash(request)
			if operation.RequestHash == hash {
				replayed = true
				if operation.ResultJSON != nil {
					return json.Unmarshal(operation.ResultJSON, &result)
				}
				return &settingsConflictError{code: "operation_outcome_unavailable", operationID: request.OperationID}
			}
			return &settingsConflictError{code: "operation_id_conflict", operationID: request.OperationID}
		}
		if operationErr != pgx.ErrNoRows {
			return operationErr
		}

		current, err := loadRetentionRow(r.Context(), tx)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", current.Revision) != request.ExpectedRevision {
			return &settingsConflictError{code: "retention_settings_changed", currentRevision: fmt.Sprintf("%d", current.Revision)}
		}
		var inventoryGeneration string
		if err := tx.QueryRow(r.Context(), `SELECT inventory_generation FROM settings_owner_drift_inventory WHERE id = 1`).Scan(&inventoryGeneration); err != nil {
			return err
		}
		if inventoryGeneration != request.ExpectedInventoryGeneration {
			return &settingsConflictError{code: "retention_owner_drift_changed"}
		}

		// Verify each requested head is an exact current drift head.
		if len(request.Heads) == 0 {
			return &settingsConflictError{code: "validation_failed"}
		}
		seenFields := map[string]struct{}{}
		archived := []map[string]any{}
		for _, head := range request.Heads {
			if _, exists := seenFields[head.Field]; exists {
				return &settingsConflictError{code: "validation_failed"}
			}
			seenFields[head.Field] = struct{}{}
			var currentHead retentionOwnerDriftHead
			var instanceRaw, legacyRaw []byte
			err := tx.QueryRow(r.Context(), `SELECT head_id, evidence_hash, instance_value, legacy_copy_value, resolution_state
				FROM settings_migration_evidence WHERE field = $1 AND is_current FOR UPDATE`, head.Field).
				Scan(&currentHead.HeadID, &currentHead.EvidenceHash, &instanceRaw, &legacyRaw, &currentHead.ResolutionState)
			if err != nil {
				return &settingsConflictError{code: "retention_owner_drift_changed"}
			}
			if currentHead.HeadID != head.HeadID || currentHead.EvidenceHash != head.EvidenceHash {
				return &settingsConflictError{code: "retention_owner_drift_changed"}
			}
			if currentHead.ResolutionState != "drift" {
				return &settingsConflictError{code: "retention_owner_drift_changed"}
			}
			if _, err := tx.Exec(r.Context(), `UPDATE settings_migration_evidence SET
				resolution_state = 'archived', resolved_at = now() WHERE head_id = $1`, head.HeadID); err != nil {
				return err
			}
			archived = append(archived, map[string]any{"field": head.Field, "head_id": head.HeadID})
		}
		if _, err := tx.Exec(r.Context(), `UPDATE settings_owner_drift_inventory SET updated_at = now() WHERE id = 1`); err != nil {
			return err
		}

		settingsResponse, err := s.buildSettingsResponse(r.Context(), tx, s.now().UTC())
		if err != nil {
			return err
		}
		result = map[string]any{
			"operation_id":         request.OperationID,
			"replayed":             false,
			"archived_heads":       archived,
			"archived_field_count": len(archived),
			"settings":             settingsResponse,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return s.recordOperation(r.Context(), tx, "owner_drift_archive", request.OperationID, canonicalArchiveHash(request), raw)
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	if replayed {
		result["replayed"] = true
	}
	writeSettingsJSON(w, http.StatusOK, result)
}

// ---- preflight ----

func (s *retentionService) handleCreatePreflight(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	// Discriminated union: policy_change | manual_cleanup.
	var raw struct {
		Kind                     string                  `json:"kind"`
		OperationID              string                  `json:"operation_id"`
		PreflightAttemptID       string                  `json:"preflight_attempt_id"`
		ExpectedSettingsRevision string                  `json:"expected_settings_revision"`
		Policies                 *retentionPolicies      `json:"policies"`
		Dataset                  string                  `json:"dataset"`
		Selection                *manualCleanupSelection `json:"selection"`
	}
	if err := decodeStrictJSONBody(r, &raw); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(raw.OperationID) == "" || strings.TrimSpace(raw.PreflightAttemptID) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id and preflight_attempt_id are required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	if raw.Kind != "policy_change" && raw.Kind != "manual_cleanup" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "kind must be policy_change or manual_cleanup", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	if raw.Kind == "policy_change" {
		if raw.Policies == nil || strings.TrimSpace(raw.ExpectedSettingsRevision) == "" {
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "policy preflight requires policies and expected_settings_revision", Params: map[string]any{}}, http.StatusUnprocessableEntity)
			return
		}
		if strings.TrimSpace(raw.Dataset) != "" || raw.Selection != nil {
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "policy_change does not accept manual cleanup fields", Params: map[string]any{}}, http.StatusUnprocessableEntity)
			return
		}
		if violations := validateRetentionPolicies(*raw.Policies); len(violations) > 0 {
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "invalid_retention_policy", Detail: "invalid retention policy", Params: map[string]any{}, Details: map[string]any{"violations": violations}}, http.StatusUnprocessableEntity)
			return
		}
	} else if raw.Policies != nil || strings.TrimSpace(raw.ExpectedSettingsRevision) != "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "manual_cleanup does not accept policy fields", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}

	var response retentionPreflightResponse
	err := pgxutil.InRepeatableReadWriteTx(r.Context(), s.pool, "settings retention preflight", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		now := s.now().UTC()
		current, err := loadRetentionRow(r.Context(), tx)
		if err != nil {
			return err
		}
		if raw.Kind == "manual_cleanup" && raw.Selection != nil && raw.Selection.Mode == "cutoff" {
			parsed, parseErr := parseRetentionCutoff(*raw.Selection.Cutoff, now)
			if parseErr != nil {
				return &settingsConflictError{code: "invalid_retention_cutoff"}
			}
			canonicalCutoff := formatRetentionCutoff(&parsed)
			raw.Selection.Cutoff = canonicalCutoff
		}
		requestHash := canonicalPreflightHash(raw)

		// Replay same attempt id/hash returns the same preview.
		var existing retentionPreflightRow
		err = tx.QueryRow(r.Context(), `SELECT id, kind, operation_id, preflight_attempt_id, token_hash, request_hash,
				settings_revision, principal_generation, affected_domains, confirmation_keyword, previewed_at, generated_at, expires_at
				FROM log_retention_preflights WHERE operation_id = $1 AND preflight_attempt_id = $2`,
			raw.OperationID, raw.PreflightAttemptID).Scan(
			&existing.ID, &existing.Kind, &existing.OperationID, &existing.PreflightAttemptID, &existing.TokenHash,
			&existing.RequestHash, &existing.SettingsRevision, &existing.PrincipalGeneration, &existing.AffectedDomains, &response.ConfirmationKeyword,
			&response.PreviewedAt, &response.GeneratedAt, &response.ExpiresAt)
		if err == nil {
			if existing.RequestHash != requestHash {
				return &settingsConflictError{code: "operation_id_conflict", operationID: raw.OperationID}
			}
			if existing.PrincipalGeneration == nil || *existing.PrincipalGeneration != managementPrincipalGeneration(r) {
				return &settingsConflictError{code: "retention_preflight_stale"}
			}
			response.PreflightID = existing.ID
			// Only a hash is stored at rest. A response-loss retry gets a new
			// opaque capability for the same sealed preview; the previous
			// capability is invalidated atomically while the row is locked.
			response.PreflightToken, err = s.issuePreflightToken()
			if err != nil {
				return fmt.Errorf("reissue preflight token: %w", err)
			}
			if _, err := tx.Exec(r.Context(), `UPDATE log_retention_preflights SET token_hash = $2 WHERE id = $1`, existing.ID, hashToken(response.PreflightToken)); err != nil {
				return err
			}
			response.Kind = existing.Kind
			response.OperationID = raw.OperationID
			response.PreflightAttemptID = raw.PreflightAttemptID
			response.Scope = "instance"
			if existing.SettingsRevision != nil {
				response.SettingsRevision = fmt.Sprintf("%d", *existing.SettingsRevision)
			} else {
				response.SettingsRevision = fmt.Sprintf("%d", current.Revision)
			}
			_ = json.Unmarshal(existing.AffectedDomains, &response.AffectedDomains)
			return nil
		}
		if err != pgx.ErrNoRows {
			return err
		}
		// Once the final destructive intent has an outcome, a later preview
		// with the same operation id is not a new attempt: it is an operation
		// identity conflict. Exact same-attempt replay was handled above.
		resourceKind := "log_retention"
		if raw.Kind == "manual_cleanup" {
			resourceKind = "manual_retention_job"
		}
		var recordedState string
		if err := tx.QueryRow(r.Context(), `SELECT state FROM settings_mutation_operations
			WHERE resource_kind = $1 AND operation_id = $2`, resourceKind, raw.OperationID).Scan(&recordedState); err == nil {
			return &settingsConflictError{code: "operation_id_conflict", operationID: raw.OperationID}
		} else if err != pgx.ErrNoRows {
			return err
		}
		if raw.Kind == "policy_change" && fmt.Sprintf("%d", current.Revision) != raw.ExpectedSettingsRevision {
			return &settingsConflictError{code: "retention_settings_changed", currentRevision: fmt.Sprintf("%d", current.Revision)}
		}

		// Build the exact affected-domain subset.
		_, impactByDomain, err := s.buildPreflightImpact(r.Context(), tx, raw, current, now)
		if err != nil {
			return err
		}
		affected := []retentionAffectedDomain{}
		for _, dataset := range retentionDatasets {
			if _, ok := impactByDomain[dataset]; !ok {
				continue
			}
			ownerSnapshot, snapshotErr := s.ownerSnapshotFor(r.Context(), tx, dataset, now)
			if snapshotErr != nil {
				return snapshotErr
			}
			affected = append(affected, retentionAffectedDomain{
				Dataset:       dataset,
				OwnerSnapshot: ownerSnapshot,
				Impact:        impactByDomain[dataset],
			})
		}
		affectedRaw, err := json.Marshal(affected)
		if err != nil {
			return err
		}

		preflightID := "pf_" + canonicalHash("preflight", raw.OperationID, raw.PreflightAttemptID, requestHash)[:16]
		token, err := s.issuePreflightToken()
		if err != nil {
			return fmt.Errorf("issue preflight token: %w", err)
		}
		expiresAt := now.Add(5 * time.Minute)
		previewedAt := now

		principalGeneration := managementPrincipalGeneration(r)
		if _, err := tx.Exec(r.Context(), `INSERT INTO log_retention_preflights (
				id, kind, operation_id, preflight_attempt_id, token_hash, scope, request_hash,
				settings_revision, principal_generation, affected_domains, confirmation_keyword, previewed_at, generated_at, expires_at, created_at
			) VALUES ($1, $2, $3, $4, $5, 'instance', $6, $7, $8, $9, 'DELETE', $10, $10, $11, $10)`,
			preflightID, raw.Kind, raw.OperationID, raw.PreflightAttemptID, hashToken(token),
			requestHash, current.Revision, principalGeneration, affectedRaw, previewedAt, expiresAt); err != nil {
			return err
		}

		response = retentionPreflightResponse{
			PreflightID:         preflightID,
			PreflightToken:      token,
			Kind:                raw.Kind,
			OperationID:         raw.OperationID,
			PreflightAttemptID:  raw.PreflightAttemptID,
			Scope:               "instance",
			RequestHash:         requestHash,
			PreviewedAt:         previewedAt.UTC().Format(time.RFC3339),
			GeneratedAt:         now.UTC().Format(time.RFC3339),
			ExpiresAt:           expiresAt.UTC().Format(time.RFC3339),
			SettingsRevision:    fmt.Sprintf("%d", current.Revision),
			AffectedDomains:     affected,
			ConfirmationKeyword: "DELETE",
		}
		return nil
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	writeSettingsJSON(w, http.StatusCreated, response)
}

// buildPreflightImpact returns the exact destructive affected-domain subset
// with bounded impact facts (SPEC §6.2). Policy bundles list only destructive
// changed datasets; manual cleanup lists exactly its selected dataset.
func (s *retentionService) buildPreflightImpact(ctx context.Context, tx pgx.Tx, raw struct {
	Kind                     string                  `json:"kind"`
	OperationID              string                  `json:"operation_id"`
	PreflightAttemptID       string                  `json:"preflight_attempt_id"`
	ExpectedSettingsRevision string                  `json:"expected_settings_revision"`
	Policies                 *retentionPolicies      `json:"policies"`
	Dataset                  string                  `json:"dataset"`
	Selection                *manualCleanupSelection `json:"selection"`
}, current retentionRow, now time.Time) ([]string, map[string]retentionImpactDetails, error) {
	impactByDomain := map[string]retentionImpactDetails{}

	if raw.Kind == "policy_change" {
		if raw.Policies == nil {
			return nil, nil, &settingsConflictError{code: "validation_failed"}
		}
		for _, dataset := range retentionDatasets {
			before := policyFieldForRow(current, dataset)
			after := policyFieldValue(*raw.Policies, dataset)
			if !isDestructiveTransition(before, after) {
				continue
			}
			impact, err := s.policyImpact(ctx, tx, dataset, before, after, now)
			if err != nil {
				return nil, nil, err
			}
			if !impact.SemanticFactsComplete {
				return nil, nil, &settingsConflictError{code: "retention_owner_unavailable"}
			}
			impactByDomain[dataset] = impact
		}
	} else if raw.Kind == "manual_cleanup" {
		if !isManagedDataset(raw.Dataset) || raw.Selection == nil {
			return nil, nil, &settingsConflictError{code: "validation_failed"}
		}
		impact, err := s.manualImpact(ctx, tx, raw.Dataset, *raw.Selection, now)
		if err != nil {
			return nil, nil, err
		}
		if !impact.SemanticFactsComplete {
			return nil, nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
		impactByDomain[raw.Dataset] = impact
	} else {
		return nil, nil, &settingsConflictError{code: "validation_failed"}
	}

	// Canonical emission order: request_logs, audit_logs, usage, events.
	ordered := []string{}
	for _, dataset := range retentionDatasets {
		if _, ok := impactByDomain[dataset]; ok {
			ordered = append(ordered, dataset)
		}
	}
	if len(ordered) == 0 {
		return nil, nil, &settingsConflictError{code: "retention_preflight_required"}
	}
	return ordered, impactByDomain, nil
}

func isManagedDataset(dataset string) bool {
	for _, item := range retentionDatasets {
		if item == dataset {
			return true
		}
	}
	return false
}

// policyImpact builds a bounded policy-change impact for one dataset.
func (s *retentionService) policyImpact(ctx context.Context, tx pgx.Tx, dataset string, before, after *int, now time.Time) (retentionImpactDetails, error) {
	cutoff := utcDayCutoff(now, *after)
	details := retentionImpactDetails{
		Change: map[string]any{
			"kind":       "policy_change",
			"before":     taggedPolicyValue(before),
			"after_days": after,
		},
		ResolvedCutoff:           formatTimePtr(&cutoff),
		MatchedLogicalBytes:      unavailableImpactBytes("logical_bytes_unavailable"),
		ReclaimablePhysicalBytes: unavailableImpactBytes("physical_bytes_unavailable"),
		SemanticFactsComplete:    true,
		Consumers:                consumerLabels(dataset),
	}
	coverageAfter, coverageErr := s.logicalCoverageAfter(ctx, tx, dataset, now, &cutoff)
	if coverageErr != nil {
		return retentionImpactDetails{}, coverageErr
	}
	details.LogicalCoverageAfter = coverageAfter
	details.SemanticFactsComplete = coverageAfter.CoverageReady
	var matched retentionImpactCount
	var retained retentionImpactCount
	var whole map[string]any
	var boundary []map[string]any
	matched, retained, whole, boundary = unavailableRetentionImpactEstimate()
	_ = withSettingsReadSavepoint(ctx, tx, "settings_impact_policy", func() error {
		var err error
		matched, retained, whole, boundary, err = estimateImpact(ctx, tx, dataset, &cutoff, false)
		return err
	})
	details.MatchedRows = matched
	details.RetainedRows = retained
	details.WholePartitions = whole
	details.BoundaryPartitions = toAnySlice(boundary)
	details.StorageLayers = []any{}
	details.NonCascades = nonCascades(dataset)
	details.Warnings = []string{"延长保留期不会恢复已经清理的记录"}
	if dataset != retentionDatasetAuditLogs {
		deadline := managementjobs.ObserveProtectionDeadline(now)
		details.PhysicalReclaimNotBefore = formatTimePtr(&deadline)
	}
	return details, nil
}

// manualImpact builds a bounded manual-cleanup impact.
func (s *retentionService) manualImpact(ctx context.Context, tx pgx.Tx, dataset string, selection manualCleanupSelection, now time.Time) (retentionImpactDetails, error) {
	var cutoff *time.Time
	deleteAll := false
	switch selection.Mode {
	case "delete_all":
		if selection.Days != nil || selection.Cutoff != nil {
			return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
		}
		deleteAll = true
	case "keep_days":
		if selection.Days == nil || selection.Cutoff != nil || !isRetentionPreset(*selection.Days) {
			return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
		}
		value := utcDayCutoff(now, *selection.Days)
		cutoff = &value
	case "cutoff":
		if selection.Cutoff == nil || selection.Days != nil {
			return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
		}
		parsed, err := parseRetentionCutoff(*selection.Cutoff, now)
		if err != nil {
			return retentionImpactDetails{}, &settingsConflictError{code: "invalid_retention_cutoff"}
		}
		cutoff = &parsed
	default:
		return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
	}

	impactCutoff := cutoff
	if deleteAll {
		// Preview delete-all against the same present-time fence semantics as
		// the worker: historical partitions and rows before the preview fence
		// are affected, while the current/future horizon remains protected.
		fence := now.UTC()
		impactCutoff = &fence
	}
	details := retentionImpactDetails{
		Change: map[string]any{
			"kind": "manual_cleanup",
			"mode": selection.Mode,
		},
		ResolvedCutoff:           formatRetentionCutoff(cutoff),
		MatchedLogicalBytes:      unavailableImpactBytes("logical_bytes_unavailable"),
		ReclaimablePhysicalBytes: unavailableImpactBytes("physical_bytes_unavailable"),
		SemanticFactsComplete:    true,
		Consumers:                consumerLabels(dataset),
		Warnings: []string{
			"预览是提交前估算；delete-all 的最终 purge_to_time 由 worker 在 execution fence 锁定",
			"手动清理不享受 scheduled query-token grace",
		},
	}
	coverageAfter, coverageErr := s.logicalCoverageAfter(ctx, tx, dataset, now, impactCutoff)
	if coverageErr != nil {
		return retentionImpactDetails{}, coverageErr
	}
	coverageReady := coverageAfter.CoverageReady
	if deleteAll {
		// The execution fence is taken by the worker after the job is accepted.
		// A preview must not turn that future fence into an exact empty logical
		// range, even when the current owner model is fresh.
		coverageAfter.FromTime = nil
		coverageAfter.Accuracy = "unavailable"
		coverageAfter.Basis = "unavailable"
		coverageAfter.Gaps = append(coverageAfter.Gaps, map[string]any{
			"reason": "execution_fence_pending",
		})
	}
	details.LogicalCoverageAfter = coverageAfter
	details.SemanticFactsComplete = coverageReady
	var matched retentionImpactCount
	var retained retentionImpactCount
	var whole map[string]any
	var boundary []map[string]any
	matched, retained, whole, boundary = unavailableRetentionImpactEstimate()
	_ = withSettingsReadSavepoint(ctx, tx, "settings_impact_manual", func() error {
		var err error
		matched, retained, whole, boundary, err = estimateImpact(ctx, tx, dataset, impactCutoff, deleteAll)
		return err
	})
	details.MatchedRows = matched
	details.RetainedRows = retained
	details.WholePartitions = whole
	details.BoundaryPartitions = toAnySlice(boundary)
	details.StorageLayers = []any{}
	details.NonCascades = nonCascades(dataset)
	// Manual purge has no scheduled Observe-token grace. Its protection is the
	// owner purge fence and final publish, so do not expose a fabricated
	// 48-hour deadline (audit likewise has its own fence contract).
	return details, nil
}

func (s *retentionService) logicalCoverageAfter(ctx context.Context, tx pgx.Tx, dataset string, now time.Time, cutoff *time.Time) (retentionCoverageProjection, error) {
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return retentionCoverageProjection{}, err
	}
	coverage, err := statsdomain.LoadActualCoverageProjection(ctx, tx, source)
	if err != nil {
		return retentionCoverageProjection{}, err
	}
	coverageReady := coverage.Complete && coverage.Freshness == "fresh" && coverage.Revision != "" && coverage.Hash != "" && coverage.GeneratedAt != nil
	earliest := coverage.Earliest
	latest := coverage.Latest
	gaps := make([]any, 0, len(coverage.Gaps)+1)
	for _, gap := range coverage.Gaps {
		gaps = append(gaps, gap)
	}
	if cutoff != nil {
		if earliest == nil || latest == nil {
			// An empty/unknown owner projection must remain empty/unknown. Do
			// not turn a policy cutoff into a fabricated retained interval.
			earliest = nil
			latest = nil
			gaps = append(gaps, map[string]any{
				"from_time": coverage.Earliest,
				"to_time":   coverage.Latest,
				"reason":    "no_retained_intersection",
			})
		} else if latest.Before(*cutoff) {
			earliest = nil
			latest = nil
			gaps = append(gaps, map[string]any{
				"from_time": coverage.Earliest,
				"to_time":   coverage.Latest,
				"reason":    "no_retained_intersection",
			})
		} else if earliest.Before(*cutoff) {
			clamped := cutoff.UTC()
			earliest = &clamped
		}
	}
	if coverage.GapReason != nil {
		gaps = append(gaps, map[string]any{"from_time": earliest, "to_time": latest, "reason": *coverage.GapReason})
	}
	accuracy := coveragePrecision(coverage.Precision, coverage.Complete, coverage.Freshness)
	if !coverage.Complete || coverage.Freshness != "fresh" {
		// A dirty, unavailable or purge-fenced owner projection cannot prove
		// semantic coverage facts even when its last precision label was
		// owner_bounds. The UI must keep confirmation disabled until the owner
		// publishes a fresh bounded read model.
		accuracy = "unavailable"
	}
	basis := "unavailable"
	if coverage.Complete && coverage.Freshness == "fresh" {
		basis = "owner_read_model"
	}
	toTime := now.UTC().Format(time.RFC3339)
	if latest != nil {
		toTime = latest.UTC().Format(time.RFC3339)
	}
	return retentionCoverageProjection{
		FromTime:      formatTimePtr(earliest),
		ToTime:        toTime,
		Gaps:          gaps,
		Accuracy:      accuracy,
		Basis:         basis,
		CoverageReady: coverageReady,
	}, nil
}

func isRetentionPreset(days int) bool {
	return days == 1 || days == 7 || days == 30 || days == 90
}

func parseRetentionCutoff(value string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasSuffix(trimmed, "Z") {
		return time.Time{}, fmt.Errorf("retention cutoff must be UTC")
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil || !parsed.UTC().Equal(parsed) || parsed.After(now.UTC()) {
		return time.Time{}, fmt.Errorf("invalid retention cutoff")
	}
	// The wire contract canonicalizes accepted custom cutoffs to UTC
	// microseconds before hashing and before the sealed job scope is created.
	return parsed.UTC().Truncate(time.Microsecond), nil
}

func formatRetentionCutoff(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format("2006-01-02T15:04:05.000000Z")
	return &formatted
}

func (s *retentionService) issuePreflightToken() (string, error) {
	// The token is a fresh 256-bit CSPRNG capability. It is returned only in
	// the response body; callers persist only hashToken(token). The second
	// no secret material is persisted.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, nil
}

func toAnySlice(values []map[string]any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func consumerLabels(dataset string) []string {
	switch dataset {
	case retentionDatasetRequestLogs:
		return []string{"Requests", "recent activity", "ingress/attempt chain", "精确请求 deep link"}
	case retentionDatasetUsageRequestEvents:
		return []string{"Overview/Analytics", "模型/Endpoint/Proxy Key/Terminal Target 统计", "token 与 spend"}
	case retentionDatasetAuditLogs:
		return []string{"Audit metadata/header/body 证据"}
	default:
		return []string{"Observe Events", "已记录 retry/ban/unban/recovery/admission evidence"}
	}
}

func nonCascades(dataset string) []any {
	if dataset == retentionDatasetRequestLogs {
		return []any{map[string]any{
			"dataset":       "audit_logs",
			"effect":        "preserved",
			"retained_rows": retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "owner_preservation_relation"},
		}}
	}
	if dataset == retentionDatasetAuditLogs {
		return []any{map[string]any{
			"dataset":       "request_logs",
			"effect":        "preserved",
			"retained_rows": retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "owner_preservation_relation"},
		}}
	}
	return []any{}
}

type retentionBoundaryEstimate struct {
	Name        string
	MatchedRows int64
}

// estimateImpact produces bounded count/partition facts using partition
// catalog metadata and a bounded boundary count (SPEC §6.2: no unbounded
// COUNT for a prettier dialog).
func estimateImpact(ctx context.Context, tx pgx.Tx, dataset string, cutoff *time.Time, deleteAll bool) (retentionImpactCount, retentionImpactCount, map[string]any, []map[string]any, error) {
	unavailable := func(err error) (retentionImpactCount, retentionImpactCount, map[string]any, []map[string]any, error) {
		return retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
			retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
			map[string]any{"count": nil, "names_preview": []string{}, "names_total_count": nil, "truncated": false},
			[]map[string]any{}, err
	}
	// Partition catalog: name, start/end bounds and reltuples estimate.
	rows, err := tx.Query(ctx, `SELECT child.relname,
			CASE WHEN child.reltuples >= 0 THEN child.reltuples::bigint ELSE NULL END,
			pg_get_expr(child.relpartbound, child.oid)
		FROM pg_inherits AS inh
		JOIN pg_class AS parent ON parent.oid = inh.inhparent
		JOIN pg_class AS child ON child.oid = inh.inhrelid
		WHERE parent.relname = $1 AND parent.relkind = 'p'
		ORDER BY child.relname ASC`, dataset)
	if err != nil {
		return unavailable(err)
	}
	defer rows.Close()
	type partitionEstimate struct {
		Name   string
		Tuples *int64
		Bound  string
	}
	partitions := []partitionEstimate{}
	for rows.Next() {
		var item partitionEstimate
		if err := rows.Scan(&item.Name, &item.Tuples, &item.Bound); err != nil {
			return unavailable(err)
		}
		partitions = append(partitions, item)
	}
	if err := rows.Err(); err != nil {
		return unavailable(err)
	}
	if len(partitions) == 0 {
		return unavailable(nil)
	}

	droppedTuples := int64(0)
	retainedTuples := int64(0)
	droppedKnown := true
	retainedKnown := true
	droppedNames := []string{}
	var boundaryPartition *retentionBoundaryEstimate
	for _, partition := range partitions {
		startTime, startOK := parseBoundTime(parsePartitionBound(partition.Bound, false))
		endStr := parsePartitionBound(partition.Bound, true)
		endTime, endOK := parseBoundTime(endStr)
		if cutoff != nil && endOK && !endTime.After(*cutoff) {
			// Whole partition is at or before the cutoff: fully dropped.
			if partition.Tuples == nil {
				droppedKnown = false
			} else {
				droppedTuples += *partition.Tuples
			}
			droppedNames = append(droppedNames, partition.Name)
			continue
		}
		if partition.Tuples == nil {
			retainedKnown = false
		} else {
			retainedTuples += *partition.Tuples
		}
		if cutoff != nil && startOK && endOK && boundaryPartition == nil && !cutoff.Before(startTime) && endTime.After(*cutoff) {
			// First partition spanning the cutoff is the boundary partition.
			if partition.Tuples != nil {
				boundaryPartition = &retentionBoundaryEstimate{
					Name:        partition.Name,
					MatchedRows: *partition.Tuples,
				}
			}
		}
	}
	// Boundary rows: bounded exact count when feasible, else catalog estimate.
	matched := retentionImpactCount{Accuracy: "estimated", Method: "partition_metadata"}
	retained := retentionImpactCount{Accuracy: "estimated", Method: "partition_metadata"}
	if droppedKnown && boundaryPartition != nil {
		matched.Value = strPtr(fmt.Sprintf("%d", droppedTuples+boundaryPartition.MatchedRows))
	} else if droppedKnown {
		matched.Value = strPtr(fmt.Sprintf("%d", droppedTuples))
	} else {
		matched.Accuracy = "unavailable"
		matched.Method = "partition_metadata_unavailable"
	}
	if retainedKnown {
		retained.Value = strPtr(fmt.Sprintf("%d", retainedTuples))
	} else {
		retained.Accuracy = "unavailable"
		retained.Method = "partition_metadata_unavailable"
	}
	whole := map[string]any{
		"count":             fmt.Sprintf("%d", len(droppedNames)),
		"names_preview":     boundedNames(droppedNames),
		"names_total_count": fmt.Sprintf("%d", len(droppedNames)),
		"truncated":         len(droppedNames) > 8,
	}
	boundary := []map[string]any{}
	if boundaryPartition != nil {
		boundary = append(boundary, map[string]any{
			"name":         boundaryPartition.Name,
			"matched_rows": map[string]any{"value": strPtr(fmt.Sprintf("%d", boundaryPartition.MatchedRows)), "accuracy": "estimated", "method": "partition_metadata"},
		})
	}
	return matched, retained, whole, boundary, nil
}

func unavailableRetentionImpactEstimate() (retentionImpactCount, retentionImpactCount, map[string]any, []map[string]any) {
	return retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
		retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
		map[string]any{"count": nil, "names_preview": []string{}, "names_total_count": nil, "truncated": false},
		[]map[string]any{}
}

func parsePartitionBound(expr string, end bool) string {
	// expr looks like: FOR VALUES FROM ('2026-08-01 00:00:00+00') TO ('2026-08-02 00:00:00+00')
	marker := " TO ("
	part := expr
	if !end {
		marker = " FROM ("
		part = expr
	}
	index := strings.Index(part, marker)
	if index < 0 {
		return ""
	}
	rest := part[index+len(marker):]
	endIndex := strings.Index(rest, ")")
	if endIndex < 0 {
		return ""
	}
	return strings.Trim(rest[:endIndex], " '")
}

func parseBoundTime(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		// pg partition bounds are often `2026-08-01 00:00:00+00`
		parsed, err = time.Parse("2006-01-02 15:04:05-07", value)
		if err != nil {
			parsed, err = time.Parse("2006-01-02 15:04:05Z07:00", value)
		}
	}
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func boundedNames(names []string) []string {
	if len(names) > 8 {
		return names[:8]
	}
	return names
}

func strPtr(value string) *string { return &value }

func unavailableImpactBytes(reason string) retentionImpactBytes {
	return retentionImpactBytes{Value: nil, Accuracy: "unavailable", Basis: reason}
}

func coveragePrecision(precision string, complete bool, freshness string) string {
	if !complete || freshness != "fresh" {
		return "unavailable"
	}
	switch precision {
	case "owner_bounds":
		return "exact"
	case "estimated":
		return "estimated"
	default:
		return "unavailable"
	}
}

// ownerSnapshotFor builds the discriminated owner snapshot for a listed
// affected domain (Observe §6.1.1 realization + audit fence projection).
func (s *retentionService) ownerSnapshotFor(ctx context.Context, tx pgx.Tx, dataset string, now time.Time) (any, error) {
	coverageSnapshot := func(source statsdomain.RetentionFloorEpochSource) (map[string]any, statsdomain.ActualCoverageProjection, error) {
		coverage, err := statsdomain.LoadActualCoverageProjection(ctx, tx, source)
		if err != nil {
			return nil, statsdomain.ActualCoverageProjection{}, err
		}
		if coverage.Revision == "" || coverage.Hash == "" || coverage.GeneratedAt == nil {
			return nil, statsdomain.ActualCoverageProjection{}, &settingsConflictError{code: "retention_owner_unavailable"}
		}
		return map[string]any{
			"domain":            coverage.Domain,
			"earliest":          formatTimePtr(coverage.Earliest),
			"latest":            formatTimePtr(coverage.Latest),
			"coverage_revision": coverage.Revision,
			"coverage_hash":     coverage.Hash,
			"generated_at":      formatTimePtr(coverage.GeneratedAt),
			"source":            coverage.Source,
			"precision":         coverage.Precision,
			"complete":          coverage.Complete,
			"freshness":         coverage.Freshness,
			"gaps":              coverage.Gaps,
			"gap_reason":        coverage.GapReason,
		}, coverage, nil
	}
	if dataset == retentionDatasetAuditLogs {
		source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
		if err != nil {
			return nil, err
		}
		if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
			return nil, &settingsConflictError{code: "retention_job_conflict"}
		}
		coverage, _, err := coverageSnapshot(source)
		if err != nil {
			return nil, err
		}
		protection, err := auditProtectionProjection(ctx, tx, now)
		if err != nil {
			return nil, err
		}
		protectionMap := map[string]any{
			"contract_version":        protection.ContractVersion,
			"fence_generation":        protection.FenceGeneration,
			"reader_fence_state":      protection.ReaderFenceState,
			"materializer_generation": protection.MaterializerGeneration,
			"materializer_state":      protection.MaterializerState,
			"generated_at":            protection.GeneratedAt,
		}
		return map[string]any{
			"kind":                  "audit",
			"dataset":               dataset,
			"contract_version":      3,
			"retention_source":      retentionOwnerSourceProjectionMap(source),
			"audit_protection":      protectionMap,
			"actual_coverage":       coverage,
			"storage_fact_evidence": auditStorageFactEvidence(ctx, tx, source.SourceRevision),
		}, nil
	}
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return nil, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return nil, &settingsConflictError{code: "retention_job_conflict"}
	}
	coverage, projection, err := coverageSnapshot(source)
	if err != nil {
		return nil, err
	}
	materializationCut, err := exactObserveMaterializationCut(dataset, projection.MaterializationCut)
	if err != nil {
		return nil, err
	}
	snapshot := map[string]any{
		"kind":                       "observe",
		"contract_version":           1,
		"dataset":                    dataset,
		"policy_generation":          source.RetentionGeneration,
		"retention_revocation_epoch": source.RetentionEpoch,
		"configured_cutoff":          formatTimePtr(source.ConfiguredCutoff),
		"published_floor":            formatTimePtr(source.PublishedFloor),
		"fence_generation":           source.FenceGeneration,
		"purge_state":                normalizedRetentionOwnerPurgeState(source.PurgeState),
		"coverage_revision":          projection.Revision,
		"coverage_hash":              projection.Hash,
		"generated_at":               formatTimePtr(projection.GeneratedAt),
		"actual_coverage":            coverage,
		"materialization_cut":        materializationCut,
	}
	return snapshot, nil
}

func normalizedRetentionOwnerPurgeState(state string) string {
	if state == "published" || state == "rolled_back" {
		return "idle"
	}
	return state
}

// exactObserveMaterializationCut preserves the owner-defined discriminated
// union. Settings validates its shape but never synthesizes a cut from its
// own read timestamp or compresses the variants into a nullable scalar.
func exactObserveMaterializationCut(dataset string, value map[string]any) (map[string]any, error) {
	kind, ok := value["kind"].(string)
	if !ok {
		return nil, &settingsConflictError{code: "retention_owner_unavailable"}
	}
	requireString := func(field string) bool {
		item, present := value[field]
		text, valid := item.(string)
		return present && valid && strings.TrimSpace(text) != ""
	}
	validOptional := func(field string) bool {
		item, present := value[field]
		if !present || item == nil {
			return true
		}
		text, valid := item.(string)
		return valid && strings.TrimSpace(text) != ""
	}
	requireUTCInstant := func(field string) bool {
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
		return validOptional(left) && validOptional(right)
	}
	switch dataset {
	case retentionDatasetRequestLogs:
		if kind != "request_visibility_cut" || !requireString("request_committed_cut") || !requireUTCInstant("request_committed_cut") || len(value) != 2 {
			return nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
	case retentionDatasetUsageRequestEvents:
		if kind != "usage_hybrid_cut" || !requireString("raw_committed_cut") || !requireUTCInstant("raw_committed_cut") || !optionalPair("rollup_manifest_cut", "build_revision") || len(value) != 4 {
			return nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
	case retentionDatasetLoadbalanceEvents:
		if kind != "event_hybrid_cut" || !requireString("raw_committed_cut") || !requireUTCInstant("raw_committed_cut") || !optionalPair("rollup_manifest_cut", "build_revision") || len(value) != 4 {
			return nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
	default:
		return nil, &settingsConflictError{code: "retention_owner_unavailable"}
	}
	return value, nil
}

// ---- manual job creation (sealed intent) ----

func (s *retentionService) handleCreateManualRetentionJob(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	var request createManualRetentionJobRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" || strings.TrimSpace(request.PreflightToken) == "" {
		writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "validation_failed", Detail: "operation_id and preflight_token are required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}

	var summary managementjobs.RetentionJobSummaryDTO
	replayed := false
	accepted := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings manual retention job", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		// Replay first: a lost-response retry carries the same operation id and
		// request hash and must return the recorded job even though the
		// preflight token was already consumed (SPEC §6.4).
		operation, err := s.loadOperation(r.Context(), tx, "manual_retention_job", request.OperationID)
		if err == nil {
			hash := canonicalManualJobHash(request)
			if operation.RequestHash == hash {
				replayed = true
				if operation.ResultJSON != nil {
					return json.Unmarshal(operation.ResultJSON, &summary)
				}
				return nil
			}
			return &settingsConflictError{code: "operation_id_conflict", operationID: request.OperationID}
		}
		if err != pgx.ErrNoRows {
			return err
		}

		tokenHash := hashToken(request.PreflightToken)
		var preflight retentionPreflightRow
		err = tx.QueryRow(r.Context(), `SELECT id, kind, operation_id, preflight_attempt_id, token_hash, request_hash,
			settings_revision, principal_generation, affected_domains, expires_at, consumed_at
			FROM log_retention_preflights WHERE token_hash = $1 FOR UPDATE`, tokenHash).Scan(
			&preflight.ID, &preflight.Kind, &preflight.OperationID, &preflight.PreflightAttemptID, &preflight.TokenHash,
			&preflight.RequestHash, &preflight.SettingsRevision, &preflight.PrincipalGeneration, &preflight.AffectedDomains, &preflight.ExpiresAt, &preflight.ConsumedAt)
		if err != nil {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.Kind != "manual_cleanup" || preflight.OperationID != request.OperationID {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.PrincipalGeneration == nil || *preflight.PrincipalGeneration != managementPrincipalGeneration(r) {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if request.Confirmation.Keyword != "DELETE" {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.ExpiresAt.Before(s.now().UTC()) {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if preflight.ConsumedAt != nil {
			// The first request may have committed the job and lost its HTTP
			// response. Re-read the durable outcome before declaring the
			// single-use token stale.
			operation, operationErr := s.loadOperation(r.Context(), tx, "manual_retention_job", request.OperationID)
			if operationErr == nil && operation.RequestHash == canonicalManualJobHash(request) && operation.ResultJSON != nil {
				replayed = true
				return json.Unmarshal(operation.ResultJSON, &summary)
			}
			return &settingsConflictError{code: "retention_preflight_stale"}
		}

		// The sealed preflight is the single source of truth for the scope.
		var domains []retentionAffectedDomain
		if err := json.Unmarshal(preflight.AffectedDomains, &domains); err != nil {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		if len(domains) != 1 {
			return &settingsConflictError{code: "retention_preflight_stale"}
		}
		dataset := domains[0].Dataset
		currentSnapshot, snapshotErr := s.ownerSnapshotFor(r.Context(), tx, dataset, s.now().UTC())
		if snapshotErr != nil {
			return snapshotErr
		}
		if !domains[0].Impact.SemanticFactsComplete {
			return &settingsConflictError{code: "retention_owner_unavailable"}
		}
		if canonicalOwnerSemanticSnapshotHash(domains[0].OwnerSnapshot) != canonicalOwnerSemanticSnapshotHash(currentSnapshot) {
			return &settingsConflictError{
				code:               "retention_preflight_stale",
				currentGenerations: map[string]string{dataset: ownerSnapshotGeneration(currentSnapshot)},
			}
		}
		mode := ""
		if change, ok := domains[0].Impact.Change.(map[string]any); ok {
			if value, ok := change["mode"].(string); ok {
				mode = value
			}
		}
		var cutoff *time.Time
		if domains[0].Impact.ResolvedCutoff != nil {
			parsed, parseErr := time.Parse(time.RFC3339Nano, *domains[0].Impact.ResolvedCutoff)
			if parseErr != nil {
				return &settingsConflictError{code: "retention_preflight_stale"}
			}
			cutoff = &parsed
		}
		deleteAll := mode == "delete_all"

		job, err := s.jobs.CreateManualRetentionJobTx(r.Context(), tx, dataset, cutoff, deleteAll, request.OperationID, preflight.ID, s.now().UTC())
		if err != nil {
			if isJobConflict(err) {
				return &settingsConflictError{code: "retention_job_conflict"}
			}
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE log_retention_preflights SET consumed_at = now(),
			consumed_operation_id = $2 WHERE id = $1`, preflight.ID, request.OperationID); err != nil {
			return err
		}
		summary = job
		accepted = true
		raw, err := json.Marshal(job)
		if err != nil {
			return err
		}
		return s.recordOperation(r.Context(), tx, "manual_retention_job", request.OperationID, canonicalManualJobHash(request), raw)
	})
	if err != nil {
		writeSettingsError(w, r, corsSnapshot, err)
		return
	}
	status := http.StatusOK
	if accepted && !replayed {
		status = http.StatusAccepted
	}
	writeSettingsJSON(w, status, map[string]any{
		"operation_id": request.OperationID,
		"replayed":     replayed,
		"job":          summary,
	})
}

// ---- operation outcome store helpers ----

type settingsOperationRow struct {
	ResourceKind string
	OperationID  string
	RequestHash  string
	ResultJSON   []byte
}

func (s *retentionService) loadOperation(ctx context.Context, tx pgx.Tx, resourceKind string, operationID string) (settingsOperationRow, error) {
	var row settingsOperationRow
	err := tx.QueryRow(ctx, `SELECT resource_kind, operation_id, request_hash, result_json
		FROM settings_mutation_operations WHERE resource_kind = $1 AND operation_id = $2`,
		resourceKind, operationID).Scan(&row.ResourceKind, &row.OperationID, &row.RequestHash, &row.ResultJSON)
	if err != nil {
		return settingsOperationRow{}, err
	}
	return row, nil
}

func (s *retentionService) recordOperation(ctx context.Context, tx pgx.Tx, resourceKind string, operationID string, requestHash string, resultJSON []byte) error {
	_, err := tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
		resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
	) VALUES ($1, $2, $3, 'completed', $4, now(), now())
	ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
		resourceKind, operationID, requestHash, resultJSON)
	return err
}

func canonicalRequestHashForBody(request putLogRetentionSettingsRequest) string {
	return canonicalJSONHash(struct {
		OperationID        string            `json:"operation_id"`
		ExpectedRevision   string            `json:"expected_revision"`
		Policies           retentionPolicies `json:"policies"`
		PreflightTokenHash string            `json:"preflight_token_hash,omitempty"`
		Confirmation       string            `json:"confirmation,omitempty"`
	}{
		OperationID:      request.OperationID,
		ExpectedRevision: request.ExpectedRevision,
		Policies:         request.Policies,
		PreflightTokenHash: func() string {
			if request.PreflightToken == nil || strings.TrimSpace(*request.PreflightToken) == "" {
				return ""
			}
			return hashToken(*request.PreflightToken)
		}(),
		Confirmation: func() string {
			if request.Confirmation == nil {
				return ""
			}
			return request.Confirmation.Keyword
		}(),
	})
}

func canonicalArchiveHash(request archiveRetentionOwnerDriftRequest) string {
	return canonicalHash(request.OperationID, request.ExpectedRevision, request.ExpectedInventoryGeneration, fmt.Sprintf("%v", request.Heads))
}

func canonicalManualJobHash(request createManualRetentionJobRequest) string {
	return canonicalJSONHash(struct {
		OperationID   string `json:"operation_id"`
		PreflightHash string `json:"preflight_token_hash"`
		Confirmation  string `json:"confirmation"`
	}{
		OperationID:   request.OperationID,
		PreflightHash: hashToken(request.PreflightToken),
		Confirmation:  request.Confirmation.Keyword,
	})
}

func canonicalPreflightHash(raw struct {
	Kind                     string                  `json:"kind"`
	OperationID              string                  `json:"operation_id"`
	PreflightAttemptID       string                  `json:"preflight_attempt_id"`
	ExpectedSettingsRevision string                  `json:"expected_settings_revision"`
	Policies                 *retentionPolicies      `json:"policies"`
	Dataset                  string                  `json:"dataset"`
	Selection                *manualCleanupSelection `json:"selection"`
}) string {
	if raw.Kind == "policy_change" && raw.Policies != nil {
		return canonicalPolicyBindingHash(raw.OperationID, raw.ExpectedSettingsRevision, *raw.Policies)
	}
	return canonicalJSONHash(struct {
		Kind        string                  `json:"kind"`
		OperationID string                  `json:"operation_id"`
		Dataset     string                  `json:"dataset"`
		Selection   *manualCleanupSelection `json:"selection"`
	}{
		Kind: raw.Kind, OperationID: raw.OperationID, Dataset: raw.Dataset, Selection: raw.Selection,
	})
}

func canonicalPolicyBindingHash(operationID string, expectedRevision string, policies retentionPolicies) string {
	return canonicalJSONHash(struct {
		Kind             string            `json:"kind"`
		OperationID      string            `json:"operation_id"`
		ExpectedRevision string            `json:"expected_settings_revision"`
		Policies         retentionPolicies `json:"policies"`
	}{
		Kind: "policy_change", OperationID: operationID, ExpectedRevision: expectedRevision, Policies: policies,
	})
}

func canonicalJSONHash(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return canonicalHash(fmt.Sprintf("marshal-error:%T", value))
	}
	return canonicalHash(string(raw))
}

func canonicalOwnerSemanticSnapshotHash(value any) string {
	// The owner snapshot is persisted verbatim for operator evidence. Its
	// coverage revision/hash, materialization cut, bounds, gaps and timestamps
	// are preview evidence, not a second semantic fence: ordinary append-only
	// facts may advance those values without changing the sealed predicate.
	// Semantic generation/state fields remain in the comparison below, so
	// policy/floor/epoch/fence/purge/materializer transitions still require a
	// fresh preflight.
	return canonicalJSONHash(stripOwnerPreviewEvidence(value))
}

func ownerSnapshotGeneration(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if value, ok := object["coverage_revision"].(string); ok {
		return value
	}
	if source, ok := object["retention_source"].(map[string]any); ok {
		if value, ok := source["source_revision"].(string); ok {
			return value
		}
	}
	return ""
}

func stripOwnerPreviewEvidence(value any) any {
	switch item := value.(type) {
	case map[string]any:
		copyObject := make(map[string]any, len(item))
		for key, child := range item {
			switch key {
			case "generated_at", "updated_at", "coverage_revision", "coverage_hash", "materialization_cut", "earliest", "latest", "gaps":
				continue
			}
			if key == "actual_coverage" {
				// Keep the owner semantic readiness labels, but not mutable
				// append-time bounds or the evidence manifest itself.
				if coverage, ok := child.(map[string]any); ok {
					semanticCoverage := map[string]any{}
					for coverageKey, coverageValue := range coverage {
						switch coverageKey {
						case "complete", "freshness", "precision", "source", "gap_reason":
							semanticCoverage[coverageKey] = stripOwnerPreviewEvidence(coverageValue)
						}
					}
					copyObject[key] = semanticCoverage
					continue
				}
			}
			if key == "storage_fact_evidence" {
				// An unavailable bounded fact set is explicitly permitted. A bound
				// generation is semantic evidence and remains comparable.
				if evidence, ok := child.(map[string]any); ok {
					bounded := map[string]any{"state": evidence["state"]}
					if evidence["state"] == "bound" {
						bounded["generation"] = evidence["generation"]
					}
					copyObject[key] = bounded
					continue
				}
			}
			copyObject[key] = stripOwnerPreviewEvidence(child)
		}
		return copyObject
	case []any:
		copyArray := make([]any, len(item))
		for index, child := range item {
			copyArray[index] = stripOwnerPreviewEvidence(child)
		}
		return copyArray
	default:
		return value
	}
}

func canonicalHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// ---- error helpers ----

type settingsConflictError struct {
	code               string
	currentRevision    string
	currentGenerations map[string]string
	operationID        string
}

func (err *settingsConflictError) Error() string { return err.code }

func isJobConflict(err error) bool {
	return strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "retention_job_conflict")
}

func writeSettingsError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var conflict *settingsConflictError
	if asConflict(err, &conflict) {
		switch conflict.code {
		case "retention_settings_changed":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "retention settings changed concurrently", Params: map[string]any{}, Details: map[string]any{"current_revision": conflict.currentRevision, "recovery": "refresh"}}, http.StatusConflict)
		case "retention_preflight_required":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "fresh destructive preflight is required", Params: map[string]any{}, Details: map[string]any{"recovery": "repreview"}}, http.StatusPreconditionRequired)
		case "retention_preflight_stale":
			currentRevision := any(nil)
			if conflict.currentRevision != "" {
				currentRevision = conflict.currentRevision
			}
			generations := conflict.currentGenerations
			if generations == nil {
				generations = map[string]string{}
			}
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "preflight is stale; repreview", Params: map[string]any{}, Details: map[string]any{"recovery": "repreview", "current_revision": currentRevision, "current_generations": generations}}, http.StatusConflict)
		case "operation_id_conflict":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "operation id already used with a different request", Params: map[string]any{}, Details: map[string]any{"operation_id": conflict.operationID, "recovery": "inspect_operation"}}, http.StatusConflict)
		case "operation_outcome_unavailable":
			// A reserved operation without its durable outcome is an internal
			// recovery fault, never a client-visible conflict or a reason to
			// synthesize success.
			writeSettingsInternalError(w, r, corsSnapshot, err)
		case "retention_owner_drift_changed":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "owner drift inventory changed", Params: map[string]any{}, Details: map[string]any{"recovery": "repreview"}}, http.StatusConflict)
		case "retention_job_conflict":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "dataset already has an executing or reserved retention job", Params: map[string]any{}, Details: map[string]any{"recovery": "inspect_operation"}}, http.StatusConflict)
		case "retention_owner_unavailable":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "retention owner snapshot is temporarily unavailable", Params: map[string]any{}, Details: map[string]any{"recovery": "retry", "retry_after_seconds": 5}}, http.StatusServiceUnavailable)
		case "invalid_retention_cutoff":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "invalid retention cutoff", Params: map[string]any{}, Details: map[string]any{"violations": []FieldViolation{{Path: "selection.cutoff", Reason: "must_be_utc_and_not_in_the_future"}}}}, http.StatusUnprocessableEntity)
		case "validation_failed":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "invalid preflight request", Params: map[string]any{}, Details: map[string]any{"violations": []any{}}}, http.StatusUnprocessableEntity)
		default:
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: err.Error(), Params: map[string]any{}}, http.StatusConflict)
		}
		return
	}
	writeSettingsInternalError(w, r, corsSnapshot, err)
}

func asConflict(err error, target **settingsConflictError) bool {
	for err != nil {
		if conflict, ok := err.(*settingsConflictError); ok {
			*target = conflict
			return true
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			err = wrapped.Unwrap()
			continue
		}
		break
	}
	return false
}

func writeSettingsInternalError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if err != nil {
		slog.Error("settings internal error", "error", err)
	}
	writeProblem(w, r, corsSnapshot, SettingsProblem{Code: "internal_error", Detail: "Internal server error", Params: map[string]any{}}, http.StatusInternalServerError)
}
