package connections

import (
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleListConnectionReferences(w http.ResponseWriter, r *http.Request) {
	connectionID, err := routeInt(r, "connection_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (connectionReferencesResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return connectionReferencesResponse{}, err
		}
		if _, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, false, s.now().UTC()); err != nil {
			return connectionReferencesResponse{}, err
		} else if !found {
			return connectionReferencesResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
		}
		records, err := listConnectionReferenceRows(r.Context(), tx, profile.ID, connectionID)
		if err != nil {
			return connectionReferencesResponse{}, err
		}
		return connectionReferencesResponse{ConnectionID: connectionID, Items: connectionReferenceResponsesFromRecords(records)}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func connectionReferenceResponsesFromRecords(records []connectionReferenceRecord) []connectionReferenceResponse {
	items := make([]connectionReferenceResponse, 0, len(records))
	for _, record := range records {
		items = append(items, connectionReferenceResponse{TargetID: record.TargetID, ModelConfigID: record.ModelConfigID, ModelID: record.ModelID, APIFamily: record.APIFamily, Position: record.Position, IsEnabled: record.IsEnabled})
	}
	return items
}

func joinConnectionReferenceModelIDs(records []connectionReferenceRecord) string {
	modelIDs := make([]string, 0, len(records))
	seen := map[string]struct{}{}
	for _, record := range records {
		if _, ok := seen[record.ModelID]; ok {
			continue
		}
		seen[record.ModelID] = struct{}{}
		modelIDs = append(modelIDs, record.ModelID)
	}
	return strings.Join(modelIDs, ", ")
}
