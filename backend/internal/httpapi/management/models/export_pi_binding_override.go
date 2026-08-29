package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// piOverrideFieldSpec applies one decoded, already-validated override value
// onto a piBindingMetadata pointer, or clears it back to source on null.
type piOverrideFieldSpec struct {
	field    string
	setNull  func(*piBindingMetadata)
	setValue func(*piBindingMetadata, any)
}

var piOverrideFieldSpecs = []piOverrideFieldSpec{
	{field: "name", setNull: func(m *piBindingMetadata) { m.Name = nil }, setValue: func(m *piBindingMetadata, v any) { m.Name = v.(*string) }},
	{field: "reasoning", setNull: func(m *piBindingMetadata) { m.Reasoning = nil }, setValue: func(m *piBindingMetadata, v any) { m.Reasoning = v.(*bool) }},
	{field: "input", setNull: func(m *piBindingMetadata) { m.Input = nil }, setValue: func(m *piBindingMetadata, v any) { m.Input = v.([]string) }},
	{field: "context_window", setNull: func(m *piBindingMetadata) { m.ContextWindow = nil }, setValue: func(m *piBindingMetadata, v any) { m.ContextWindow = v.(*int64) }},
	{field: "max_tokens", setNull: func(m *piBindingMetadata) { m.MaxTokens = nil }, setValue: func(m *piBindingMetadata, v any) { m.MaxTokens = v.(*int64) }},
	{field: "thinking_level_map", setNull: func(m *piBindingMetadata) { m.ThinkingLevelMap = nil }, setValue: func(m *piBindingMetadata, v any) { m.ThinkingLevelMap = v.(map[string]*string) }},
	{field: "compat", setNull: func(m *piBindingMetadata) { m.Compat = nil }, setValue: func(m *piBindingMetadata, v any) { m.Compat = v.(map[string]any) }},
}

// decodePiOverrideFields validates a PUT override body against the same Pi
// 0.84.3 schema RenderPi enforces (via modelexport.ValidatePiSourceField), so
// a stored override can never later fail render with a shape error. Values
// are returned already converted to their binding-storage Go type; nil marks
// an explicit restore-to-source.
func decodePiOverrideFields(body []byte, api string) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, newPiDomainError(http.StatusBadRequest, "Invalid request body", nil)
	}
	if len(raw) == 0 {
		return nil, newPiDomainError(http.StatusUnprocessableEntity, "override payload must carry at least one field", map[string]any{"field": "body"})
	}
	values := make(map[string]any, len(raw))
	for key, valueRaw := range raw {
		var spec *piOverrideFieldSpec
		for index := range piOverrideFieldSpecs {
			if piOverrideFieldSpecs[index].field == key {
				spec = &piOverrideFieldSpecs[index]
				break
			}
		}
		if spec == nil {
			return nil, newPiDomainError(http.StatusUnprocessableEntity, fmt.Sprintf("unknown override field %q", key), map[string]any{"field": key})
		}
		if string(valueRaw) == "null" {
			values[key] = nil
			continue
		}
		parsed, err := parsePiOverrideValue(api, key, valueRaw)
		if err != nil {
			return nil, err
		}
		values[key] = parsed
	}
	return values, nil
}

func parsePiOverrideValue(api, field string, raw json.RawMessage) (any, error) {
	invalid := func(reason string) error {
		return newPiDomainError(http.StatusUnprocessableEntity, reason, map[string]any{"field": field})
	}
	switch field {
	case "name":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, invalid("must be a string or null")
		}
		if err := modelexport.ValidatePiSourceField(api, field, value); err != nil {
			return nil, invalid(err.Error())
		}
		return &value, nil
	case "reasoning":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, invalid("must be a boolean or null")
		}
		return &value, nil
	case "context_window", "max_tokens":
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, invalid("must be a whole number or null")
		}
		if err := modelexport.ValidatePiSourceField(api, field, value); err != nil {
			return nil, invalid(err.Error())
		}
		return &value, nil
	case "input":
		var decoded []any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, invalid("must be an array of \"text\"/\"image\" or null")
		}
		if err := modelexport.ValidatePiSourceField(api, field, decoded); err != nil {
			return nil, invalid(err.Error())
		}
		values := make([]string, 0, len(decoded))
		for _, item := range decoded {
			values = append(values, item.(string))
		}
		return values, nil
	case "thinking_level_map":
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, invalid("must be an object of Pi thinking levels or null")
		}
		if err := modelexport.ValidatePiSourceField(api, field, decoded); err != nil {
			return nil, invalid(err.Error())
		}
		values := make(map[string]*string, len(decoded))
		for key, item := range decoded {
			if item == nil {
				values[key] = nil
				continue
			}
			text := item.(string)
			values[key] = &text
		}
		return values, nil
	case "compat":
		decoded, err := decodePiCompatJSON(raw)
		if err != nil {
			return nil, invalid("must be an object or null")
		}
		if err := modelexport.ValidatePiSourceField(api, field, decoded); err != nil {
			return nil, invalid(err.Error())
		}
		return decoded, nil
	}
	return nil, invalid("unsupported override field")
}

// handlePutPiOverride serves PUT /api/models/{model_config_id}/pi/override.
func (s *Service) handlePutPiOverride(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	now := s.nowUTC()
	response, txErr := s.putPiOverrideInTransaction(r.Context(), r, modelConfigID, body, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) putPiOverrideInTransaction(ctx context.Context, r *http.Request, modelConfigID int, body []byte, now time.Time) (piBindingResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (piBindingResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return piBindingResponse{}, profileErr
		}
		model, found, modelErr := loadModelRecord(ctx, tx, profile.ID, modelConfigID, true)
		if modelErr != nil {
			return piBindingResponse{}, modelErr
		}
		if !found {
			return piBindingResponse{}, newPiDomainError(http.StatusNotFound, "Model configuration not found", nil)
		}
		var binding piBindingRecord
		if boundErr := loadBoundPiBindingForUpdate(ctx, tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return piBindingResponse{}, boundErr
		}
		if validateErr := validatePiBindingForModel(binding, model); validateErr != nil {
			return piBindingResponse{}, validateErr
		}
		values, decodeErr := decodePiOverrideFields(body, binding.API)
		if decodeErr != nil {
			return piBindingResponse{}, decodeErr
		}
		for _, spec := range piOverrideFieldSpecs {
			value, present := values[spec.field]
			if !present {
				continue
			}
			if value == nil {
				spec.setNull(&binding.Override)
				continue
			}
			spec.setValue(&binding.Override, value)
		}
		binding.UpdatedAt = nextPiBindingUpdatedAt(binding.UpdatedAt, now)
		if upsertErr := upsertPiBinding(ctx, tx, binding, binding.UpdatedAt); upsertErr != nil {
			return piBindingResponse{}, upsertErr
		}
		saved, _, saveErr := loadPiBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return piBindingResponse{}, saveErr
		}
		return saved.response(), nil
	})
}

// handleClearPiOverride serves DELETE /api/models/{model_config_id}/pi/override.
func (s *Service) handleClearPiOverride(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	now := s.nowUTC()
	response, txErr := s.clearPiOverrideInTransaction(r.Context(), r, modelConfigID, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) clearPiOverrideInTransaction(ctx context.Context, r *http.Request, modelConfigID int, now time.Time) (piBindingResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (piBindingResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return piBindingResponse{}, profileErr
		}
		model, found, modelErr := loadModelRecord(ctx, tx, profile.ID, modelConfigID, true)
		if modelErr != nil {
			return piBindingResponse{}, modelErr
		}
		if !found {
			return piBindingResponse{}, newPiDomainError(http.StatusNotFound, "Model configuration not found", nil)
		}
		var binding piBindingRecord
		if boundErr := loadBoundPiBindingForUpdate(ctx, tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return piBindingResponse{}, boundErr
		}
		if validateErr := validatePiBindingForModel(binding, model); validateErr != nil {
			return piBindingResponse{}, validateErr
		}
		binding.Override = piBindingMetadata{}
		binding.UpdatedAt = nextPiBindingUpdatedAt(binding.UpdatedAt, now)
		if upsertErr := upsertPiBinding(ctx, tx, binding, binding.UpdatedAt); upsertErr != nil {
			return piBindingResponse{}, upsertErr
		}
		saved, _, saveErr := loadPiBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return piBindingResponse{}, saveErr
		}
		return saved.response(), nil
	})
}

// handleUnbindModelPi serves DELETE /api/models/{model_config_id}/pi.
func (s *Service) handleUnbindModelPi(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	response, txErr := s.unbindPiInTransaction(r.Context(), r, modelConfigID)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) unbindPiInTransaction(ctx context.Context, r *http.Request, modelConfigID int) (piBindingResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (piBindingResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return piBindingResponse{}, profileErr
		}
		_, found, modelErr := loadModelRecord(ctx, tx, profile.ID, modelConfigID, true)
		if modelErr != nil {
			return piBindingResponse{}, modelErr
		}
		if !found {
			return piBindingResponse{}, newPiDomainError(http.StatusNotFound, "Model configuration not found", nil)
		}
		if _, _, lockErr := loadPiBindingForUpdate(ctx, tx, profile.ID, modelConfigID); lockErr != nil {
			return piBindingResponse{}, lockErr
		}
		if err := deletePiBinding(ctx, tx, modelConfigID); err != nil {
			return piBindingResponse{}, err
		}
		return piBindingRecord{}.response(), nil
	})
}
