package modelexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

const OpenCodeTargetVersion = "1.18.27"

// OpenCodeSourceFacts contains only persisted facts consumed by OpenCode
// export. Neither live catalogs nor Pi bindings are dependencies.
type OpenCodeSourceFacts struct {
	TargetVersion string              `json:"target_version"`
	Models        []OpenCodeModelFact `json:"models"`
}

type OpenCodeModelFact struct {
	ModelConfigID         int                        `json:"model_config_id"`
	ModelID               string                     `json:"model_id"`
	APIFamily             string                     `json:"api_family"`
	DisplayName           *string                    `json:"display_name,omitempty"`
	IsEnabled             bool                       `json:"is_enabled"`
	Selectable            bool                       `json:"selectable"`
	UnselectableReason    *string                    `json:"unselectable_reason,omitempty"`
	OpenAIAcceptedFormat  *string                    `json:"openai_accepted_format,omitempty"`
	OpenAIImageOperations *string                    `json:"openai_image_operations,omitempty"`
	SourceMetadata        map[string]json.RawMessage `json:"-"`
	OverrideMetadata      map[string]json.RawMessage `json:"-"`
	Targets               []TargetFact               `json:"targets"`
}

// ComputeOpenCodeSourceDigest freezes the actual safe projection and its
// provenance, not unrelated catalog fields, binding timestamps or coordinates.
// Changes to shadowed source values cannot change the export result.
func ComputeOpenCodeSourceDigest(facts OpenCodeSourceFacts) (string, error) {
	type row struct {
		OpenCodeModelFact
		Metadata OpenCodeMetadataProjection `json:"metadata"`
	}
	rows := make([]row, 0, len(facts.Models))
	for _, fact := range facts.Models {
		fact.Targets = append([]TargetFact(nil), fact.Targets...)
		sort.Slice(fact.Targets, func(i, j int) bool { return fact.Targets[i].TerminalTargetID < fact.Targets[j].TerminalTargetID })
		rows = append(rows, row{fact, ProjectOpenCodeMetadata(fact)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ModelConfigID < rows[j].ModelConfigID })
	canonical, err := CanonicalJSON(struct {
		TargetVersion string `json:"target_version"`
		Models        []row  `json:"models"`
	}{facts.TargetVersion, rows})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func NormalizeOpenCodeSelection(ids []int, facts OpenCodeSourceFacts) ([]int, error) {
	if len(ids) == 0 {
		return nil, errors.New("model_config_ids must not be empty")
	}
	byID := make(map[int]OpenCodeModelFact, len(facts.Models))
	for _, fact := range facts.Models {
		byID[fact.ModelConfigID] = fact
	}
	seen := map[int]bool{}
	selected := []int{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		fact, ok := byID[id]
		reason := ""
		switch {
		case !ok:
			reason = "not_found_in_default_profile"
		case !fact.IsEnabled || !fact.Selectable:
			reason = "unselectable"
			if fact.UnselectableReason != nil {
				reason = *fact.UnselectableReason
			}
		case OpenCodeSDKForModel(fact.APIFamily, fact.OpenAIAcceptedFormat) == "":
			reason = "unsupported_api_family"
		case !ProjectOpenCodeMetadata(fact).LimitsValid:
			reason = "invalid_metadata_limits"
		}
		if reason != "" {
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: reason}
		}
		selected = append(selected, id)
	}
	sort.Ints(selected)
	return selected, nil
}
