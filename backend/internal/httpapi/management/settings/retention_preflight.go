package settings

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/jackc/pgx/v5"
)

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
