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

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

const pricingTemplateImportSchemaVersion = 3

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
	if requestBody.SchemaVersion != pricingTemplateImportSchemaVersion {
		writeDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusBadRequest, Detail: "schema_version must be 3"})
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
		request := pricingTemplateImportRequest{SchemaVersion: requestBody.SchemaVersion, Mode: requestBody.Mode, Templates: requestBody.Templates}
		mode, rows, importErrors, err := normalizePricingTemplateImportRows(request)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		if len(importErrors) > 0 {
			return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "pricing_import_preview_required: the import payload is not committable"}
		}
		preview, err := buildPricingTemplateImportPreview(r.Context(), tx, profile.ID, request.SchemaVersion, mode, rows)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		if strings.TrimSpace(requestBody.PreviewHash) != preview.PreviewHash {
			return pricingTemplateImportResponse{}, &domainError{StatusCode: http.StatusConflict, Detail: "pricing_import_stale: the import preview no longer matches the submitted payload or current template state"}
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
	mode, rows, importErrors, err := normalizePricingTemplateImportRows(requestBody)
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	if len(importErrors) > 0 {
		return pricingTemplateImportResponse{Skipped: []string{}, Errors: importErrors}, nil
	}
	return pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "connection", func(tx pgx.Tx) (pricingTemplateImportResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return pricingTemplateImportResponse{}, err
		}
		return buildPricingTemplateImportPreview(r.Context(), tx, profile.ID, requestBody.SchemaVersion, mode, rows)
	})
}

func normalizePricingTemplateImportRows(requestBody pricingTemplateImportRequest) (string, []pricingTemplateImportRow, []pricingTemplateImportError, error) {
	if requestBody.SchemaVersion != pricingTemplateImportSchemaVersion {
		return "", nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "schema_version must be 3"}
	}
	mode := strings.TrimSpace(requestBody.Mode)
	if mode != "upsert_by_name" && mode != "create_only" {
		return "", nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "mode must be upsert_by_name or create_only"}
	}
	if len(requestBody.Templates) == 0 {
		return "", nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "templates must not be empty"}
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
	return mode, rows, importErrors, nil
}

func buildPricingTemplateImportPreview(ctx context.Context, exec queryExecutor, profileID, schemaVersion int, mode string, rows []pricingTemplateImportRow) (pricingTemplateImportResponse, error) {
	existingItems, err := listPricingTemplates(ctx, exec, profileID)
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	existingByName := map[string]pricingTemplateResponse{}
	for _, existing := range existingItems {
		existingByName[existing.Name] = existing
	}
	result := pricingTemplateImportResponse{Skipped: []string{}, Errors: []pricingTemplateImportError{}, Mode: mode, Rows: rows}
	hashRows := make([]pricingTemplateImportHashRow, 0, len(rows))
	for _, row := range rows {
		current, exists := existingByName[row.Name]
		if !exists {
			result.Created++
			result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "create", TemplateKind: string(row.Shape.Kind), NextVersion: 1, PricingStructureChanged: true})
			hashRows = append(hashRows, newPricingTemplateImportHashRow(row, pricingTemplateResponse{}, "create"))
			continue
		}
		if mode == "create_only" {
			result.Skipped = append(result.Skipped, row.Name)
			result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "skipped", TemplateKind: string(row.Shape.Kind), CurrentVersion: current.Version, NextVersion: current.Version})
			hashRows = append(hashRows, newPricingTemplateImportHashRow(row, current, "skipped"))
			continue
		}
		shapeChanged := !pricingTemplateShapesEqual(pricingTemplateShapeFromResponse(current), row.Shape)
		metadataChanged := !stringsEqualPointers(current.Description, row.Description)
		if !shapeChanged && !metadataChanged {
			result.Items = append(result.Items, pricingTemplateImportItem{Name: row.Name, Action: "no_op", TemplateKind: string(row.Shape.Kind), CurrentVersion: current.Version, NextVersion: current.Version})
			hashRows = append(hashRows, newPricingTemplateImportHashRow(row, current, "no_op"))
			continue
		}
		result.Updated++
		nextVersion := current.Version
		if shapeChanged {
			nextVersion++
		}
		result.Items = append(result.Items, pricingTemplateImportItem{
			Name: row.Name, Action: "update", TemplateKind: string(row.Shape.Kind),
			CurrentVersion: current.Version, NextVersion: nextVersion,
			TemplateKindChanged: current.TemplateKind != string(row.Shape.Kind), PricingStructureChanged: shapeChanged,
		})
		hashRows = append(hashRows, newPricingTemplateImportHashRow(row, current, "update"))
	}
	hashInput, err := json.Marshal(map[string]any{"schema_version": schemaVersion, "mode": mode, "profile_id": profileID, "rows": hashRows})
	if err != nil {
		return pricingTemplateImportResponse{}, err
	}
	sum := sha256.Sum256(hashInput)
	result.PreviewHash = fmt.Sprintf("%x", sum[:])
	result.Committable = true
	return result, nil
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

type pricingTemplateImportHashCard struct {
	Role               string  `json:"role"`
	InputPrice         string  `json:"input_price"`
	OutputPrice        string  `json:"output_price"`
	CachedInputPrice   *string `json:"cached_input_price"`
	CacheCreationPrice *string `json:"cache_creation_price"`
	ReasoningPrice     *string `json:"reasoning_price"`
}

type pricingTemplateImportHashWindow struct {
	WeekdayMask int `json:"weekday_mask"`
	StartMinute int `json:"start_minute"`
	EndMinute   int `json:"end_minute"`
}

type pricingTemplateImportHashRow struct {
	Name             string                            `json:"name"`
	Description      *string                           `json:"description"`
	TemplateKind     string                            `json:"template_kind"`
	Cards            []pricingTemplateImportHashCard   `json:"cards"`
	TierThreshold    *int                              `json:"tier_threshold"`
	Timezone         *string                           `json:"timezone"`
	Windows          []pricingTemplateImportHashWindow `json:"windows"`
	Digest           string                            `json:"digest"`
	Action           string                            `json:"action"`
	CurrentTemplate  int                               `json:"current_template_id"`
	CurrentRevision  int64                             `json:"current_revision_id"`
	CurrentVersion   int                               `json:"current_version"`
	CurrentUpdatedAt string                            `json:"current_updated_at"`
}

func newPricingTemplateImportHashRow(row pricingTemplateImportRow, current pricingTemplateResponse, action string) pricingTemplateImportHashRow {
	view := pricingTemplateImportHashRow{
		Name: row.Name, Description: cloneString(row.Description), TemplateKind: string(row.Shape.Kind),
		TierThreshold: cloneTemplateInt(row.Shape.TierThreshold), Timezone: cloneString(row.Shape.Timezone),
		Digest: row.Shape.Digest, Action: action, CurrentTemplate: current.ID,
		CurrentRevision: current.RevisionID, CurrentVersion: current.Version,
	}
	if !current.UpdatedAt.IsZero() {
		view.CurrentUpdatedAt = current.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	for _, role := range pricingkind.RolesFor(row.Shape.Kind) {
		card := row.Shape.Cards[role]
		view.Cards = append(view.Cards, pricingTemplateImportHashCard{
			Role: role, InputPrice: card.InputPrice, OutputPrice: card.OutputPrice,
			CachedInputPrice: cloneString(card.CachedInputPrice), CacheCreationPrice: cloneString(card.CacheCreationPrice),
			ReasoningPrice: cloneString(card.ReasoningPrice),
		})
	}
	for _, window := range row.Shape.Windows {
		view.Windows = append(view.Windows, pricingTemplateImportHashWindow{WeekdayMask: window.WeekdayMask, StartMinute: window.StartMinute, EndMinute: window.EndMinute})
	}
	return view
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
	if err := json.Unmarshal(version, &schemaVersion); err != nil || schemaVersion != pricingTemplateImportSchemaVersion {
		return fmt.Errorf("schema_version must be 3")
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
	Name                    string `json:"name"`
	Action                  string `json:"action"`
	TemplateKind            string `json:"template_kind,omitempty"`
	CurrentVersion          int    `json:"current_version,omitempty"`
	NextVersion             int    `json:"next_version,omitempty"`
	TemplateKindChanged     bool   `json:"template_kind_changed"`
	PricingStructureChanged bool   `json:"pricing_structure_changed"`
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
