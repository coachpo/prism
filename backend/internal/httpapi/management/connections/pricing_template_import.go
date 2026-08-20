package connections

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleImportPricingTemplates(w http.ResponseWriter, r *http.Request) {
	// Preview-only import (SPEC 7.6): validates every row and returns the
	// per-row action, the summary and a preview hash binding the canonical
	// payload; nothing is written until the commit endpoint replays the
	// identical payload.
	response, err := s.previewPricingTemplateImport(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCommitPricingTemplateImport(w http.ResponseWriter, r *http.Request) {
	var requestBody pricingTemplateImportCommitRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if requestBody.SchemaVersion != 2 {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "schema_version must be 2"})
		return
	}
	preview, err := s.previewImportPayload(r, pricingTemplateImportRequest{SchemaVersion: requestBody.SchemaVersion, Mode: requestBody.Mode, Templates: requestBody.Templates})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if strings.TrimSpace(requestBody.PreviewHash) != preview.PreviewHash {
		responseutil.WriteJSON(w, http.StatusConflict, pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{{Detail: "pricing_import_stale: the import preview no longer matches the submitted payload"}}})
		return
	}
	if !preview.Committable {
		responseutil.WriteJSON(w, http.StatusConflict, pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{{Detail: "pricing_import_preview_required: the import preview is not committable"}}})
		return
	}
	commitResponse, err := pgxutil.InTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateImportResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		if err := lockProfileRow(r.Context(), tx, profile.ID); err != nil {
			return pricingTemplateImportResponse{}, err
		}
		return commitPricingTemplateImport(r.Context(), tx, profile.ID, s.nowUTC(), preview)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, commitResponse)
}

func (s *Service) previewPricingTemplateImport(r *http.Request) (pricingTemplateImportResponse, error) {
	body, err := decodeJSONRawBody(r)
	if err != nil {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Invalid request body"}
	}
	if err := pricingTemplateImportKeysPresent(body); err != nil {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
	}
	var requestBody pricingTemplateImportRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requestBody); err != nil {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: responseutil.SanitizeDecodeError(err).Error()}
	}
	return s.previewImportPayload(r, requestBody)
}

func (s *Service) previewImportPayload(r *http.Request, requestBody pricingTemplateImportRequest) (pricingTemplateImportResponse, error) {
	if requestBody.SchemaVersion != 2 {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "schema_version must be 2"}
	}
	mode := strings.TrimSpace(requestBody.Mode)
	if mode != "upsert_by_name" && mode != "create_only" {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "mode must be upsert_by_name or create_only"}
	}
	if len(requestBody.Templates) == 0 {
		return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "templates must not be empty"}
	}
	rows := make([]pricingTemplateImportRow, 0, len(requestBody.Templates))
	seen := map[string]int{}
	importErrors := make([]pricingTemplateImportError, 0)
	for index, template := range requestBody.Templates {
		name, err := normalizePricingTemplateName(template.Name)
		if err != nil {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: strings.TrimSpace(template.Name), Detail: err.Error()})
			continue
		}
		if firstIndex, ok := seen[name]; ok {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: name, Detail: fmt.Sprintf("duplicate name also appears at index %d", firstIndex)})
			continue
		}
		seen[name] = index
		shape, err := normalizePricingTemplateShape(template)
		if err != nil {
			importErrors = append(importErrors, pricingTemplateImportError{Index: index, Name: name, Detail: err.Error()})
			continue
		}
		rows = append(rows, pricingTemplateImportRow{Name: name, Description: normalizeOptionalTrimmedString(template.Description), Shape: shape})
	}
	if len(importErrors) > 0 {
		return pricingTemplateImportResponse{Skipped: []string{}, Errors: importErrors}, nil
	}

	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateImportResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		existingItems, err := listPricingTemplates(r.Context(), tx, profile.ID)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		existingByName := map[string]pricingTemplateResponse{}
		for _, existing := range existingItems {
			existingByName[existing.Name] = existing
		}
		result := pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{}, Mode: mode, Rows: rows}
		for _, row := range rows {
			current, exists := existingByName[row.Name]
			if !exists {
				result.Created++
				result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "create", TemplateKind: string(row.Shape.Kind)})
				continue
			}
			if mode == "create_only" {
				result.Skipped = append(result.Skipped, row.Name)
				result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "skipped", TemplateKind: string(row.Shape.Kind)})
				continue
			}
			if pricingTemplateShapesEqual(pricingTemplateShapeFromResponse(current), row.Shape) {
				result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "no_op", TemplateKind: string(row.Shape.Kind)})
				continue
			}
			result.Updated++
			result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "update", TemplateKind: string(row.Shape.Kind), CurrentVersion: current.Version, NextVersion: current.Version + 1})
		}
		hashInput, err := json.Marshal(map[string]any{"schema_version": requestBody.SchemaVersion, "mode": mode, "profile_id": profile.ID, "rows": result.Items})
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		sum := sha256.Sum256(hashInput)
		result.PreviewHash = fmt.Sprintf("%x", sum[:])
		result.Committable = true
		return result, nil
	})
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	return response, nil
}

func commitPricingTemplateImport(ctx context.Context, tx pgx.Tx, profileID int, currentTime time.Time, preview pricingTemplateImportResponse) (pricingTemplateImportResponse, error) {
	existingItems, err := listPricingTemplates(ctx, tx, profileID)
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	existingByName := map[string]pricingTemplateResponse{}
	for _, existing := range existingItems {
		existingByName[existing.Name] = existing
	}
	result := pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{}, Rows: preview.Rows}
	for _, item := range preview.Items {
		switch item.Action {
		case "create":
			var row pricingTemplateImportRow
			for _, candidate := range preview.Rows {
				if candidate.Name == item.Name {
					row = candidate
					break
				}
			}
			if _, err := createPricingTemplateWithShape(ctx, tx, profileID, currentTime, row.Name, row.Description, row.Shape); err != nil {
				return pricingTemplateImportResponse{}, err
			}
			result.Created++
		case "update":
			current, ok := existingByName[item.Name]
			if !ok {
				return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: fmt.Sprintf("pricing_import_stale: template %q disappeared", item.Name)}
			}
			var row pricingTemplateImportRow
			for _, candidate := range preview.Rows {
				if candidate.Name == item.Name {
					row = candidate
					break
				}
			}
			if err := updatePricingTemplateWithShape(ctx, tx, profileID, current, row.Name, row.Description, row.Shape, currentTime); err != nil {
				return pricingTemplateImportResponse{}, err
			}
			result.Updated++
		case "skipped":
			result.Skipped = append(result.Skipped, item.Name)
		default:
			// no_op: nothing to write, nothing to report
		}
	}
	return result, nil
}

type pricingTemplateImportRow struct {
	Name        string
	Description *string
	Shape       pricingTemplateShape
}

// pricingTemplateImportKeysPresent rejects unknown top-level keys and
// requires the mode/templates keys (fail-closed strict decoder, SPEC 5.3).
func pricingTemplateImportKeysPresent(body []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	for key := range raw {
		if key != "schema_version" && key != "mode" && key != "templates" {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	version, ok := raw["schema_version"]
	if !ok {
		return fmt.Errorf("schema_version is required")
	}
	var schemaVersion int
	if err := json.Unmarshal(version, &schemaVersion); err != nil || schemaVersion != 2 {
		return fmt.Errorf("schema_version must be 2")
	}
	if _, ok := raw["mode"]; !ok {
		return fmt.Errorf("mode is required")
	}
	templatesRaw, ok := raw["templates"]
	if !ok {
		return fmt.Errorf("templates is required")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(templatesRaw, &rows); err != nil {
		return fmt.Errorf("templates must be an array")
	}
	for index, row := range rows {
		if err := pricingTemplateRowKeysPresent(row); err != nil {
			return fmt.Errorf("templates[%d]: %w", index, err)
		}
	}
	return nil
}

type pricingTemplateImportRequest struct {
	SchemaVersion int                            `json:"schema_version"`
	Mode          string                         `json:"mode"`
	Templates     []pricingTemplateCreateRequest `json:"templates"`
}

type pricingTemplateImportCommitRequest struct {
	SchemaVersion int                            `json:"schema_version"`
	Mode          string                         `json:"mode"`
	Templates     []pricingTemplateCreateRequest `json:"templates"`
	PreviewHash   string                         `json:"preview_hash"`
}

type pricingTemplateImportItem struct {
	Name           string `json:"name"`
	Action         string `json:"action"`
	TemplateKind   string `json:"template_kind,omitempty"`
	CurrentVersion int    `json:"current_version,omitempty"`
	NextVersion    int    `json:"next_version,omitempty"`
}

type pricingTemplateImportError struct {
	Index  int    `json:"index"`
	Name   string `json:"name,omitempty"`
	Detail string `json:"detail"`
}

type pricingTemplateImportResponse struct {
	Created     int                          `json:"created"`
	Updated     int                          `json:"updated"`
	Skipped     []string                     `json:"skipped"`
	Errors      []pricingTemplateImportError `json:"errors"`
	Mode        string                       `json:"mode,omitempty"`
	Items       []pricingTemplateImportItem  `json:"items,omitempty"`
	PreviewHash string                       `json:"preview_hash,omitempty"`
	Committable bool                         `json:"committable"`
	Rows        []pricingTemplateImportRow   `json:"-"`
}
