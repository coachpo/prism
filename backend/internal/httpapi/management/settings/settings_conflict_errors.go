package settings

import (
	"net/http"
	"strings"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type settingsConflictError struct {
	code               string
	currentRevision    string
	currentGenerations map[string]string
	operationID        string
}

func (err *settingsConflictError) Error() string { return err.code }

func isJobConflict(err error) bool {
	return strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "retention_job_conflict")
}

func writeSettingsError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var conflict *settingsConflictError
	if asConflict(err, &conflict) {
		switch conflict.code {
		case "retention_settings_changed":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "retention settings changed concurrently", Params: map[string]any{}, Details: map[string]any{"current_revision": conflict.currentRevision, "recovery": "refresh"}}, http.StatusConflict)
		case "retention_preflight_required":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "fresh destructive preflight is required", Params: map[string]any{}, Details: map[string]any{"recovery": "repreview"}}, http.StatusPreconditionRequired)
		case "retention_preflight_stale":
			currentRevision := any(nil)
			if conflict.currentRevision != "" {
				currentRevision = conflict.currentRevision
			}
			generations := conflict.currentGenerations
			if generations == nil {
				generations = map[string]string{}
			}
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "preflight is stale; repreview", Params: map[string]any{}, Details: map[string]any{"recovery": "repreview", "current_revision": currentRevision, "current_generations": generations}}, http.StatusConflict)
		case "operation_id_conflict":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "operation id already used with a different request", Params: map[string]any{}, Details: map[string]any{"operation_id": conflict.operationID, "recovery": "inspect_operation"}}, http.StatusConflict)
		case "operation_outcome_unavailable":
			// A reserved operation without its durable outcome is an internal
			// recovery fault, never a client-visible conflict or a reason to
			// synthesize success.
			writeSettingsInternalError(w, r, corsSnapshot, err)
		case "retention_owner_drift_changed":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "owner drift inventory changed", Params: map[string]any{}, Details: map[string]any{"recovery": "repreview"}}, http.StatusConflict)
		case "retention_job_conflict":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "dataset already has an executing or reserved retention job", Params: map[string]any{}, Details: map[string]any{"recovery": "inspect_operation"}}, http.StatusConflict)
		case "retention_owner_unavailable":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "retention owner snapshot is temporarily unavailable", Params: map[string]any{}, Details: map[string]any{"recovery": "retry", "retry_after_seconds": 5}}, http.StatusServiceUnavailable)
		case "invalid_retention_cutoff":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "invalid retention cutoff", Params: map[string]any{}, Details: map[string]any{"violations": []FieldViolation{{Path: "selection.cutoff", Reason: "must_be_utc_and_not_in_the_future"}}}}, http.StatusUnprocessableEntity)
		case "validation_failed":
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: "invalid preflight request", Params: map[string]any{}, Details: map[string]any{"violations": []any{}}}, http.StatusUnprocessableEntity)
		default:
			writeProblem(w, r, corsSnapshot, SettingsProblem{Code: conflict.code, Detail: err.Error(), Params: map[string]any{}}, http.StatusConflict)
		}
		return
	}
	writeSettingsInternalError(w, r, corsSnapshot, err)
}

func asConflict(err error, target **settingsConflictError) bool {
	for err != nil {
		if conflict, ok := err.(*settingsConflictError); ok {
			*target = conflict
			return true
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			err = wrapped.Unwrap()
			continue
		}
		break
	}
	return false
}
