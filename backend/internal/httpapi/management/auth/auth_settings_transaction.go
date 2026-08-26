package auth

import (
	"context"
	"fmt"
	"net/http"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// executeAuthSettingsMutation owns the one bounded transaction for an auth
// settings PUT. The transaction is intentionally not split into separate
// transactions: writer admission,
// Proxy readiness fencing, immutable staging, session revocation, operation
// recording, and pointer publication retain their existing order.
func (s *Service) executeAuthSettingsMutation(ctx context.Context, request putAuthSettingsRequest) (authSettingsMutationOutcome, error) {
	outcome := authSettingsMutationOutcome{}
	err := pgxutil.InTx(ctx, s.pool, "settings auth put", func(tx pgx.Tx) error {
		if err := auditdomain.AcquireAffectedWriterAdmission(ctx, tx); err != nil {
			return authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		// Proxy readiness is the first domain fence. Every auth staging and
		// activation path takes it before the auth control singleton, matching
		// the ordering used by proxy-key mutations.
		if err := lockProxyKeyReadiness(ctx, tx); err != nil {
			return authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		// Replay is checked before the current revision. A lost response must
		// remain replayable even after the successful operation advanced it.
		handled, err := resolveAuthSettingsReplay(ctx, tx, request, &outcome)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}

		row, err := s.readAuthSettings(ctx, tx, true)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%d", row.Revision) != request.ExpectedRevision {
			return authSettingsProblem(http.StatusConflict, "auth_settings_changed", "auth_settings_changed", map[string]any{
				"details": map[string]any{"current_revision": fmt.Sprintf("%d", row.Revision), "recovery": "refresh"},
			})
		}
		if row.TransitionState != nil {
			effectiveGeneration := "1"
			if row.EffectiveGeneration != nil {
				effectiveGeneration = *row.EffectiveGeneration
			}
			return authSettingsProblem(http.StatusConflict, "auth_transition_in_progress", "auth_transition_in_progress", map[string]any{
				"details": map[string]any{
					"transition_state":     *row.TransitionState,
					"effective_generation": effectiveGeneration,
					"recovery":             "inspect_existing_transition",
					"retry_after_seconds":  nil,
				},
			})
		}
		if err := validateAuthSettingsMutationKind(request); err != nil {
			return err
		}

		readiness, readinessErr := s.captureProxyKeyReadiness(ctx, tx)
		if readinessErr != nil {
			return authSettingsProblem(http.StatusServiceUnavailable, "auth_readiness_unavailable", "auth_readiness_unavailable", map[string]any{
				"details": map[string]any{"recovery": "retry", "retry_after_seconds": 5},
			})
		}
		input, err := prepareAuthSettingsMutation(request, row, readiness)
		if err != nil {
			return err
		}

		draft, err := s.stageAuthSettingsVersionInTransaction(ctx, tx, row, request, readiness, input)
		if err != nil {
			return err
		}
		if err := s.supersedeAuthSettingsVersionInTransaction(ctx, tx, row, draft); err != nil {
			return err
		}
		if err := s.revokeAuthSettingsSessionsInTransaction(ctx, tx, row, input, draft.commitAt); err != nil {
			return err
		}
		legacyColumnsExist, err := authSettingsLegacyColumnsExistInTransaction(ctx, tx)
		if err != nil {
			return err
		}
		outcome.result = buildAuthSettingsMutationResult(row, request, readiness, input, draft)
		if err := recordAuthSettingsOperationInTransaction(ctx, tx, request, outcome.result, draft.commitAt); err != nil {
			return err
		}
		var readinessGeneration int64
		if input.enabling {
			if _, scanErr := fmt.Sscanf(readiness.Generation, "%d", &readinessGeneration); scanErr != nil {
				return fmt.Errorf("invalid proxy key readiness generation: %w", scanErr)
			}
		}
		// This call performs the final database statement on the successful
		// path. The surrounding InTx commit remains the linearization point.
		return publishAuthSettingsPointerInTransaction(ctx, tx, row, request, readiness, input, draft, legacyColumnsExist, readinessGeneration)
	})
	return outcome, err
}
