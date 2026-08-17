package models

import (
	"context"
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleListModelTargets(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelAccessTargetResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		model, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		return loadModelTargetResponses(r.Context(), tx, profile.ID, model.ID, s.now().UTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) accessTargetMutationEnvelopeFor(ctx context.Context, tx pgx.Tx, profileID int, modelConfigID int, targets []modelAccessTargetResponse) (accessTargetMutationEnvelope, error) {
	warnings, err := modelMutationWarnings(ctx, tx, profileID, modelConfigID)
	if err != nil {
		return accessTargetMutationEnvelope{}, err
	}
	if targets == nil {
		targets = []modelAccessTargetResponse{}
	}
	return accessTargetMutationEnvelope{AccessTargets: targets, ConfigurationWarnings: warnings}, nil
}

func (s *Service) handleCreateModelTarget(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	requestBody, err := decodeAccessTargetCreateRequest(r)
	if err != nil {
		writeDecodeError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (accessTargetMutationEnvelope, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		request, err := accessTargetRequestFromCreate(requestBody, len(items))
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		items, err = insertAccessTargetMutationItem(items, request)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		targets, err := s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		warnings, err := modelMutationWarnings(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		return accessTargetMutationEnvelope{AccessTargets: targets, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func (s *Service) handleUpdateModelTarget(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := routeInt(r, "target_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	requestBody, err := decodeAccessTargetUpdateRequest(r)
	if err != nil {
		writeDecodeError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (accessTargetMutationEnvelope, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		items, err = updateAccessTargetMutationItem(items, targetID, requestBody)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		var targets []modelAccessTargetResponse
		if isAccessTargetMetadataOnlyUpdate(requestBody) {
			targets, err = s.updateModelTargetMetadataFromMutationItems(r.Context(), tx, profile.ID, model, items)
		} else {
			targets, err = s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
		}
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		warnings, err := modelMutationWarnings(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		return accessTargetMutationEnvelope{AccessTargets: targets, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleMoveModelTargetPosition(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := routeInt(r, "target_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody modelAccessTargetMoveRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (accessTargetMutationEnvelope, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		items, err = moveAccessTargetMutationItem(items, targetID, requestBody.ToIndex)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		targets, err := s.updateModelTargetMetadataFromMutationItems(r.Context(), tx, profile.ID, model, items)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		warnings, err := modelMutationWarnings(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		return accessTargetMutationEnvelope{AccessTargets: targets, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteModelTarget(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	targetID, err := routeInt(r, "target_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (accessTargetMutationEnvelope, error) {
		profile, model, items, err := s.loadModelTargetMutationState(r.Context(), tx, r, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		deletedPrivateConnection, err := s.deletePrivateConnectionTargetFromMutationItems(r.Context(), tx, profile.ID, model, targetID, items)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		var targets []modelAccessTargetResponse
		if deletedPrivateConnection {
			targets, err = loadModelTargetResponses(r.Context(), tx, profile.ID, model.ID, s.now().UTC())
			if err != nil {
				return accessTargetMutationEnvelope{}, err
			}
		} else {
			items, err = deleteAccessTargetMutationItem(items, targetID)
			if err != nil {
				return accessTargetMutationEnvelope{}, err
			}
			targets, err = s.replaceModelTargetsFromMutationItems(r.Context(), tx, profile.ID, model, items)
			if err != nil {
				return accessTargetMutationEnvelope{}, err
			}
		}
		warnings, err := modelMutationWarnings(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return accessTargetMutationEnvelope{}, err
		}
		return accessTargetMutationEnvelope{AccessTargets: targets, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
