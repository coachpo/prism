package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type authSettingsVersionDraft struct {
	configID          int64
	newAuthGeneration int64
	newGeneration     string
	sessionVersion    int64
	username          *string
	passwordHash      *string
	desiredMode       string
	commitAt          time.Time
}

// stageAuthSettingsVersionInTransaction owns immutable config-version
// creation and its staged-to-effective state transition.
func (s *Service) stageAuthSettingsVersionInTransaction(ctx context.Context, tx pgx.Tx, row authSettingsReadRow, request putAuthSettingsRequest, readiness proxyKeyReadinessSnapshot, input authSettingsMutationInput) (authSettingsVersionDraft, error) {
	newAuthGeneration := nextGenerationInt(row.EffectiveGeneration)
	newGeneration := fmt.Sprintf("%d", newAuthGeneration)
	sessionVersion := row.LegacyTokenVersion
	if input.accountUpdate || input.disabling && row.LegacyAuthEnabled || input.enabling && !row.LegacyAuthEnabled {
		sessionVersion++
	}
	var passwordHash *string
	if input.newPassword != nil && strings.TrimSpace(*input.newPassword) != "" {
		hash, err := hashPassword(*input.newPassword)
		if err != nil {
			return authSettingsVersionDraft{}, err
		}
		passwordHash = &hash
	} else if input.accountUpdate {
		passwordHash = row.LegacyPasswordHash
	} else {
		passwordHash = row.LegacyPasswordHash
	}
	username := input.username
	if username == nil {
		username = row.LegacyUsername
	}
	desiredMode := "disabled"
	if request.DesiredAuthEnabled {
		desiredMode = "enabled"
	}
	var configID int64
	if err := tx.QueryRow(ctx, `INSERT INTO auth_config_versions (
		subject_key, generation, desired_mode, username, password_hash, session_version,
		state, created_operation_id, readiness_generation, counted_at, active_count,
		expired_count, disabled_count, safe_active_count, zero_key_acknowledged,
		created_at, updated_at
	) VALUES ('app', $1, $2, $3, $4, $5, 'staged', $6, $7, $8, $9, $10, $11, $12, $13, now(), now())
	RETURNING id`, newGeneration, desiredMode, username, passwordHash, sessionVersion,
		request.OperationID,
		func() *string {
			if readiness.Generation == "" {
				return nil
			}
			return &readiness.Generation
		}(),
		func() *time.Time {
			if readiness.CountedAt.IsZero() {
				return nil
			}
			return &readiness.CountedAt
		}(),
		func() *string {
			if readiness.Generation == "" {
				return nil
			}
			value := fmt.Sprintf("%d", readiness.Active)
			return &value
		}(),
		func() *string {
			if readiness.Generation == "" {
				return nil
			}
			value := fmt.Sprintf("%d", readiness.Expired)
			return &value
		}(),
		func() *string {
			if readiness.Generation == "" {
				return nil
			}
			value := fmt.Sprintf("%d", readiness.Disabled)
			return &value
		}(),
		func() *string {
			if readiness.Generation == "" {
				return nil
			}
			value := fmt.Sprintf("%d", readiness.SafeActive)
			return &value
		}(),
		input.zeroKeyAcknowledged).Scan(&configID); err != nil {
		return authSettingsVersionDraft{}, err
	}
	commitAt := s.nowUTC()
	if _, err := tx.Exec(ctx, `UPDATE auth_config_versions SET state = 'effective', updated_at = $2 WHERE id = $1`, configID, commitAt); err != nil {
		return authSettingsVersionDraft{}, err
	}
	return authSettingsVersionDraft{
		configID:          configID,
		newAuthGeneration: newAuthGeneration,
		newGeneration:     newGeneration,
		sessionVersion:    sessionVersion,
		username:          username,
		passwordHash:      passwordHash,
		desiredMode:       desiredMode,
		commitAt:          commitAt,
	}, nil
}

// supersedeAuthSettingsVersionInTransaction closes the prior effective
// immutable version before the new pointer is published.
func (s *Service) supersedeAuthSettingsVersionInTransaction(ctx context.Context, tx pgx.Tx, row authSettingsReadRow, draft authSettingsVersionDraft) error {
	if row.EffectiveConfigID != nil && *row.EffectiveConfigID != draft.configID {
		_, err := tx.Exec(ctx, `UPDATE auth_config_versions SET state = 'superseded', updated_at = $2
			WHERE id = $1 AND state = 'effective'`, *row.EffectiveConfigID, draft.commitAt)
		return err
	}
	return nil
}

// revokeAuthSettingsSessionsInTransaction keeps session invalidation in the
// same transaction and before the guarded effective-pointer publication.
func (s *Service) revokeAuthSettingsSessionsInTransaction(ctx context.Context, tx pgx.Tx, row authSettingsReadRow, input authSettingsMutationInput, revokedAt time.Time) error {
	if (input.disabling && row.LegacyAuthEnabled) || (input.accountUpdate && row.LegacyAuthEnabled) {
		_, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2
			WHERE auth_subject_id = $1 AND revoked_at IS NULL`, row.ID, revokedAt)
		return err
	}
	return nil
}

func nextGenerationInt(current *string) int64 {
	var number int64
	if current != nil {
		if _, err := fmt.Sscanf(*current, "%d", &number); err == nil {
			return number + 1
		}
	}
	// Non-numeric legacy generation (e.g. test seeds): fall back to a
	// monotonically increasing timestamp so the generation stays unique and
	// strictly increasing.
	return time.Now().UTC().UnixNano()
}
