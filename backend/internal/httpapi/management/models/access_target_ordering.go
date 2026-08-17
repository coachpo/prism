package models

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

type accessTargetMutationItem struct {
	ID      int
	Request modelAccessTargetRequest
}

func accessTargetRequestFromCreate(input modelAccessTargetCreateRequest, existingCount int) (modelAccessTargetRequest, error) {
	position := existingCount
	if input.Position != nil {
		position = *input.Position
	}
	if position < 0 || position > existingCount {
		return modelAccessTargetRequest{}, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("position must be between 0 and %d", existingCount)}
	}
	request := modelAccessTargetRequest{TargetType: modelrouting.NormalizeTargetType(input.TargetType), TargetModelID: normalizeOptionalString(input.TargetModelID, false, false), ConnectionID: copyIntPtr(input.ConnectionID), Position: position, IsEnabled: input.IsEnabled}
	if err := validatePublicAccessTarget(request); err != nil {
		return modelAccessTargetRequest{}, err
	}
	return request, nil
}

func accessTargetRequestFromRecord(record accessTargetRecord) modelAccessTargetRequest {
	enabled := record.IsEnabled
	request := modelAccessTargetRequest{TargetType: record.TargetType, Position: record.Position, IsEnabled: &enabled}
	if modelrouting.IsModelTargetType(record.TargetType) && record.TargetModel != nil {
		request.TargetModelID = stringPtr(record.TargetModel.ModelID)
	}
	if modelrouting.IsTerminalTargetType(record.TargetType) {
		request.ConnectionID = copyIntPtr(record.TargetConnectionID)
	}
	return request
}

func insertAccessTargetMutationItem(items []accessTargetMutationItem, request modelAccessTargetRequest) ([]accessTargetMutationItem, error) {
	position := request.Position
	if position < 0 || position > len(items) {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("position must be between 0 and %d", len(items))}
	}
	normalizeMutationItemPositions(items)
	for index := range items {
		if items[index].Request.Position >= position {
			items[index].Request.Position++
		}
	}
	items = append(items, accessTargetMutationItem{Request: request})
	normalizeMutationItemPositions(items)
	return items, nil
}

func updateAccessTargetMutationItem(items []accessTargetMutationItem, targetID int, input modelAccessTargetUpdateRequest) ([]accessTargetMutationItem, error) {
	index := findAccessTargetMutationIndex(items, targetID)
	if index == -1 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	item := items[index]
	if modelrouting.IsTerminalTargetType(item.Request.TargetType) {
		return updateConnectionAccessTargetMutationItem(items, targetID, index, input)
	}
	updated := item.Request
	if input.TargetType.Set {
		if input.TargetType.Value != nil && modelrouting.IsTerminalTargetType(*input.TargetType.Value) {
			return nil, connectionAccessTargetsManagedError()
		}
		if input.TargetType.Value == nil {
			updated.TargetType = ""
		} else {
			updated.TargetType = modelrouting.NormalizeTargetType(*input.TargetType.Value)
		}
		if modelrouting.IsModelTargetType(updated.TargetType) {
			updated.ConnectionID = nil
		}
		if modelrouting.IsTerminalTargetType(updated.TargetType) {
			updated.TargetModelID = nil
		}
	}
	if input.TargetModelID.Set {
		updated.TargetModelID = normalizeOptionalString(input.TargetModelID.Value, false, false)
		if !input.TargetType.Set && updated.TargetModelID != nil && strings.TrimSpace(*updated.TargetModelID) != "" {
			updated.TargetType = modelrouting.TargetTypeModel
			updated.ConnectionID = nil
		}
	}
	if input.ConnectionID.Set || input.TargetConnectionID.Set {
		return nil, connectionAccessTargetsManagedError()
	}
	if input.IsEnabled.Set {
		updated.IsEnabled = &input.IsEnabled.Value
	}
	item.Request = updated
	items[index] = item
	if input.Position.Set {
		if input.Position.Value == nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "position is required"}
		}
		return moveAccessTargetMutationItem(items, targetID, *input.Position.Value)
	}
	normalizeMutationItemPositions(items)
	return items, nil
}

func moveAccessTargetMutationItem(items []accessTargetMutationItem, targetID int, toIndex int) ([]accessTargetMutationItem, error) {
	if len(items) == 0 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	if toIndex < 0 || toIndex >= len(items) {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("to_index must be between 0 and %d", len(items)-1)}
	}
	normalizeMutationItemPositions(items)
	fromIndex := findAccessTargetMutationIndex(items, targetID)
	if fromIndex == -1 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	moved := items[fromIndex]
	items = append(items[:fromIndex], items[fromIndex+1:]...)
	items = append(items[:toIndex], append([]accessTargetMutationItem{moved}, items[toIndex:]...)...)
	assignMutationItemPositions(items)
	return items, nil
}

func deleteAccessTargetMutationItem(items []accessTargetMutationItem, targetID int) ([]accessTargetMutationItem, error) {
	index := findAccessTargetMutationIndex(items, targetID)
	if index == -1 {
		return nil, &domainError{StatusCode: http.StatusNotFound, Detail: "Model access target not found"}
	}
	if modelrouting.IsTerminalTargetType(items[index].Request.TargetType) {
		return nil, connectionAccessTargetsManagedError()
	}
	items = append(items[:index], items[index+1:]...)
	assignMutationItemPositions(items)
	return items, nil
}

func accessTargetRequestsFromMutationItems(items []accessTargetMutationItem) []modelAccessTargetRequest {
	normalizeMutationItemPositions(items)
	requests := make([]modelAccessTargetRequest, 0, len(items))
	for _, item := range items {
		requests = append(requests, item.Request)
	}
	return requests
}

func updateConnectionAccessTargetMutationItem(items []accessTargetMutationItem, targetID int, index int, input modelAccessTargetUpdateRequest) ([]accessTargetMutationItem, error) {
	if input.TargetType.Set || input.TargetModelID.Set || input.ConnectionID.Set || input.TargetConnectionID.Set {
		return nil, connectionAccessTargetsManagedError()
	}
	if input.IsEnabled.Set {
		items[index].Request.IsEnabled = &input.IsEnabled.Value
	}
	if input.Position.Set {
		if input.Position.Value == nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "position is required"}
		}
		return moveAccessTargetMutationItem(items, targetID, *input.Position.Value)
	}
	normalizeMutationItemPositions(items)
	return items, nil
}

func isAccessTargetMetadataOnlyUpdate(input modelAccessTargetUpdateRequest) bool {
	return !input.TargetType.Set && !input.TargetModelID.Set && !input.ConnectionID.Set && !input.TargetConnectionID.Set
}

func hasEnabledAccessTargetMutationItem(items []accessTargetMutationItem) bool {
	for _, item := range items {
		if item.Request.IsEnabled == nil || *item.Request.IsEnabled {
			return true
		}
	}
	return false
}

func modelAccessTargetRequestsOnly(values []modelAccessTargetRequest) []modelAccessTargetRequest {
	items := make([]modelAccessTargetRequest, 0, len(values))
	for _, value := range values {
		if modelrouting.IsModelTargetType(value.TargetType) {
			items = append(items, value)
		}
	}
	return items
}

func normalizeMutationItemPositions(items []accessTargetMutationItem) {
	sort.SliceStable(items, func(left int, right int) bool {
		if items[left].Request.Position == items[right].Request.Position {
			return items[left].ID < items[right].ID
		}
		return items[left].Request.Position < items[right].Request.Position
	})
	assignMutationItemPositions(items)
}

func assignMutationItemPositions(items []accessTargetMutationItem) {
	for index := range items {
		items[index].Request.Position = index
	}
}

func findAccessTargetMutationIndex(items []accessTargetMutationItem, targetID int) int {
	for index, item := range items {
		if item.ID == targetID {
			return index
		}
	}
	return -1
}

func normalizeOptionalString(value *string, lower bool, emptyToNil bool) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if lower {
		trimmed = strings.ToLower(trimmed)
	}
	if emptyToNil && trimmed == "" {
		return nil
	}
	resolved := trimmed
	return &resolved
}

func validatePublicAccessTarget(accessTarget modelAccessTargetRequest) error {
	if modelrouting.IsTerminalTargetType(accessTarget.TargetType) || accessTarget.ConnectionID != nil {
		return connectionAccessTargetsManagedError()
	}
	return nil
}

func connectionAccessTargetsManagedError() error {
	return &domainError{StatusCode: http.StatusBadRequest, Detail: "terminal targets are managed through model-scoped connection routes"}
}

func stringPtr(value string) *string {
	resolved := value
	return &resolved
}
