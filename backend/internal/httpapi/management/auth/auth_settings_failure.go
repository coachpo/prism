package auth

import (
	"context"
	"encoding/json"
	"net/http"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// recordAuthReadinessConflict persists the failed operation after the
// activation transaction rolls back, including a deliberately unusable
// password-bearing version for exact replay verification.
func (s *Service) recordAuthReadinessConflict(ctx context.Context, conflict *authReadinessConflictError) error {
	return pgxutil.InTx(ctx, s.pool, "settings auth readiness conflict", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
			return err
		}
		// Keep the same Proxy -> Auth fence order as successful activation and
		// proxy-key mutations. This outcome is written after the failed
		// activation transaction rolled back, but it must still serialize with a
		// concurrent readiness refresh before publishing its recovery snapshot.
		if err := lockProxyKeyReadiness(ctx, tx); err != nil {
			return err
		}
		row, err := s.readAuthSettings(ctx, tx, true)
		if err != nil {
			return err
		}
		// Keep a deliberately slow verifier for a secret-bearing conflict so
		// an exact replay can prove the same password without persisting the
		// raw secret or a cheap request digest. The version is unusable and is
		// never pointed to by app_auth_settings.
		if conflict.Request.AccountChange.NewPassword != nil {
			hash, hashErr := hashPassword(*conflict.Request.AccountChange.NewPassword)
			if hashErr != nil {
				return hashErr
			}
			username := conflict.Request.AccountChange.Username
			if username == nil {
				username = row.DesiredUsername
			}
			generation := "unusable-" + conflict.OperationID
			if _, err := tx.Exec(ctx, `INSERT INTO auth_config_versions (
				subject_key, generation, desired_mode, username, password_hash, session_version,
				state, created_operation_id, created_at, updated_at
			) VALUES ('app', $1, $2, $3, $4, $5, 'unusable', $6, now(), now())
			ON CONFLICT (subject_key, generation) DO NOTHING`, generation,
				func() string {
					if row.DesiredAuthEnabled {
						return "enabled"
					}
					return "disabled"
				}(),
				username, hash, row.LegacyTokenVersion, conflict.OperationID); err != nil {
				return err
			}
		}
		settingsResponse, err := s.buildAuthSettingsResponse(ctx, tx)
		if err != nil {
			return err
		}
		desiredGeneration := "1"
		if row.DesiredGeneration != nil {
			desiredGeneration = *row.DesiredGeneration
		}
		effectiveGeneration := "1"
		if row.EffectiveGeneration != nil {
			effectiveGeneration = *row.EffectiveGeneration
		}
		result := authOperationResult{
			OperationID:         conflict.OperationID,
			State:               "rolled_back",
			DesiredGeneration:   desiredGeneration,
			EffectiveGeneration: effectiveGeneration,
			Retryable:           false,
			SafeError: &struct {
				Code              string `json:"code"`
				RetryAfterSeconds *int   `json:"retry_after_seconds"`
			}{Code: conflict.domainError.Code, RetryAfterSeconds: nil},
			ReadinessConflict: &authReadinessConflictResult{
				Code:                     conflict.domainError.Code,
				CurrentProxyKeyReadiness: readinessDTO(conflict.Readiness),
				RequiredAcknowledgements: []string{"enable_without_active_proxy_keys"},
				NewOperationIDRequired:   true,
			},
			SessionAction: "none",
			Settings:      settingsResponse,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO settings_mutation_operations (
			resource_kind, operation_id, request_hash, state, result_json, created_at, updated_at
		) VALUES ('auth_settings', $1, $2, 'rolled_back', $3, now(), now())
		ON CONFLICT (resource_kind, operation_id) DO NOTHING`, conflict.OperationID, canonicalAuthRequestHash(conflict.Request), raw)
		return err
	})
}

func readinessConflictProblem(readiness proxyKeyReadinessSnapshot, code string, operationID string) error {
	details := map[string]any{
		"operation_id":                operationID,
		"operation_recorded":          false,
		"new_operation_id_required":   true,
		"required_acknowledgements":   []string{"enable_without_active_proxy_keys"},
		"current_proxy_key_readiness": readinessDTO(readiness),
		"recovery":                    "review_and_resubmit",
	}
	return &authReadinessConflictError{
		domainError: authSettingsProblem(http.StatusConflict, code, code, map[string]any{"details": details}).(*domainError),
		OperationID: operationID,
		Readiness:   readiness,
	}
}
