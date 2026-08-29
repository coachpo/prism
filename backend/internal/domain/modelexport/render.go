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

type RenderResult struct {
	Platform      Platform            `json:"platform"`
	Content       string              `json:"-"`
	ContentSHA256 string              `json:"content_sha256"`
	FileName      string              `json:"file_name"`
	MIMEType      string              `json:"mime_type"`
	ModelResults  []ModelRenderResult `json:"model_results"`
	Warnings      []string            `json:"warnings"`
}

type ModelRenderResult struct {
	ModelConfigID   int      `json:"model_config_id"`
	ModelID         string   `json:"model_id"`
	CostExported    bool     `json:"cost_exported"`
	WarningCodes    []string `json:"warning_codes,omitempty"`
	MissingMetadata []string `json:"missing_metadata,omitempty"`
}

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
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: "not_found_in_default_profile"}
		}
		if !fact.Selectable {
			reason := "unselectable"
			if fact.UnselectableReason != nil {
				reason = *fact.UnselectableReason
			}
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: reason}
		}
		if len(fact.PiCandidates) > 1 && fact.PiSelected == nil {
			return nil, &ErrCandidateUnselected{ModelConfigID: id}
		}
	}
	return deduped, nil
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
		FileName:      PiFileName,
	}
	return result, nil
}

func checkLockedPath(key string, lockedPaths []string) error {
	for _, locked := range lockedPaths {
		if key == locked || strings.HasPrefix(key, locked+".") || strings.HasPrefix(locked, key+".") {
			return &ErrLockedField{Field: key}
		}
	}
	return nil
}

func checkLockedLeafPath(path string, lockedPaths []string) error {
	for _, locked := range lockedPaths {
		if path == locked || strings.HasPrefix(path, locked+".") {
			return &ErrLockedField{Field: path}
		}
	}
	return nil
}

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

type rawJSON json.RawMessage

func (r rawJSON) MarshalJSON() ([]byte, error) { return r, nil }
