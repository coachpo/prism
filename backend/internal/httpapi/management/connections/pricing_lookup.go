package connections

import (
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (s *Service) handleListPricingTemplateConnections(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
		return
	}
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (pricingTemplateConnectionsResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateConnectionsResponse{}, err
		}
		if _, err := validatePricingTemplateID(r.Context(), tx, profile.ID, &templateID); err != nil {
			return pricingTemplateConnectionsResponse{}, err
		}

		rows, err := listPricingTemplateConnectionUsageRows(r.Context(), tx, profile.ID, templateID)
		if err != nil {
			return pricingTemplateConnectionsResponse{}, err
		}
		items := make([]pricingTemplateConnectionUsageItem, 0, len(rows))
		for _, row := range rows {
			modelID := ""
			if row.ModelID != nil {
				modelID = *row.ModelID
			}
			endpointName := ""
			if row.EndpointName != nil {
				endpointName = *row.EndpointName
			}
			items = append(items, pricingTemplateConnectionUsageItem{
				ConnectionID:   row.ConnectionID,
				ConnectionName: row.ConnectionName,
				ModelConfigID:  row.ModelConfigID,
				ModelID:        modelID,
				EndpointID:     row.EndpointID,
				EndpointName:   endpointName,
			})
		}
		return pricingTemplateConnectionsResponse{TemplateID: templateID, Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
