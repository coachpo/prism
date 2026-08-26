package modelexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// RenderResult is one deterministic generated file plus its audit trail.
type RenderResult struct {
	// Platform the file targets.
	Platform Platform `json:"platform"`
	// Content is the full UTF-8 JSON document including the trailing newline.
	Content string `json:"-"`
	// ContentSHA256 is the exact SHA-256 hex digest of Content's UTF-8 bytes.
	ContentSHA256 string `json:"content_sha256"`
	// FileName is the fixed download name for this platform.
	FileName string `json:"file_name"`
	// MIMEType is the fixed media type used for copies and downloads.
	MIMEType string `json:"mime_type"`
	// ModelResults carries the per-model audit trail in render order.
	ModelResults []ModelRenderResult `json:"model_results"`
	// Warnings holds document-level warning codes, deduplicated and sorted.
	Warnings []string `json:"warnings"`
}

// ModelRenderResult is the honest per-model outcome: the model always renders,
// while missing metadata and omitted prices stay visible warnings instead of
// being disguised as complete configuration.
type ModelRenderResult struct {
	ModelConfigID int    `json:"model_config_id"`
	ModelID       string `json:"model_id"`
	// CostExported reports whether a cost group was emitted.
	CostExported bool     `json:"cost_exported"`
	WarningCodes []string `json:"warning_codes,omitempty"`
	// MissingMetadata lists known metadata leaves absent after the merge.
	MissingMetadata []string `json:"missing_metadata,omitempty"`
}

// Typed domain failures live in errors.go.

// NormalizeSelection validates the explicit selection truth: non-empty input,
// duplicate removal, stable ascending order, and every id present and
// selectable. Any violation fails the whole request.
func NormalizeSelection(ids []int, facts SourceFacts) ([]int, error) {
	if len(ids) == 0 {
		return nil, errors.New("model_config_ids must not be empty")
	}
	seen := make(map[int]struct{}, len(ids))
	deduped := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	sort.Ints(deduped)
	byID := make(map[int]ModelFact, len(facts.Models))
	for _, fact := range facts.Models {
		byID[fact.ModelConfigID] = fact
	}
	for _, id := range deduped {
		fact, ok := byID[id]
		if !ok {
			// Unknown ids cover cross-profile references too: another
			// profile's model never appears in this snapshot.
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: "not_found_in_default_profile"}
		}
		if !fact.Selectable {
			reason := "unselectable"
			if fact.UnselectableReason != nil {
				reason = *fact.UnselectableReason
			}
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: reason}
		}
	}
	return deduped, nil
}

// ManualEnhancement is the operator-authored third merge layer for one model.
type ManualEnhancement struct {
	// Fields is a JSON object keyed by platform field names.
	Fields json.RawMessage
	// OverrideFields names the top-level keys allowed to replace values that
	// earlier layers already provided.
	OverrideFields []string
}

// Validate rejects sensitive recursive keys anywhere inside the payload.
func (m ManualEnhancement) Validate() error {
	fields, err := decodeEnhancementObject(m.Fields)
	if err != nil {
		return err
	}
	return rejectSensitiveValue(fields, "")
}

// ValidateForPlatform validates the complete manual boundary before a renderer
// mutates its output object. It combines recursive secret rejection, locked
// Prism-owned paths, exact override paths, and the pinned target model schema.
func (m ManualEnhancement) ValidateForPlatform(platform Platform) error {
	if err := m.Validate(); err != nil {
		return err
	}
	fields, err := decodeEnhancementObject(m.Fields)
	if err != nil {
		return err
	}
	lockedPaths := piLockedPaths
	schemaValidator := validatePiEnhancement
	if platform == PlatformOpenCode {
		lockedPaths = ocLockedPaths
		schemaValidator = validateOpenCodeEnhancement
	} else if platform != PlatformPi {
		return &ErrInvalidEnhancement{Reason: fmt.Sprintf("unsupported platform %q", platform)}
	}
	if err := rejectLockedEnhancementPaths(fields, "", lockedPaths); err != nil {
		return err
	}
	seenOverrides := map[string]struct{}{}
	for _, rawPath := range m.OverrideFields {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			return &ErrInvalidEnhancement{Field: "override_fields", Reason: "must not contain an empty path"}
		}
		if _, duplicate := seenOverrides[path]; duplicate {
			return &ErrInvalidEnhancement{Field: path, Reason: "duplicate override path"}
		}
		seenOverrides[path] = struct{}{}
		if err := checkLockedPath(path, lockedPaths); err != nil {
			return err
		}
		for _, segment := range strings.Split(path, ".") {
			if KeyLooksSensitive(segment) {
				return &ErrSensitiveField{Field: path}
			}
		}
		if !enhancementPathExists(fields, path) {
			return &ErrInvalidEnhancement{Field: path, Reason: "override path is absent from fields"}
		}
	}
	return schemaValidator(fields)
}

func decodeEnhancementObject(raw json.RawMessage) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, &ErrInvalidEnhancement{Reason: "payload must be one JSON object"}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, &ErrInvalidEnhancement{Reason: "payload must contain exactly one JSON object"}
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return nil, &ErrInvalidEnhancement{Reason: "payload must be a JSON object"}
	}
	return fields, nil
}

func rejectLockedEnhancementPaths(fields map[string]any, prefix string, lockedPaths []string) error {
	for key, value := range fields {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if err := checkLockedLeafPath(path, lockedPaths); err != nil {
			return err
		}
		if nested, ok := value.(map[string]any); ok {
			if err := rejectLockedEnhancementPaths(nested, path, lockedPaths); err != nil {
				return err
			}
		}
	}
	return nil
}

func enhancementPathExists(fields map[string]any, path string) bool {
	var current any = fields
	segments := strings.Split(path, ".")
	for index, segment := range segments {
		object, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current, ok = object[segment]
		if !ok {
			return false
		}
		if index == len(segments)-1 {
			return true
		}
	}
	return false
}

func invalidTargetField(field string, reason string) error {
	return &ErrInvalidEnhancement{Field: field, Reason: reason}
}

func targetString(value any, field string, nonEmpty bool) error {
	text, ok := value.(string)
	if !ok || (nonEmpty && text == "") {
		return invalidTargetField(field, "must be a string"+map[bool]string{true: " with at least one character"}[nonEmpty])
	}
	return nil
}

func targetBool(value any, field string) error {
	if _, ok := value.(bool); !ok {
		return invalidTargetField(field, "must be a boolean")
	}
	return nil
}

func targetNumber(value any, field string) error {
	var literal string
	switch typed := value.(type) {
	case json.Number:
		literal = typed.String()
	case decimal:
		literal = string(typed)
	case int:
		literal = strconv.Itoa(typed)
	case int32:
		literal = strconv.FormatInt(int64(typed), 10)
	case int64:
		literal = strconv.FormatInt(typed, 10)
	case float64:
		literal = strconv.FormatFloat(typed, 'g', -1, 64)
	default:
		return invalidTargetField(field, "must be a finite number")
	}
	parsed, err := strconv.ParseFloat(literal, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return invalidTargetField(field, "must be a finite number")
	}
	return nil
}

func targetSchemaError(err error) error {
	if err == nil {
		return nil
	}
	var invalid *ErrInvalidEnhancement
	if errors.As(err, &invalid) {
		return &ErrTargetSchema{Field: invalid.Field, Reason: invalid.Reason}
	}
	return err
}

func targetHTTPURL(value any, field string) error {
	text, ok := value.(string)
	if !ok || text == "" {
		return invalidTargetField(field, "must be a non-empty HTTP(S) URL")
	}
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return invalidTargetField(field, "must be a non-empty HTTP(S) URL without credentials, query, or fragment")
	}
	return nil
}

func targetStringArray(value any, field string, allowed map[string]struct{}) error {
	items, ok := value.([]any)
	if !ok {
		return invalidTargetField(field, "must be an array of strings")
	}
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return invalidTargetField(fmt.Sprintf("%s[%d]", field, index), "must be a string")
		}
		if allowed != nil {
			if _, accepted := allowed[text]; !accepted {
				return invalidTargetField(fmt.Sprintf("%s[%d]", field, index), "contains an unsupported value")
			}
		}
	}
	return nil
}

func targetStringRecord(value any, field string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object of string values")
	}
	for key, item := range object {
		if _, ok := item.(string); !ok {
			return invalidTargetField(field+"."+key, "must be a string")
		}
	}
	return nil
}

func sortedAnyKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizedTargetDocument(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, &ErrTargetSchema{Reason: fmt.Sprintf("document is not JSON-serializable: %v", err)}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, &ErrTargetSchema{Reason: "document is not valid JSON"}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, &ErrTargetSchema{Reason: "document must contain exactly one JSON value"}
	}
	object, ok := decoded.(map[string]any)
	if !ok {
		return nil, &ErrTargetSchema{Reason: "document root must be an object"}
	}
	return object, nil
}

func rejectSensitiveValue(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if KeyLooksSensitive(key) {
				return &ErrSensitiveField{Field: childPath}
			}
			if err := rejectSensitiveValue(typed[key], childPath); err != nil {
				return err
			}
		}
	case []any:
		for index, item := range typed {
			if err := rejectSensitiveValue(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case nil, string, bool, json.Number:
		return nil
	default:
		return fmt.Errorf("enhancement field %q has an unsupported JSON value", path)
	}
	return nil
}

// decimal is a canonical decimal literal marshalled verbatim as a JSON
// number. Price values flow through this type so the emitted bytes match the
// configured literal exactly — no float formatting, no rounding surprises,
// explicit zeros preserved as 0.
type decimal string

func (d decimal) MarshalJSON() ([]byte, error) {
	text := string(d)
	if text == "" {
		return nil, errors.New("empty decimal literal")
	}
	integral, fractional := text, ""
	if cutIntegral, cutFractional, found := strings.Cut(text, "."); found {
		integral, fractional = cutIntegral, cutFractional
	}
	if integral == "" {
		integral = "0"
	}
	for _, r := range integral {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("decimal %q is not a plain non-negative number", text)
		}
	}
	for _, r := range fractional {
		if r < '0' || r > '9' {
			return nil, fmt.Errorf("decimal %q is not a plain non-negative number", text)
		}
	}
	return []byte(text), nil
}

// finalizeDocument serializes the document with a trailing newline and derives
// the SHA-256 over exactly those UTF-8 bytes.
func finalizeDocument(platform Platform, document any) (*RenderResult, error) {
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s document: %w", platform, err)
	}
	content := string(raw) + "\n"
	sum := sha256.Sum256([]byte(content))
	result := &RenderResult{
		Platform:      platform,
		Content:       content,
		ContentSHA256: hex.EncodeToString(sum[:]),
		MIMEType:      "application/json;charset=utf-8",
	}
	switch platform {
	case PlatformPi:
		result.FileName = PiFileName
	case PlatformOpenCode:
		result.FileName = OpenCodeFileName
	}
	return result, nil
}

// RenderDispatch is the platform-agnostic render payload shared by both
// renderers.
type RenderDispatch struct {
	Facts         SourceFacts
	Selection     []int
	Enrichment    map[int]PlatformCandidate
	Enhancements  map[int]ManualEnhancement
	BaseURL       string
	ProviderID    string
	IncludeAPIKey bool
	APIKey        string
	DefaultModel  *int
}

// DispatchRender routes one validated payload to the platform renderer. Both
// renderers share the same dispatch contract and differ only in document
// assembly.
func DispatchRender(platform Platform, payload RenderDispatch) (*RenderResult, error) {
	switch platform {
	case PlatformPi:
		if payload.DefaultModel != nil {
			return nil, &ErrDefaultModel{Reason: "Pi does not support a config-level default model"}
		}
		return RenderPi(PiInput{
			Facts:         payload.Facts,
			Selection:     payload.Selection,
			Enrichment:    payload.Enrichment,
			Enhancements:  payload.Enhancements,
			BaseURL:       payload.BaseURL,
			ProviderID:    payload.ProviderID,
			IncludeAPIKey: payload.IncludeAPIKey,
			APIKey:        payload.APIKey,
		})
	case PlatformOpenCode:
		return RenderOpenCode(OpenCodeInput{
			Facts:         payload.Facts,
			Selection:     payload.Selection,
			Enrichment:    payload.Enrichment,
			Enhancements:  payload.Enhancements,
			BaseURL:       payload.BaseURL,
			ProviderID:    payload.ProviderID,
			IncludeAPIKey: payload.IncludeAPIKey,
			APIKey:        payload.APIKey,
			DefaultModel:  payload.DefaultModel,
		})
	default:
		return nil, fmt.Errorf("unsupported export platform %q", platform)
	}
}

// applyEnhancement merges the manual layer into the generated model object
// following the three-layer contract: fill missing keys, overwrite only
// listed keys, fail closed on locked paths and sensitive keys anywhere.
func applyEnhancement(target map[string]any, enhancement ManualEnhancement, lockedPaths []string) error {
	fields, err := decodeEnhancementObject(enhancement.Fields)
	if err != nil {
		return err
	}
	override := make(map[string]struct{}, len(enhancement.OverrideFields))
	for _, field := range enhancement.OverrideFields {
		field = strings.TrimSpace(field)
		if field == "" {
			return &ErrInvalidEnhancement{Field: "override_fields", Reason: "must not contain an empty path"}
		}
		if err := checkLockedPath(field, lockedPaths); err != nil {
			return err
		}
		for _, segment := range strings.Split(field, ".") {
			if KeyLooksSensitive(segment) {
				return &ErrSensitiveField{Field: field}
			}
		}
		override[field] = struct{}{}
	}
	return mergeEnhancementObject(target, fields, "", override, lockedPaths)
}

func mergeEnhancementObject(target map[string]any, fields map[string]any, prefix string, override map[string]struct{}, lockedPaths []string) error {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		value := fields[key]
		if nested, ok := value.(map[string]any); ok {
			if existing, present := target[key]; present {
				if existingObject, objectOK := existing.(map[string]any); objectOK {
					if err := mergeEnhancementObject(existingObject, nested, path, override, lockedPaths); err != nil {
						return err
					}
					continue
				}
				if _, allowed := override[path]; !allowed {
					continue
				}
			}
			copyObject := map[string]any{}
			if err := mergeEnhancementObject(copyObject, nested, path, override, lockedPaths); err != nil {
				return err
			}
			target[key] = copyObject
			continue
		}
		if err := checkLockedLeafPath(path, lockedPaths); err != nil {
			return err
		}
		if _, exists := target[key]; exists {
			if _, allowed := override[path]; !allowed {
				continue
			}
		}
		target[key] = value
	}
	return nil
}

// checkLockedPath fails closed when a manual key lands on Prism-managed truth
// or inside one of its subtrees.
func checkLockedPath(key string, lockedPaths []string) error {
	for _, locked := range lockedPaths {
		if key == locked || strings.HasPrefix(key, locked+".") || strings.HasPrefix(locked, key+".") {
			return &ErrLockedField{Field: key}
		}
	}
	return nil
}

// checkLockedLeafPath rejects the exact locked path and anything below it,
// while allowing a containing object to carry unrelated safe siblings.
func checkLockedLeafPath(path string, lockedPaths []string) error {
	for _, locked := range lockedPaths {
		if path == locked || strings.HasPrefix(path, locked+".") {
			return &ErrLockedField{Field: path}
		}
	}
	return nil
}

// decodeCanonicalJSON converts a raw message into ordered generic values for
// embedding into the output tree.
func decodeCanonicalJSON(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("JSON value must contain exactly one value")
	}
	return decoded, nil
}

// rawJSON embeds an already-encoded JSON value verbatim.
type rawJSON json.RawMessage

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }
