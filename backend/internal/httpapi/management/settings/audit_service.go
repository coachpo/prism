package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

// audit settings (SPEC §9): three-state family policies, group revision/CAS,
// durable operation identity and the bounded logical storage summary.

const (
	auditModeDisabled     = "disabled"
	auditModeMetadataOnly = "metadata_only"
	auditModeBodyCapture  = "body_capture"
)

var auditFamilies = []string{"openai", "anthropic", "gemini"}

func modeFromFlags(enabled, captureBodies bool) string {
	if !enabled {
		return auditModeDisabled
	}
	if captureBodies {
		return auditModeBodyCapture
	}
	return auditModeMetadataOnly
}

func flagsFromMode(mode string) (bool, bool) {
	switch mode {
	case auditModeBodyCapture:
		return true, true
	case auditModeMetadataOnly:
		return true, false
	default:
		return false, false
	}
}

type auditGroupState struct {
	Revision  int64
	UpdatedAt time.Time
}

func (s *Service) loadAuditGroupStateDetails(ctx context.Context, tx pgx.Tx, profileID int, forUpdate bool) (auditGroupState, error) {
	var state auditGroupState
	query := `SELECT revision, updated_at FROM profile_audit_settings_state WHERE profile_id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, profileID).Scan(&state.Revision, &state.UpdatedAt)
	if err == nil {
		return state, nil
	}
	if err != pgx.ErrNoRows {
		return auditGroupState{}, err
	}
	if !forUpdate {
		// Read-only projection: a missing group row means the profile has no
		// saved audit settings yet; the default revision is 1 without writing
		// inside the read-only transaction.
		return auditGroupState{Revision: 1, UpdatedAt: s.now().UTC()}, nil
	}
	// Fresh profiles get a generation-1 group state lazily on first write.
	if _, err := tx.Exec(ctx, `INSERT INTO profile_audit_settings_state (profile_id, revision, writer_generation, updated_at)
		VALUES ($1, 1, 1, now()) ON CONFLICT (profile_id) DO NOTHING`, profileID); err != nil {
		return auditGroupState{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT revision, updated_at FROM profile_audit_settings_state WHERE profile_id = $1 FOR UPDATE`, profileID).Scan(&state.Revision, &state.UpdatedAt); err != nil {
		return auditGroupState{}, err
	}
	return state, nil
}

func (s *Service) loadAuditGroupState(ctx context.Context, tx pgx.Tx, profileID int, forUpdate bool) (int64, error) {
	state, err := s.loadAuditGroupStateDetails(ctx, tx, profileID, forUpdate)
	return state.Revision, err
}

func (s *Service) handleGetAuditSettings(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "settings audit read", func(tx pgx.Tx) (targetAuditSettingsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return targetAuditSettingsResponse{}, err
		}
		groupState, err := s.loadAuditGroupStateDetails(r.Context(), tx, profile.ID, false)
		if err != nil {
			return targetAuditSettingsResponse{}, err
		}
		rows, err := tx.Query(r.Context(), `SELECT api_family, audit_enabled, audit_capture_bodies
			FROM profile_api_family_audit_settings WHERE profile_id = $1 ORDER BY api_family ASC`, profile.ID)
		if err != nil {
			return targetAuditSettingsResponse{}, err
		}
		defer rows.Close()
		byFamily := map[string]string{}
		for rows.Next() {
			var family string
			var enabled, capture bool
			if err := rows.Scan(&family, &enabled, &capture); err != nil {
				return targetAuditSettingsResponse{}, err
			}
			byFamily[family] = modeFromFlags(enabled, capture)
		}
		if err := rows.Err(); err != nil {
			return targetAuditSettingsResponse{}, err
		}
		policies := []auditPolicyRow{}
		for _, family := range auditFamilies {
			mode, ok := byFamily[family]
			if !ok {
				mode = auditModeDisabled // legacy projection: missing family behaves as disabled
			}
			policies = append(policies, auditPolicyRow{Family: family, Mode: mode})
		}
		return targetAuditSettingsResponse{
			Revision:  fmt.Sprintf("%d", groupState.Revision),
			UpdatedAt: groupState.UpdatedAt.UTC().Format(time.RFC3339),
			Policies:  policies,
			FixedCaptureLimits: map[string]int64{
				"per_request_body_bytes":               4 * 1024 * 1024,
				"aggregate_request_body_bytes":         12 * 1024 * 1024,
				"final_response_body_bytes":            4 * 1024 * 1024,
				"aggregate_raw_body_bytes_per_ingress": 16 * 1024 * 1024,
			},
		}, nil
	})
	if err != nil {
		writeSettingsInternalError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}

func (s *Service) handlePutAuditSettings(w http.ResponseWriter, r *http.Request) {
	var request putAuditSettingsRequest
	if err := decodeStrictJSONBody(r, &request); err != nil {
		writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "validation_failed", Detail: "Invalid request body", Params: map[string]any{}}, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.OperationID) == "" {
		writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "validation_failed", Detail: "operation_id is required", Params: map[string]any{}}, http.StatusUnprocessableEntity)
		return
	}
	// Exactly one unique row per family with a valid mode.
	seen := map[string]bool{}
	for _, policy := range request.Policies {
		if !isAuditFamily(policy.Family) {
			writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "invalid_audit_policy", Detail: "unknown audit family", Params: map[string]any{}, Details: map[string]any{"violations": []FieldViolation{{Path: "policies.family", Reason: "unsupported"}}}}, http.StatusUnprocessableEntity)
			return
		}
		if seen[policy.Family] {
			writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "invalid_audit_policy", Detail: "duplicate audit family", Params: map[string]any{}, Details: map[string]any{"violations": []FieldViolation{{Path: "policies.family", Reason: "duplicate"}}}}, http.StatusUnprocessableEntity)
			return
		}
		seen[policy.Family] = true
		switch policy.Mode {
		case auditModeDisabled, auditModeMetadataOnly, auditModeBodyCapture:
		default:
			writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "invalid_audit_policy", Detail: "unknown audit mode", Params: map[string]any{}, Details: map[string]any{"violations": []FieldViolation{{Path: "policies.mode", Reason: "unsupported"}}}}, http.StatusUnprocessableEntity)
			return
		}
	}
	if len(seen) != 3 {
		writeProblem(w, r, s.corsSnapshot(), SettingsProblem{Code: "invalid_audit_policy", Detail: "settings must include exactly openai, anthropic, and gemini", Params: map[string]any{}, Details: map[string]any{"violations": []FieldViolation{{Path: "policies", Reason: "must_include_all_families"}}}}, http.StatusUnprocessableEntity)
		return
	}

	var result putAuditSettingsResponse
	replayed := false
	err := pgxutil.InTx(r.Context(), s.pool, "settings audit put", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(r.Context(), tx); err != nil {
			return newSettingsMutationError(http.StatusServiceUnavailable, SettingsProblem{
				Code:    "settings_owner_unavailable",
				Detail:  "Requests/Audit writer admission is temporarily unavailable",
				Params:  map[string]any{},
				Details: map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		// Replay is checked before the current revision so a lost response stays
		// replayable after the successful write advanced the group revision.
		var operationRow struct {
			RequestHash string
			ResultJSON  []byte
		}
		err := tx.QueryRow(r.Context(), `SELECT request_hash, result_json FROM settings_mutation_operations
			WHERE resource_kind = 'audit_settings' AND operation_id = $1`, request.OperationID).
			Scan(&operationRow.RequestHash, &operationRow.ResultJSON)
		if err == nil {
			hash := canonicalAuditHash(request)
			if operationRow.RequestHash == hash {
				replayed = true
				if len(operationRow.ResultJSON) > 0 {
					return json.Unmarshal(operationRow.ResultJSON, &result)
				}
				return nil
			}
			return newSettingsMutationError(http.StatusConflict, SettingsProblem{
				Code:    "operation_id_conflict",
				Detail:  "operation id already used with a different request",
				Params:  map[string]any{},
				Details: map[string]any{"operation_id": request.OperationID, "recovery": "inspect_operation"},
			})
		}
		if err != pgx.ErrNoRows {
			return err
		}

		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return err
		}
		revision, err := s.loadAuditGroupState(r.Context(), tx, profile.ID, true)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", revision) != request.ExpectedRevision {
			return newSettingsMutationError(http.StatusConflict, SettingsProblem{
				Code:    "audit_settings_changed",
				Detail:  "audit settings changed concurrently",
				Params:  map[string]any{},
				Details: map[string]any{"current_revision": fmt.Sprintf("%d", revision), "recovery": "refresh"},
			})
		}

		// Upsert preserving the immutable migration provenance.
		for _, policy := range request.Policies {
			enabled, capture := flagsFromMode(policy.Mode)
			if _, err := tx.Exec(r.Context(), `INSERT INTO profile_api_family_audit_settings (
				profile_id, api_family, audit_enabled, audit_capture_bodies, migration_provenance, created_at, updated_at
			) VALUES ($1, $2, $3, $4, 'explicit', now(), now())
			ON CONFLICT ON CONSTRAINT uq_profile_api_family_audit_settings_profile_family DO UPDATE
			SET audit_enabled = $3, audit_capture_bodies = $4, updated_at = now()`,
				profile.ID, policy.Family, enabled, capture); err != nil {
				return err
			}
		}
		updatedAt := s.now().UTC()
		commandTag, err := tx.Exec(r.Context(), `UPDATE profile_audit_settings_state SET revision = revision + 1, updated_at = $3
			WHERE profile_id = $1 AND revision = $2`, profile.ID, revision, updatedAt)
		if err != nil {
			return err
		}
		if commandTag.RowsAffected() != 1 {
			return newSettingsMutationError(http.StatusConflict, SettingsProblem{
				Code:    "audit_settings_changed",
				Detail:  "audit settings changed concurrently",
				Params:  map[string]any{},
				Details: map[string]any{"recovery": "refresh"},
			})
		}

		newRevision := revision + 1
		policies := []auditPolicyRow{}
		for _, family := range auditFamilies {
			mode := auditModeDisabled
			for _, policy := range request.Policies {
				if policy.Family == family {
					mode = policy.Mode
				}
			}
			policies = append(policies, auditPolicyRow{Family: family, Mode: mode})
		}
		result = putAuditSettingsResponse{
			OperationID: request.OperationID,
			Replayed:    false,
			Settings: targetAuditSettingsResponse{
				Revision:  fmt.Sprintf("%d", newRevision),
				UpdatedAt: updatedAt.Format(time.RFC3339),
				Policies:  policies,
				FixedCaptureLimits: map[string]int64{
					"per_request_body_bytes":               4 * 1024 * 1024,
					"aggregate_request_body_bytes":         12 * 1024 * 1024,
					"final_response_body_bytes":            4 * 1024 * 1024,
					"aggregate_raw_body_bytes_per_ingress": 16 * 1024 * 1024,
				},
			},
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO settings_mutation_operations (
			resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
		) VALUES ('audit_settings', $1, $2, 'completed', $3, now(), now())
		ON CONFLICT (resource_kind, operation_id) DO NOTHING`,
			request.OperationID, canonicalAuditHash(request), raw)
		return err
	})
	if err != nil {
		if writeSettingsMutationError(w, r, s.corsSnapshot(), err) {
			return
		}
		writeSettingsInternalError(w, r, s.corsSnapshot(), err)
		return
	}
	result.Replayed = replayed
	writeSettingsJSON(w, http.StatusOK, result)
}

func isAuditFamily(value string) bool {
	for _, family := range auditFamilies {
		if family == value {
			return true
		}
	}
	return false
}

func canonicalAuditHash(request putAuditSettingsRequest) string {
	parts := []string{request.OperationID, request.ExpectedRevision}
	for _, policy := range request.Policies {
		parts = append(parts, policy.Family+":"+policy.Mode)
	}
	return canonicalHash(parts...)
}

// handleGetAuditStorageSummary: bounded logical storage facts + owner
// projections in one shared-fence RR snapshot (SPEC §9.4).
func (s *Service) handleGetAuditStorageSummary(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "settings audit storage summary", func(tx pgx.Tx) (map[string]any, error) {
		now := s.now().UTC()
		source, err := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, "audit_logs", now)
		if err != nil {
			return nil, err
		}
		protection, err := auditdomain.LoadAuditFenceMaterializerProjection(r.Context(), tx, now)
		if err != nil {
			return nil, err
		}
		var factState struct {
			CurrentGeneration *string
			FactsComplete     bool
			LastFactDay       *time.Time
			GeneratedAt       *time.Time
		}
		if err := tx.QueryRow(r.Context(), `SELECT current_generation, facts_complete, last_fact_day, generated_at
			FROM audit_storage_fact_state WHERE id = 1`).Scan(
			&factState.CurrentGeneration, &factState.FactsComplete, &factState.LastFactDay, &factState.GeneratedAt); err != nil {
			return nil, err
		}

		response := map[string]any{
			"source_revision":             source.SourceRevision,
			"generated_at":                now.Format(time.RFC3339),
			"retention_source":            retentionSourceProjectionMap(source),
			"audit_protection":            protection,
			"retained_rows":               nil,
			"logical_header_bytes":        nil,
			"logical_body_bytes":          nil,
			"last_7d_logical_bytes_added": nil,
			"sampled_days":                0,
			"daily_average_logical_bytes": nil,
			"precision":                   "unavailable",
			"freshness":                   "partial",
		}

		if factState.CurrentGeneration != nil && factState.FactsComplete {
			var factCount int64
			var factRevisionMismatch bool
			if err := tx.QueryRow(r.Context(), `SELECT COUNT(*), COALESCE(bool_or(observe_source_revision <> $2), FALSE)
				FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`,
				*factState.CurrentGeneration, source.SourceRevision).Scan(&factCount, &factRevisionMismatch); err != nil {
				return nil, err
			}
			if factCount == 0 || factRevisionMismatch {
				reason := "facts_not_ready"
				if factRevisionMismatch {
					reason = "source_revision_mismatch"
				}
				response["storage_fact_evidence"] = map[string]any{"state": "unavailable", "reason_code": reason}
				return response, nil
			}
			var facts struct {
				TotalRows     int64
				HeaderBytes   int64
				BodyBytes     int64
				SevenDayBytes int64
				DayCount      int
				SevenDayCount int
			}
			windowStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7)
			windowEnd := windowStart.AddDate(0, 0, 7)
			err := tx.QueryRow(r.Context(), `SELECT COALESCE(SUM(logical_rows),0), COALESCE(SUM(logical_header_bytes),0),
				COALESCE(SUM(logical_body_bytes),0),
				COALESCE(SUM(logical_header_bytes + logical_body_bytes) FILTER (WHERE utc_day >= $2::date AND utc_day < $3::date),0),
				COUNT(*), COUNT(*) FILTER (WHERE utc_day >= $2::date AND utc_day < $3::date)
				FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`, *factState.CurrentGeneration,
				windowStart.Format("2006-01-02"), windowEnd.Format("2006-01-02")).
				Scan(&facts.TotalRows, &facts.HeaderBytes, &facts.BodyBytes, &facts.SevenDayBytes, &facts.DayCount, &facts.SevenDayCount)
			if err == nil {
				total := fmt.Sprintf("%d", facts.TotalRows)
				header := fmt.Sprintf("%d", facts.HeaderBytes)
				body := fmt.Sprintf("%d", facts.BodyBytes)
				response["retained_rows"] = &total
				response["logical_header_bytes"] = &header
				response["logical_body_bytes"] = &body
				response["sampled_days"] = facts.DayCount
				if facts.SevenDayCount == 7 {
					sevenDay := fmt.Sprintf("%d", facts.SevenDayBytes)
					average := fmt.Sprintf("%d", facts.SevenDayBytes/7)
					response["last_7d_logical_bytes_added"] = &sevenDay
					response["daily_average_logical_bytes"] = &average
				}
				response["precision"] = "exact"
				response["freshness"] = "fresh"
				response["storage_fact_evidence"] = map[string]any{"state": "bound", "generation": *factState.CurrentGeneration}
			} else {
				response["storage_fact_evidence"] = map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}
			}
		} else {
			response["storage_fact_evidence"] = map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}
		}
		return response, nil
	})
	if err != nil {
		writeSettingsInternalError(w, r, s.corsSnapshot(), err)
		return
	}
	writeSettingsJSON(w, http.StatusOK, response)
}
