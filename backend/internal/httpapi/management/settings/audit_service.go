package settings

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

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

type auditGroupState struct {
	Revision  int64
	UpdatedAt time.Time
}

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

	result, replayed, err := s.putAuditSettingsInTransaction(r.Context(), r, request)
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
