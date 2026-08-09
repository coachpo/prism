package connections

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

type terminalTargetCopyRequest struct {
	DestinationModelConfigIDs []int `json:"destination_model_config_ids"`
	EnableCopies              bool  `json:"enable_copies"`
}

type terminalTargetCopyResponse struct {
	SourceConnectionID    int                                 `json:"source_connection_id"`
	Items                 []terminalTargetCopyItem            `json:"items"`
	ConfigurationWarnings []modelrouting.ConfigurationWarning `json:"configuration_warnings"`
}

type terminalTargetCopyItem struct {
	ModelConfigID     int                            `json:"model_config_id"`
	ConnectionSummary redactedConnectionSummary      `json:"connection_summary"`
	AccessTarget      connectionMutationAccessTarget `json:"access_target"`
}

// redactedConnectionSummary is the copy-response connection shape: it carries
// counts, never header/parameter values or endpoint credentials.
type redactedConnectionSummary struct {
	ID                          int                      `json:"id"`
	Name                        *string                  `json:"name"`
	EndpointID                  int                      `json:"endpoint_id"`
	IsActive                    bool                     `json:"is_active"`
	OpenAITextCapability        *string                  `json:"openai_text_capability"`
	PricingTemplate             *redactedPricingTemplate `json:"pricing_template"`
	QPSLimit                    *int                     `json:"qps_limit"`
	MaxInFlightNonStream        *int                     `json:"max_in_flight_non_stream"`
	MaxInFlightStream           *int                     `json:"max_in_flight_stream"`
	CustomHeaderCount           int                      `json:"custom_header_count"`
	CustomRequestParameterCount int                      `json:"custom_request_parameter_count"`
}

type redactedPricingTemplate struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func redactConnectionSummary(item connectionResponse) redactedConnectionSummary {
	summary := redactedConnectionSummary{
		ID:                          item.ID,
		Name:                        item.Name,
		EndpointID:                  item.EndpointID,
		IsActive:                    item.IsActive,
		OpenAITextCapability:        item.OpenAITextCapability,
		QPSLimit:                    item.QPSLimit,
		MaxInFlightNonStream:        item.MaxInFlightNonStream,
		MaxInFlightStream:           item.MaxInFlightStream,
		CustomHeaderCount:           len(item.CustomHeaders),
		CustomRequestParameterCount: item.CustomRequestParameters.TopLevelKeyCount(),
	}
	if item.PricingTemplate != nil {
		summary.PricingTemplate = &redactedPricingTemplate{ID: item.PricingTemplate.ID, Name: item.PricingTemplate.Name}
	}
	return summary
}

func (s *Service) handleCreateConnectionCopies(w http.ResponseWriter, r *http.Request) {
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
	var requestBody terminalTargetCopyRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	destinations := dedupeCopyDestinationIDs(requestBody.DestinationModelConfigIDs)
	if len(requestBody.DestinationModelConfigIDs) == 0 || len(destinations) != len(requestBody.DestinationModelConfigIDs) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "invalid_terminal_target_copy_destinations: destinations must be unique positive integers")
		return
	}
	if len(destinations) > 100 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, "invalid_terminal_target_copy_destinations: at most 100 destinations")
		return
	}
	for _, destinationID := range destinations {
		if destinationID <= 0 {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "invalid_terminal_target_copy_destinations: destinations must be unique positive integers")
			return
		}
	}

	response, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (terminalTargetCopyResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return terminalTargetCopyResponse{}, err
		}
		sourceOwner, found, err := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if err != nil {
			return terminalTargetCopyResponse{}, err
		}
		if !found {
			return terminalTargetCopyResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		for _, destinationID := range destinations {
			if destinationID == modelConfigID {
				return terminalTargetCopyResponse{}, &DomainError{StatusCode: http.StatusBadRequest, Detail: "invalid_terminal_target_copy_destinations: source owner cannot be a destination"}
			}
		}
		allModels, err := loadAndLockCopyModels(r.Context(), tx, profile.ID, sourceOwner.ID, destinations)
		if err != nil {
			return terminalTargetCopyResponse{}, err
		}
		sourceOwner = allModels[sourceOwner.ID]
		if err := lockCopyAccessTargetRows(r.Context(), tx, profile.ID, append([]int{sourceOwner.ID}, destinations...)); err != nil {
			return terminalTargetCopyResponse{}, err
		}
		source, found, err := loadConnectionRecord(r.Context(), tx, profile.ID, connectionID, true)
		if err != nil {
			return terminalTargetCopyResponse{}, err
		}
		if !found {
			return terminalTargetCopyResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "terminal_target_not_found"}
		}
		if _, found, err := loadConnectionOwnerReference(r.Context(), tx, profile.ID, sourceOwner.ID, source.ID, true); err != nil {
			return terminalTargetCopyResponse{}, err
		} else if !found {
			return terminalTargetCopyResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "terminal_target_not_found"}
		}
		destinationModels := make(map[int]modelRecord, len(destinations))
		for _, destinationID := range destinations {
			destinationModels[destinationID] = allModels[destinationID]
		}

		now := s.nowUTC()
		items := make([]terminalTargetCopyItem, 0, len(destinations))
		warnings := make([]modelrouting.ConfigurationWarning, 0, len(destinations))
		for _, destinationID := range destinations {
			destination := destinationModels[destinationID]
			position, err := nextModelAccessTargetPosition(r.Context(), tx, profile.ID, destinationID)
			if err != nil {
				return terminalTargetCopyResponse{}, err
			}
			copied := connectionResponse{
				ProfileID:               profile.ID,
				APIFamily:               destination.APIFamily,
				EndpointID:              source.EndpointID,
				IsActive:                source.IsActive,
				Priority:                position,
				Name:                    cloneString(source.Name),
				AuthType:                cloneString(source.AuthType),
				CustomHeaders:           cloneHeaderMap(source.CustomHeaders),
				CustomRequestParameters: source.CustomRequestParameters.Clone(),
				OpenAITextCapability:    cloneString(source.OpenAITextCapability),
				PricingTemplateID:       cloneInt(source.PricingTemplateID),
				QPSLimit:                cloneInt(source.QPSLimit),
				MaxInFlightNonStream:    cloneInt(source.MaxInFlightNonStream),
				MaxInFlightStream:       cloneInt(source.MaxInFlightStream),
				CreatedAt:               now,
				UpdatedAt:               now,
			}
			copiedConnectionID, err := insertTerminalTarget(r.Context(), tx, terminalTargetRecordFromConnectionResponse(copied))
			if err != nil {
				return terminalTargetCopyResponse{}, err
			}
			accessTargetID, err := insertOwnerTerminalTargetAccessWithEnabledReturningID(r.Context(), tx, profile.ID, destinationID, copiedConnectionID, position, requestBody.EnableCopies, now)
			if err != nil {
				return terminalTargetCopyResponse{}, err
			}
			loaded, found, err := loadModelConnectionRecord(r.Context(), tx, profile.ID, destinationID, copiedConnectionID)
			if err != nil {
				return terminalTargetCopyResponse{}, err
			}
			if !found {
				return terminalTargetCopyResponse{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Connection not found"}
			}
			item := terminalTargetCopyItem{
				ModelConfigID:     destinationID,
				ConnectionSummary: redactConnectionSummary(loaded),
				AccessTarget: connectionMutationAccessTarget{
					ID:               accessTargetID,
					TargetType:       "connection",
					ConnectionID:     intPointer(copiedConnectionID),
					TerminalTargetID: intPointer(copiedConnectionID),
					Position:         position,
					IsEnabled:        requestBody.EnableCopies,
				},
			}
			items = append(items, item)
			if providerauth.IsOpenAI(destination.APIFamily) && destination.OpenAIAcceptedFormat != nil && source.OpenAITextCapability != nil {
				ownerWarnings := modelrouting.GenerateOpenAIWarningsForTarget(*destination.OpenAIAcceptedFormat, *source.OpenAITextCapability, "terminal_target.openai_text_capability", destinationID, accessTargetID, copiedConnectionID)
				warnings = append(warnings, ownerWarnings...)
			}
		}
		return terminalTargetCopyResponse{SourceConnectionID: connectionID, Items: items, ConfigurationWarnings: warnings}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusCreated, response)
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func intPointer(value int) *int {
	return &value
}

func cloneHeaderMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func dedupeCopyDestinationIDs(values []int) []int {
	seen := map[int]struct{}{}
	unique := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

// loadAndLockCopyModels locks the source owner and destination model rows in
// one deterministic id order, preventing reversed copy batches from deadlocking
// while still validating profile and API-family ownership under the lock.
func loadAndLockCopyModels(ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int, destinations []int) (map[int]modelRecord, error) {
	modelIDs := append([]int{sourceModelConfigID}, destinations...)
	sort.Ints(modelIDs)
	models := map[int]modelRecord{}
	for _, modelConfigID := range modelIDs {
		record, found, err := loadModelRecord(ctx, tx, profileID, modelConfigID, true)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &DomainError{StatusCode: http.StatusNotFound, Detail: "terminal_target_copy_destination_not_found"}
		}
		models[modelConfigID] = record
	}
	sourceOwner := models[sourceModelConfigID]
	for _, destinationID := range destinations {
		if !modelrouting.SameAPIFamily(models[destinationID].APIFamily, sourceOwner.APIFamily) {
			return nil, &DomainError{StatusCode: http.StatusConflict, Detail: "terminal_target_copy_api_family_conflict"}
		}
	}
	return models, nil
}

// lockCopyAccessTargetRows locks source and destination access-target rows in
// model id order so concurrent reorder/connection-create cannot interleave with
// the copy's tail-position allocation.
func lockCopyAccessTargetRows(ctx context.Context, tx pgx.Tx, profileID int, modelConfigIDs []int) error {
	sortedModelConfigIDs := append([]int(nil), modelConfigIDs...)
	sort.Ints(sortedModelConfigIDs)
	for _, modelConfigID := range sortedModelConfigIDs {
		if err := lockModelAccessTargetRows(ctx, tx, profileID, modelConfigID); err != nil {
			return fmt.Errorf("lock copy access targets for model %d: %w", modelConfigID, err)
		}
	}
	return nil
}
