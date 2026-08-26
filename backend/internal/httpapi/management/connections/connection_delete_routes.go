package connections

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleDeleteModelConnection(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (deletedConnectionMutationEnvelope, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, true)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if !found {
			return deletedConnectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		if err := lockProfileAccessTargetRows(r.Context(), tx, profile.ID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		current, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true, s.now().UTC())
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if !found {
			return deletedConnectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		reference, found, err := loadConnectionOwnerReference(r.Context(), tx, profile.ID, owner.ID, current.ID, true)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if !found {
			return deletedConnectionMutationEnvelope{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found for owner model"}
		}
		if err := ensureOwnerConnectionDeleteAllowed(r.Context(), tx, profile.ID, owner, reference.TargetID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if err := deleteModelAccessTargetRow(r.Context(), tx, reference.TargetID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if err := deleteTerminalTarget(r.Context(), tx, current.ID); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		if err := compactModelAccessTargetPositions(r.Context(), tx, profile.ID, owner.ID, s.nowUTC()); err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		accessTargets, err := loadOwnerMutationAccessTargets(r.Context(), tx, profile.ID, owner.ID)
		if err != nil {
			return deletedConnectionMutationEnvelope{}, err
		}
		return deletedConnectionMutationEnvelope{Deleted: true, AccessTargets: accessTargets, ConfigurationWarnings: []modelrouting.ConfigurationWarning{}}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func ensureOwnerConnectionDeleteAllowed(ctx context.Context, exec queryExecutor, profileID int, owner modelRecord, deletingTargetID int) error {
	if !owner.IsEnabled {
		return nil
	}
	enabledCount, err := countEnabledModelAccessTargetsExcluding(ctx, exec, profileID, owner.ID, deletingTargetID)
	if err != nil {
		return err
	}
	if enabledCount == 0 {
		return &DomainError{StatusCode: http.StatusBadRequest, Detail: "enabled models must include at least one enabled access target"}
	}
	return nil
}
