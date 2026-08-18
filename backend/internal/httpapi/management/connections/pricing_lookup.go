package connections

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleListPricingTemplateConnections(w http.ResponseWriter, r *http.Request) {
	templateID, err := routeInt(r, "template_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateConnectionsResponse, error) {
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
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

type pricingTemplateConnectionUsageItem struct {
	ConnectionID   int     `json:"connection_id"`
	ConnectionName *string `json:"connection_name"`
	ModelConfigID  int     `json:"model_config_id"`
	ModelID        string  `json:"model_id"`
	EndpointID     int     `json:"endpoint_id"`
	EndpointName   string  `json:"endpoint_name"`
}

type pricingTemplateConnectionsResponse struct {
	TemplateID int                                  `json:"template_id"`
	Items      []pricingTemplateConnectionUsageItem `json:"items"`
}
