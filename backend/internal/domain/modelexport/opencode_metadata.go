package modelexport

import (
	"encoding/json"
	"math/big"
	"strings"
)

type OpenCodeMetadataIssue struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
	Source string `json:"source"`
}

// OpenCodeMetadataProjection is the effective export metadata in persisted
// catalog leaf names. Invalid present overrides stay visible and never silently
// fall back to source values. The renderer alone nests target schema fields.
type OpenCodeMetadataProjection struct {
	MergedMetadata  map[string]json.RawMessage `json:"merged_metadata"`
	Provenance      map[string]string          `json:"provenance"`
	MissingMetadata []string                   `json:"missing_metadata"`
	Issues          []OpenCodeMetadataIssue    `json:"issues"`
	LimitsValid     bool                       `json:"limits_valid"`
}

func OpenCodeMetadataLeaves() []string {
	return []string{"name", "family", "release_date", "attachment", "reasoning", "tool_call", "temperature", "modalities_input", "modalities_output", "limit_context", "limit_input", "limit_output"}
}

func ProjectOpenCodeMetadata(fact OpenCodeModelFact) OpenCodeMetadataProjection {
	result := OpenCodeMetadataProjection{
		MergedMetadata: map[string]json.RawMessage{}, Provenance: map[string]string{},
		MissingMetadata: []string{}, Issues: []OpenCodeMetadataIssue{},
	}
	for _, leaf := range OpenCodeMetadataLeaves() {
		raw, source := fact.SourceMetadata[leaf], "models_dev_source"
		if over := fact.OverrideMetadata[leaf]; presentOpenCodeValue(over) {
			raw, source = over, "models_dev_override"
		}
		if leaf == "name" && fact.DisplayName != nil && strings.TrimSpace(*fact.DisplayName) != "" {
			raw, _ = json.Marshal(strings.TrimSpace(*fact.DisplayName))
			source = "prism_display_name"
		}
		if presentOpenCodeValue(raw) {
			value, reason := validOpenCodeMetadata(leaf, raw)
			if reason == "" {
				result.MergedMetadata[leaf] = value
				result.Provenance[leaf] = source
			} else {
				result.Issues = append(result.Issues, OpenCodeMetadataIssue{leaf, reason, source})
			}
		}
		if _, ok := result.MergedMetadata[leaf]; !ok {
			if leaf == "name" {
				result.MergedMetadata[leaf], _ = json.Marshal(fact.ModelID)
				result.Provenance[leaf] = "model_id"
			} else {
				result.MissingMetadata = append(result.MissingMetadata, leaf)
				if !presentOpenCodeValue(raw) {
					result.Issues = append(result.Issues, OpenCodeMetadataIssue{leaf, "missing", "none"})
				}
			}
		}
	}
	_, contextOK := result.MergedMetadata["limit_context"]
	_, outputOK := result.MergedMetadata["limit_output"]
	result.LimitsValid = contextOK && outputOK
	if contextOK {
		context, _ := new(big.Rat).SetString(string(result.MergedMetadata["limit_context"]))
		for _, leaf := range []string{"limit_input", "limit_output"} {
			if raw, ok := result.MergedMetadata[leaf]; ok {
				limit, _ := new(big.Rat).SetString(string(raw))
				if limit.Cmp(context) > 0 {
					result.LimitsValid = false
					result.Issues = append(result.Issues, OpenCodeMetadataIssue{leaf, "exceeds_context", result.Provenance[leaf]})
				}
			}
		}
	}
	// An explicitly supplied invalid input limit invalidates the group too;
	// omitting an absent optional input limit is lossless.
	for _, issue := range result.Issues {
		if issue.Field == "limit_input" && issue.Reason != "missing" {
			result.LimitsValid = false
		}
	}
	return result
}

func presentOpenCodeValue(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}

func validOpenCodeMetadata(leaf string, raw json.RawMessage) (json.RawMessage, string) {
	value, err := decodeCanonicalJSON(raw)
	if err != nil {
		return nil, "invalid_json"
	}
	switch leaf {
	case "name", "family", "release_date":
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, "invalid_string"
		}
		value = strings.TrimSpace(text)
	case "attachment", "reasoning", "tool_call", "temperature":
		if _, ok := value.(bool); !ok {
			return nil, "invalid_boolean"
		}
	case "limit_context", "limit_input", "limit_output":
		number, ok := value.(json.Number)
		if !ok {
			return nil, "invalid_limit"
		}
		n, ok := new(big.Rat).SetString(number.String())
		if !ok || !n.IsInt() || n.Sign() < 0 || n.Cmp(new(big.Rat).SetInt64(9007199254740991)) > 0 || (leaf != "limit_input" && n.Sign() == 0) {
			return nil, "invalid_limit"
		}
	case "modalities_input", "modalities_output":
		allowed := map[string]struct{}{"text": {}, "image": {}, "audio": {}, "video": {}, "pdf": {}}
		if targetStringArray(value, leaf, allowed) != nil {
			return nil, "unsupported_modality"
		}
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "invalid_json"
	}
	return canonical, ""
}
