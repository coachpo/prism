package endpoints

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// EndpointDirectReference describes one direct Terminal Target ownership row
// referencing an endpoint. It is used by both the references batch read and
// the typed endpoint_in_use delete blocker. It never carries endpoint API
// keys, header values or request-parameter values.
type EndpointDirectReference struct {
	ConnectionID          int                       `json:"connection_id"`
	AccessTargetID        *int                      `json:"access_target_id"`
	TerminalTargetName    *string                   `json:"terminal_target_name"`
	ModelConfigID         int                       `json:"model_config_id"`
	ModelID               string                    `json:"model_id"`
	ModelDisplayName      *string                   `json:"model_display_name"`
	APIFamily             string                    `json:"api_family"`
	AuthoredStagePosition int                       `json:"authored_stage_position"`
	IsEnabled             bool                      `json:"is_enabled"`
	IsActive              bool                      `json:"is_active"`
	OpenAITextCapability  *string                   `json:"openai_text_capability"`
	PricingTemplate       *referencePricingTemplate `json:"pricing_template"`
	CustomHeaderCount     int                       `json:"custom_header_count"`
}

type referencePricingTemplate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type endpointReferencesBatchRequest struct {
	EndpointIDs []int `json:"endpoint_ids"`
}

type endpointReferencesBatchResponse struct {
	Items []endpointReferenceItem `json:"items"`
}

type endpointReferenceItem struct {
	EndpointID int                       `json:"endpoint_id"`
	References []EndpointDirectReference `json:"references"`
}

// listEndpointDirectReferences returns direct Terminal Target ownership rows
// for the given endpoints in one bounded query. Missing/cross-profile endpoint
// ids yield no rows; callers must verify every requested id separately.
func listEndpointDirectReferences(ctx context.Context, tx pgx.Tx, profileID int, endpointIDs []int) (map[int][]EndpointDirectReference, error) {
	if len(endpointIDs) == 0 {
		return map[int][]EndpointDirectReference{}, nil
	}
	rows, err := tx.Query(ctx, `WITH ranked AS (
		SELECT connections.endpoint_id, connections.id AS connection_id, model_access_targets.id AS access_target_id, connections.name,
			model_configs.id AS model_config_id, model_configs.model_id, model_configs.display_name, model_configs.api_family,
			model_access_targets.position,
			(ROW_NUMBER() OVER (PARTITION BY model_access_targets.source_model_config_id ORDER BY model_access_targets.position ASC, model_access_targets.id ASC) - 1)::integer AS authored_stage_position,
			model_access_targets.is_enabled, connections.is_active, connections.openai_text_capability,
			pricing_templates.id AS pricing_template_id, pricing_templates.name AS pricing_name, connections.custom_headers
		FROM connections
		JOIN model_access_targets ON model_access_targets.profile_id = connections.profile_id AND model_access_targets.target_type = 'connection' AND model_access_targets.target_connection_id = connections.id
		JOIN model_configs ON model_configs.id = model_access_targets.source_model_config_id AND model_configs.profile_id = connections.profile_id
		LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id
		WHERE connections.profile_id = $1
	)
	SELECT endpoint_id, connection_id, access_target_id, name, model_config_id, model_id, display_name, api_family,
		authored_stage_position, is_enabled, is_active, openai_text_capability,
		pricing_template_id, pricing_name, custom_headers
	FROM ranked
	WHERE endpoint_id = ANY($2)
	ORDER BY model_config_id ASC, position ASC, access_target_id ASC`,
		profileID, int32ArrayForEndpoints(endpointIDs))
	if err != nil {
		return nil, fmt.Errorf("query endpoint direct references for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := map[int][]EndpointDirectReference{}
	for rows.Next() {
		var reference EndpointDirectReference
		var connectionID int
		var accessTargetID int
		var endpointID int
		var terminalTargetName, modelDisplayName, openAITextCapability, pricingName sql.NullString
		var pricingTemplateID sql.NullInt32
		var customHeadersRaw sql.NullString
		if err := rows.Scan(&endpointID, &connectionID, &accessTargetID, &terminalTargetName, &reference.ModelConfigID, &reference.ModelID, &modelDisplayName, &reference.APIFamily, &reference.AuthoredStagePosition, &reference.IsEnabled, &reference.IsActive, &openAITextCapability, &pricingTemplateID, &pricingName, &customHeadersRaw); err != nil {
			return nil, fmt.Errorf("scan endpoint direct reference for profile %d: %w", profileID, err)
		}
		reference.ConnectionID = connectionID
		reference.AccessTargetID = intPtr(accessTargetID)
		reference.TerminalTargetName = nullableStringValue(terminalTargetName)
		reference.ModelDisplayName = nullableStringValue(modelDisplayName)
		reference.OpenAITextCapability = nullableStringValue(openAITextCapability)
		if pricingTemplateID.Valid {
			reference.PricingTemplate = &referencePricingTemplate{ID: int(pricingTemplateID.Int32), Name: strings.TrimSpace(pricingName.String)}
		}
		reference.CustomHeaderCount = customHeaderCount(customHeadersRaw)
		items[endpointID] = append(items[endpointID], reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint direct references for profile %d: %w", profileID, err)
	}
	return items, nil
}

func (s *Service) handleEndpointReferencesBatch(w http.ResponseWriter, r *http.Request) {
	var requestBody endpointReferencesBatchRequest
	if err := decodeStrictJSON(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if len(requestBody.EndpointIDs) == 0 || len(requestBody.EndpointIDs) > 100 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "endpoint_ids must contain between 1 and 100 endpoint ids")
		return
	}
	seen := map[int]struct{}{}
	for _, endpointID := range requestBody.EndpointIDs {
		if endpointID <= 0 {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "endpoint_ids must be positive integers")
			return
		}
		if _, exists := seen[endpointID]; exists {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "endpoint_ids must be unique")
			return
		}
		seen[endpointID] = struct{}{}
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint", func(tx pgx.Tx) (endpointReferencesBatchResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return endpointReferencesBatchResponse{}, err
		}
		referencesByEndpoint, err := listEndpointDirectReferences(r.Context(), tx, profile.ID, requestBody.EndpointIDs)
		if err != nil {
			return endpointReferencesBatchResponse{}, err
		}
		missingIDs := make([]int, 0, len(requestBody.EndpointIDs))
		for _, endpointID := range requestBody.EndpointIDs {
			if _, exists := referencesByEndpoint[endpointID]; exists {
				continue
			}
			found, err := endpointExistsInProfile(r.Context(), tx, profile.ID, endpointID)
			if err != nil {
				return endpointReferencesBatchResponse{}, err
			}
			if !found {
				missingIDs = append(missingIDs, endpointID)
			}
		}
		if len(missingIDs) > 0 {
			return endpointReferencesBatchResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: map[string]any{
				"message":     "Endpoint not found",
				"endpoint_id": missingIDs,
			}}
		}
		items := make([]endpointReferenceItem, 0, len(requestBody.EndpointIDs))
		for _, endpointID := range requestBody.EndpointIDs {
			references := referencesByEndpoint[endpointID]
			if len(references) == 0 {
				references = []EndpointDirectReference{}
			}
			items = append(items, endpointReferenceItem{EndpointID: endpointID, References: references})
		}
		return endpointReferencesBatchResponse{Items: items}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func endpointExistsInProfile(ctx context.Context, tx pgx.Tx, profileID int, endpointID int) (bool, error) {
	var existingID int
	err := tx.QueryRow(ctx, `SELECT id FROM endpoints WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, endpointID).Scan(&existingID)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load endpoint %d for profile %d: %w", endpointID, profileID, err)
	}
	return true, nil
}

func customHeaderCount(raw sql.NullString) int {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return 0
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil {
		return 0
	}
	return len(values)
}

func decodeStrictJSON(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func int32ArrayForEndpoints(endpointIDs []int) []int32 {
	values := make([]int32, 0, len(endpointIDs))
	for _, endpointID := range endpointIDs {
		values = append(values, int32(endpointID))
	}
	return values
}

func dereferenceInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
