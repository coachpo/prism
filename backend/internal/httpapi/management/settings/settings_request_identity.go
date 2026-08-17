package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func canonicalHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func canonicalRequestHashForBody(request putLogRetentionSettingsRequest) string {
	return canonicalJSONHash(struct {
		OperationID        string            `json:"operation_id"`
		ExpectedRevision   string            `json:"expected_revision"`
		Policies           retentionPolicies `json:"policies"`
		PreflightTokenHash string            `json:"preflight_token_hash,omitempty"`
		Confirmation       string            `json:"confirmation,omitempty"`
	}{
		OperationID:      request.OperationID,
		ExpectedRevision: request.ExpectedRevision,
		Policies:         request.Policies,
		PreflightTokenHash: func() string {
			if request.PreflightToken == nil || strings.TrimSpace(*request.PreflightToken) == "" {
				return ""
			}
			return hashToken(*request.PreflightToken)
		}(),
		Confirmation: func() string {
			if request.Confirmation == nil {
				return ""
			}
			return request.Confirmation.Keyword
		}(),
	})
}

func canonicalArchiveHash(request archiveRetentionOwnerDriftRequest) string {
	return canonicalHash(request.OperationID, request.ExpectedRevision, request.ExpectedInventoryGeneration, fmt.Sprintf("%v", request.Heads))
}

func canonicalManualJobHash(request createManualRetentionJobRequest) string {
	return canonicalJSONHash(struct {
		OperationID   string `json:"operation_id"`
		PreflightHash string `json:"preflight_token_hash"`
		Confirmation  string `json:"confirmation"`
	}{
		OperationID:   request.OperationID,
		PreflightHash: hashToken(request.PreflightToken),
		Confirmation:  request.Confirmation.Keyword,
	})
}

func canonicalPreflightHash(raw struct {
	Kind                     string                  `json:"kind"`
	OperationID              string                  `json:"operation_id"`
	PreflightAttemptID       string                  `json:"preflight_attempt_id"`
	ExpectedSettingsRevision string                  `json:"expected_settings_revision"`
	Policies                 *retentionPolicies      `json:"policies"`
	Dataset                  string                  `json:"dataset"`
	Selection                *manualCleanupSelection `json:"selection"`
}) string {
	if raw.Kind == "policy_change" && raw.Policies != nil {
		return canonicalPolicyBindingHash(raw.OperationID, raw.ExpectedSettingsRevision, *raw.Policies)
	}
	return canonicalJSONHash(struct {
		Kind        string                  `json:"kind"`
		OperationID string                  `json:"operation_id"`
		Dataset     string                  `json:"dataset"`
		Selection   *manualCleanupSelection `json:"selection"`
	}{
		Kind: raw.Kind, OperationID: raw.OperationID, Dataset: raw.Dataset, Selection: raw.Selection,
	})
}

func canonicalPolicyBindingHash(operationID string, expectedRevision string, policies retentionPolicies) string {
	return canonicalJSONHash(struct {
		Kind             string            `json:"kind"`
		OperationID      string            `json:"operation_id"`
		ExpectedRevision string            `json:"expected_settings_revision"`
		Policies         retentionPolicies `json:"policies"`
	}{
		Kind: "policy_change", OperationID: operationID, ExpectedRevision: expectedRevision, Policies: policies,
	})
}

func canonicalJSONHash(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return canonicalHash(fmt.Sprintf("marshal-error:%T", value))
	}
	return canonicalHash(string(raw))
}

func canonicalOwnerSemanticSnapshotHash(value any) string {
	// The owner snapshot is persisted verbatim for operator evidence. Its
	// coverage revision/hash, materialization cut, bounds, gaps and timestamps
	// are preview evidence, not a second semantic fence: ordinary append-only
	// facts may advance those values without changing the sealed predicate.
	// Semantic generation/state fields remain in the comparison below, so
	// policy/floor/epoch/fence/purge/materializer transitions still require a
	// fresh preflight.
	return canonicalJSONHash(stripOwnerPreviewEvidence(value))
}

func ownerSnapshotGeneration(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if value, ok := object["coverage_revision"].(string); ok {
		return value
	}
	if source, ok := object["retention_source"].(map[string]any); ok {
		if value, ok := source["source_revision"].(string); ok {
			return value
		}
	}
	return ""
}

func stripOwnerPreviewEvidence(value any) any {
	switch item := value.(type) {
	case map[string]any:
		copyObject := make(map[string]any, len(item))
		for key, child := range item {
			switch key {
			case "generated_at", "updated_at", "coverage_revision", "coverage_hash", "materialization_cut", "earliest", "latest", "gaps":
				continue
			}
			if key == "actual_coverage" {
				// Keep the owner semantic readiness labels, but not mutable
				// append-time bounds or the evidence manifest itself.
				if coverage, ok := child.(map[string]any); ok {
					semanticCoverage := map[string]any{}
					for coverageKey, coverageValue := range coverage {
						switch coverageKey {
						case "complete", "freshness", "precision", "source", "gap_reason":
							semanticCoverage[coverageKey] = stripOwnerPreviewEvidence(coverageValue)
						}
					}
					copyObject[key] = semanticCoverage
					continue
				}
			}
			if key == "storage_fact_evidence" {
				// An unavailable bounded fact set is explicitly permitted. A bound
				// generation is semantic evidence and remains comparable.
				if evidence, ok := child.(map[string]any); ok {
					bounded := map[string]any{"state": evidence["state"]}
					if evidence["state"] == "bound" {
						bounded["generation"] = evidence["generation"]
					}
					copyObject[key] = bounded
					continue
				}
			}
			copyObject[key] = stripOwnerPreviewEvidence(child)
		}
		return copyObject
	case []any:
		copyArray := make([]any, len(item))
		for index, child := range item {
			copyArray[index] = stripOwnerPreviewEvidence(child)
		}
		return copyArray
	default:
		return value
	}
}
