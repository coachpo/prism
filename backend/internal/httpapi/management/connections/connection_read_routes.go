package connections

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleListConnectionsBatch(w http.ResponseWriter, r *http.Request) {
	var requestBody modelConnectionsBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	normalizedModelIDs := dedupeIntValues(requestBody.ModelConfigIDs)
	if len(normalizedModelIDs) == 0 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "model_config_ids must contain at least one model config id")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (modelConnectionsBatchResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		if err := ensureModelConfigIDsExist(r.Context(), tx, profile.ID, normalizedModelIDs); err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		connectionsByModel, err := listConnectionsByModelIDs(r.Context(), tx, profile.ID, normalizedModelIDs, s.now().UTC())
		if err != nil {
			return modelConnectionsBatchResponse{}, err
		}
		items := make([]modelConnectionsBatchItem, 0, len(normalizedModelIDs))
		for _, modelConfigID := range normalizedModelIDs {
			connections := connectionsByModel[modelConfigID]
			if connections == nil {
				connections = []connectionResponse{}
			}
			items = append(items, modelConnectionsBatchItem{ModelConfigID: modelConfigID, Connections: maskConnectionsForWire(connections)})
		}
		return modelConnectionsBatchResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListModelConnections(w http.ResponseWriter, r *http.Request) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		owner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		connections, err := listConnectionsForModel(r.Context(), tx, profile.ID, owner.ID, s.now().UTC())
		if err != nil {
			return nil, err
		}
		return maskConnectionsForWire(connections), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListConnections(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) ([]connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		connections, err := listConnections(r.Context(), tx, profile.ID, s.now().UTC())
		if err != nil {
			return nil, err
		}
		return maskConnectionsForWire(connections), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionResponse{}, err
		}
		connection, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false, s.now().UTC())
		if err != nil {
			return connectionResponse{}, err
		}
		if !found {
			return connectionResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		return connection.maskedForWire(), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func dedupeIntValues(values []int) []int {
	seen := map[int]struct{}{}
	items := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}
