package models

import (
	"net/http"
	"sort"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleModelsByEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) ([]modelConfigListResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		rows, err := listEndpointModelRows(r.Context(), tx, profile.ID, []int{endpointID})
		if err != nil {
			return nil, err
		}
		records, counts := collectEndpointModelCounts(rows)
		sort.Slice(records, func(left int, right int) bool {
			return records[left].ModelID < records[right].ModelID
		})
		strategies, accessTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, records)
		if err != nil {
			return nil, err
		}
		summaries := map[int]modelrouting.RoutingSummary{}
		if err := attachRoutingSummaries(records, accessTargets, strategies, summaries); err != nil {
			return nil, err
		}
		response := make([]modelConfigListResponse, 0, len(records))
		for _, record := range records {
			item := buildModelListResponse(record, strategies, accessTargets, counts, health, s.now().UTC())
			if summary, ok := summaries[record.ID]; ok {
				summary := summary
				item.RoutingSummary = &summary
			}
			response = append(response, item)
		}
		return response, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleModelsByEndpoints(w http.ResponseWriter, r *http.Request) {
	var requestBody endpointModelsBatchRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (endpointModelsBatchResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointModelsBatchResponse{}, err
		}
		if len(requestBody.EndpointIDs) == 0 {
			return endpointModelsBatchResponse{Items: []endpointModelsBatchItem{}}, nil
		}
		rows, err := listEndpointModelRows(r.Context(), tx, profile.ID, requestBody.EndpointIDs)
		if err != nil {
			return endpointModelsBatchResponse{}, err
		}
		byEndpointRecords, byEndpointCounts, allRecords := collectBatchEndpointModels(rows)
		strategies, accessTargets, health, err := loadModelRelations(r.Context(), tx, profile.ID, allRecords)
		if err != nil {
			return endpointModelsBatchResponse{}, err
		}
		items := make([]endpointModelsBatchItem, 0, len(requestBody.EndpointIDs))
		summaries := map[int]modelrouting.RoutingSummary{}
		if err := attachRoutingSummaries(allRecords, accessTargets, strategies, summaries); err != nil {
			return endpointModelsBatchResponse{}, err
		}
		for _, endpointID := range requestBody.EndpointIDs {
			records := byEndpointRecords[endpointID]
			sort.Slice(records, func(left int, right int) bool {
				return records[left].ModelID < records[right].ModelID
			})
			models := make([]modelConfigListResponse, 0, len(records))
			for _, record := range records {
				item := buildModelListResponse(record, strategies, accessTargets, byEndpointCounts[endpointID], health, s.now().UTC())
				if summary, ok := summaries[record.ID]; ok {
					summary := summary
					item.RoutingSummary = &summary
				}
				models = append(models, item)
			}
			items = append(items, endpointModelsBatchItem{EndpointID: endpointID, Models: models})
		}
		return endpointModelsBatchResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func collectEndpointModelCounts(rows []endpointModelConnectionRow) ([]modelRecord, map[int]modelConnectionCounts) {
	recordsByID := map[int]modelRecord{}
	counts := map[int]modelConnectionCounts{}
	seenTerminalConnections := map[int]map[int]struct{}{}
	for _, row := range rows {
		recordsByID[row.ReachableModelID] = row.ReachableModelData
		if _, ok := seenTerminalConnections[row.ReachableModelID]; !ok {
			seenTerminalConnections[row.ReachableModelID] = map[int]struct{}{}
		}
		if _, seen := seenTerminalConnections[row.ReachableModelID][row.TerminalConnectionID]; seen {
			continue
		}
		seenTerminalConnections[row.ReachableModelID][row.TerminalConnectionID] = struct{}{}
		count := counts[row.ReachableModelID]
		count.Total++
		if row.ConnectionIsActive {
			count.Active++
		}
		counts[row.ReachableModelID] = count
	}
	records := make([]modelRecord, 0, len(recordsByID))
	for _, record := range recordsByID {
		records = append(records, record)
	}
	return records, counts
}

func collectBatchEndpointModels(rows []endpointModelConnectionRow) (map[int][]modelRecord, map[int]map[int]modelConnectionCounts, []modelRecord) {
	byEndpointRecords := map[int][]modelRecord{}
	byEndpointCounts := map[int]map[int]modelConnectionCounts{}
	allRecordsByID := map[int]modelRecord{}
	seenByEndpoint := map[int]map[int]struct{}{}
	seenTerminalConnections := map[int]map[int]map[int]struct{}{}
	for _, row := range rows {
		allRecordsByID[row.ReachableModelID] = row.ReachableModelData
		if _, ok := byEndpointCounts[row.EndpointID]; !ok {
			byEndpointCounts[row.EndpointID] = map[int]modelConnectionCounts{}
		}
		if _, ok := seenTerminalConnections[row.EndpointID]; !ok {
			seenTerminalConnections[row.EndpointID] = map[int]map[int]struct{}{}
		}
		if _, ok := seenTerminalConnections[row.EndpointID][row.ReachableModelID]; !ok {
			seenTerminalConnections[row.EndpointID][row.ReachableModelID] = map[int]struct{}{}
		}
		if _, seen := seenTerminalConnections[row.EndpointID][row.ReachableModelID][row.TerminalConnectionID]; !seen {
			seenTerminalConnections[row.EndpointID][row.ReachableModelID][row.TerminalConnectionID] = struct{}{}
			count := byEndpointCounts[row.EndpointID][row.ReachableModelID]
			count.Total++
			if row.ConnectionIsActive {
				count.Active++
			}
			byEndpointCounts[row.EndpointID][row.ReachableModelID] = count
		}
		if _, ok := seenByEndpoint[row.EndpointID]; !ok {
			seenByEndpoint[row.EndpointID] = map[int]struct{}{}
		}
		if _, seen := seenByEndpoint[row.EndpointID][row.ReachableModelID]; !seen {
			byEndpointRecords[row.EndpointID] = append(byEndpointRecords[row.EndpointID], row.ReachableModelData)
			seenByEndpoint[row.EndpointID][row.ReachableModelID] = struct{}{}
		}
	}
	allRecords := make([]modelRecord, 0, len(allRecordsByID))
	for _, record := range allRecordsByID {
		allRecords = append(allRecords, record)
	}
	sortModelRecordsByID(allRecords)
	return byEndpointRecords, byEndpointCounts, allRecords
}
