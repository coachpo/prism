package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
)

const maxBatchItems = 20

type batchItemInput struct {
	ID           int    `json:"id"`
	ContextLimit *int64 `json:"context_limit,omitempty"`
	OutputLimit  *int64 `json:"output_limit,omitempty"`
}
type batchRequest struct {
	Items                  []batchItemInput `json:"items"`
	ReferenceID            int              `json:"reference_id,omitempty"`
	Client                 string           `json:"client,omitempty"`
	ConfirmManualOverrides bool             `json:"confirm_manual_overrides"`
	PreviewToken           string           `json:"preview_token,omitempty"`
}
type batchItemResult struct {
	ID             int            `json:"id"`
	Label          string         `json:"label"`
	Before         map[string]any `json:"before"`
	After          map[string]any `json:"after"`
	Error          string         `json:"error,omitempty"`
	ManualOverride bool           `json:"manual_override"`
}
type batchResponse struct {
	Action       string            `json:"action"`
	PreviewToken string            `json:"preview_token"`
	CanApply     bool              `json:"can_apply"`
	Items        []batchItemResult `json:"items"`
	Applied      bool              `json:"applied"`
}

func validateBatchRequest(action string, input *batchRequest) error {
	bad := func(detail string) error {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: detail}
	}
	if len(input.Items) == 0 || len(input.Items) > maxBatchItems {
		return bad("batch requires 1 to 20 explicitly selected items")
	}
	if action != "model_limits" && action != "model_strategy" && action != "target_pricing" {
		return bad("unsupported batch action")
	}
	if action == "model_limits" {
		if input.Client != "" && input.Client != "opencode" {
			return bad("limit overrides use the independent models.dev/OpenCode binding")
		}
		if input.ReferenceID != 0 {
			return bad("limit overrides cannot assign references")
		}
	} else if input.ReferenceID <= 0 || input.Client != "" {
		return bad("assignment requires a positive reference_id and no client")
	}
	seen := map[int]bool{}
	for _, item := range input.Items {
		if item.ID <= 0 || seen[item.ID] {
			return bad("item ids must be positive and unique")
		}
		seen[item.ID] = true
		if action != "model_limits" && (item.ContextLimit != nil || item.OutputLimit != nil) {
			return bad("one batch cannot mix actions")
		}
	}
	sort.Slice(input.Items, func(i, j int) bool { return input.Items[i].ID < input.Items[j].ID })
	return nil
}

func batchToken(action string, input batchRequest, items []batchItemResult) string {
	input.PreviewToken = ""
	// Confirmation authorizes replacing a displayed manual value, but is not
	// an authoring fact and may be checked after the operator reads the diff.
	input.ConfirmManualOverrides = false
	data, _ := json.Marshal(struct {
		Action string
		Input  batchRequest
		Items  []batchItemResult
	}{action, input, items})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
func limitInputError(input batchItemInput) string {
	if input.ContextLimit == nil || input.OutputLimit == nil {
		return "context_limit and output_limit require reliable explicit values"
	}
	if *input.ContextLimit <= 0 || *input.OutputLimit <= 0 || *input.ContextLimit > 9007199254740991 || *input.OutputLimit > *input.ContextLimit {
		return "limits must be positive safe integers with output_limit <= context_limit"
	}
	return ""
}
func batchItemError(id int, err error) batchItemResult {
	return batchItemResult{ID: id, Label: fmt.Sprintf("#%d", id), Before: map[string]any{}, After: map[string]any{}, Error: err.Error()}
}
