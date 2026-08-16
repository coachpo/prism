package endpoints

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	referenceKindOwned  = "owned_terminal_target"
	referenceKindOrphan = "orphan_connection"
)

const referenceCursorDomain = "prism:endpoint-reference-cursor:v1"

// referenceRow is one scanned direct-reference projection row. Owner fields
// are null for orphan connections (LEFT JOIN).
type referenceRow struct {
	ConnectionID              int
	ConnectionName            *string
	APIFamily                 string
	ConnectionIsActive        bool
	OpenAITextCapability      *string
	OpenAIImageCapability     *string
	PricingTemplateID         *int
	OwnerMatID                *int
	OwnerMatPosition          *int
	OwnerMatEnabled           *bool
	OwnerModelConfigID        *int
	OwnerModelID              *string
	OwnerDisplayName          *string
	OwnerModelEnabled         *bool
	OwnerOpenAIAcceptedFormat *string
	PricingTemplate           *referencePricingRow
}

type referencePricingRow struct {
	ID      int
	Name    string
	Version int
}

// canonicalReferenceSet is the full ordered direct-reference projection for
// one Endpoint, plus its derived summary and snapshot hash.
type canonicalReferenceSet struct {
	EndpointID int
	Summary    endpointReferenceSummary
	Items      []endpointReferenceItem
	Hash       string
	OrderKeys  []referenceOrderKey
}

type referenceOrderKey struct {
	Rank        int
	LowerName   *string
	ModelID     *string
	ModelConfig *int
	Position    *int
	Connection  int
}

func (key referenceOrderKey) encode() string {
	return fmt.Sprintf("%d|%s|%s|%s|%s|%d",
		key.Rank,
		nullableStringForCursor(key.LowerName),
		nullableStringForCursor(key.ModelID),
		nullableIntForCursor(key.ModelConfig),
		nullableIntForCursor(key.Position),
		key.Connection,
	)
}

func nullableStringForCursor(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableIntForCursor(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

// loadCanonicalReferenceSets executes the shared canonical query for the given
// Endpoint IDs in the effective profile and derives full ordered projections,
// summaries and snapshot hashes. It is a pure read; it never updates
// timestamps or cache generations.
func loadCanonicalReferenceSets(ctx context.Context, exec queryExecutor, profileID int, endpointIDs []int) (map[int]canonicalReferenceSet, error) {
	rows, err := exec.Query(ctx, canonicalReferenceQuery, profileID, int32ArrayArg(endpointIDs))
	if err != nil {
		return nil, fmt.Errorf("query endpoint references for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	type scannedRow struct {
		endpointID int
		row        referenceRow
	}
	scanned := make([]scannedRow, 0)
	for rows.Next() {
		var endpointID int
		var row referenceRow
		var connectionName sql.NullString
		var openAITextCapability sql.NullString
		var openAIImageCapability sql.NullString
		var pricingTemplateID sql.NullInt32
		var matID sql.NullInt32
		var matPosition sql.NullInt32
		var matEnabled sql.NullBool
		var modelConfigID sql.NullInt32
		var modelID sql.NullString
		var displayName sql.NullString
		var modelEnabled sql.NullBool
		var openAIAcceptedFormat sql.NullString
		var templateID sql.NullInt32
		var templateName sql.NullString
		var templateVersion sql.NullInt32
		if err := rows.Scan(
			&endpointID,
			&row.ConnectionID,
			&connectionName,
			&row.APIFamily,
			&row.ConnectionIsActive,
			&openAITextCapability,
			&openAIImageCapability,
			&pricingTemplateID,
			&matID,
			&matPosition,
			&matEnabled,
			&modelConfigID,
			&modelID,
			&displayName,
			&modelEnabled,
			&openAIAcceptedFormat,
			&templateID,
			&templateName,
			&templateVersion,
		); err != nil {
			return nil, fmt.Errorf("scan endpoint reference row: %w", err)
		}
		row.ConnectionName = nullableStringValue(connectionName)
		row.OpenAITextCapability = nullableStringValue(openAITextCapability)
		row.OpenAIImageCapability = nullableStringValue(openAIImageCapability)
		row.PricingTemplateID = nullableInt32(pricingTemplateID)
		row.OwnerMatID = nullableInt32(matID)
		row.OwnerMatPosition = nullableInt32(matPosition)
		if matEnabled.Valid {
			value := matEnabled.Bool
			row.OwnerMatEnabled = &value
		}
		row.OwnerModelConfigID = nullableInt32(modelConfigID)
		row.OwnerModelID = nullableStringValue(modelID)
		row.OwnerDisplayName = nullableStringValue(displayName)
		if modelEnabled.Valid {
			value := modelEnabled.Bool
			row.OwnerModelEnabled = &value
		}
		row.OwnerOpenAIAcceptedFormat = nullableStringValue(openAIAcceptedFormat)
		if templateID.Valid {
			row.PricingTemplate = &referencePricingRow{ID: int(templateID.Int32), Name: templateName.String, Version: int(templateVersion.Int32)}
		}
		scanned = append(scanned, scannedRow{endpointID: endpointID, row: row})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint references for profile %d: %w", profileID, err)
	}

	// Duplicate-owner corruption detection: a Connection must have at most one
	// owner row. Any violation fails the whole read closed with typed 409.
	ownerCounts := map[int]int{}
	for _, entry := range scanned {
		if entry.row.OwnerMatID != nil {
			ownerCounts[entry.row.ConnectionID]++
		}
	}
	corruptConnectionIDs := make([]int, 0)
	for connectionID, count := range ownerCounts {
		if count > 1 {
			corruptConnectionIDs = append(corruptConnectionIDs, connectionID)
		}
	}
	if len(corruptConnectionIDs) > 0 {
		sort.Ints(corruptConnectionIDs)
		return nil, &referenceIntegrityError{EndpointIDs: endpointIDs, ConnectionIDs: corruptConnectionIDs}
	}

	sets := make(map[int]canonicalReferenceSet, len(endpointIDs))
	byEndpoint := map[int][]referenceRow{}
	for _, entry := range scanned {
		byEndpoint[entry.endpointID] = append(byEndpoint[entry.endpointID], entry.row)
	}
	for _, endpointID := range endpointIDs {
		rows := byEndpoint[endpointID]
		if rows == nil {
			rows = []referenceRow{}
		}
		sets[endpointID] = deriveCanonicalSet(endpointID, rows)
	}
	return sets, nil
}

const canonicalReferenceQuery = `
SELECT
	c.endpoint_id,
	c.id,
	c.name,
	c.api_family,
	c.is_active,
	c.openai_text_capability,
	c.openai_image_capability,
	c.pricing_template_id,
	mat.id,
	mat.position,
	mat.is_enabled,
	m.id,
	m.model_id,
	m.display_name,
	m.is_enabled,
	m.openai_accepted_format,
	p.id,
	p.name,
	revisions.version
FROM connections c
LEFT JOIN model_access_targets mat
	ON mat.profile_id = c.profile_id
	AND mat.target_type = 'connection'
	AND mat.target_connection_id = c.id
LEFT JOIN model_configs m
	ON m.profile_id = c.profile_id
	AND m.id = mat.source_model_config_id
LEFT JOIN pricing_templates p
	ON p.profile_id = c.profile_id
	AND p.id = c.pricing_template_id
LEFT JOIN pricing_template_revisions revisions
	ON revisions.id = p.current_revision_id
WHERE c.profile_id = $1
	AND c.endpoint_id = ANY($2)
ORDER BY c.endpoint_id ASC, c.id ASC`

// deriveCanonicalSet builds the ordered items, summary, order keys and
// snapshot hash for one Endpoint from its scanned rows.
func deriveCanonicalSet(endpointID int, rows []referenceRow) canonicalReferenceSet {
	items := make([]endpointReferenceItem, 0, len(rows))
	keys := make([]referenceOrderKey, 0, len(rows))
	modelIDs := map[int]struct{}{}
	enabledCount := 0
	orphanCount := 0

	for _, row := range rows {
		item := endpointReferenceItem{
			Kind:                  referenceKindOwned,
			ConnectionID:          row.ConnectionID,
			TerminalTargetID:      row.ConnectionID,
			TerminalTargetName:    row.ConnectionName,
			APIFamily:             row.APIFamily,
			ConnectionIsActive:    row.ConnectionIsActive,
			OpenAITextCapability:  row.OpenAITextCapability,
			OpenAIImageCapability: row.OpenAIImageCapability,
		}
		lowerName := (*string)(nil)
		modelID := (*string)(nil)
		modelConfig := (*int)(nil)
		position := (*int)(nil)
		reasons := make([]string, 0, 4)

		if row.OwnerMatID == nil {
			item.Kind = referenceKindOrphan
			reasons = append(reasons, "orphaned")
			orphanCount++
		} else {
			item.AccessTarget = &endpointReferenceAccessTarget{ID: *row.OwnerMatID, Position: *row.OwnerMatPosition, IsEnabled: *row.OwnerMatEnabled}
			item.OwnerModel = &endpointReferenceOwnerModel{
				ID:                   *row.OwnerModelConfigID,
				ModelID:              *row.OwnerModelID,
				DisplayName:          row.OwnerDisplayName,
				IsEnabled:            *row.OwnerModelEnabled,
				OpenAIAcceptedFormat: row.OwnerOpenAIAcceptedFormat,
			}
			modelIDs[*row.OwnerModelConfigID] = struct{}{}
			lowered := strings.ToLower(displayNameOrModelID(row.OwnerDisplayName, row.OwnerModelID))
			lowerName = &lowered
			modelID = row.OwnerModelID
			modelConfig = row.OwnerModelConfigID
			position = row.OwnerMatPosition

			if !*row.OwnerModelEnabled {
				reasons = append(reasons, "model_disabled")
			}
			if !*row.OwnerMatEnabled {
				reasons = append(reasons, "access_target_disabled")
			}
			if !row.ConnectionIsActive {
				reasons = append(reasons, "connection_inactive")
			}
			if pricingIntegrityBroken(row) {
				reasons = append(reasons, "configuration_integrity_error")
			}
			if *row.OwnerModelEnabled && *row.OwnerMatEnabled && row.ConnectionIsActive && !pricingIntegrityBroken(row) {
				enabledCount++
			}
		}

		if row.PricingTemplate != nil {
			item.PricingTemplate = &endpointReferencePricingTemplate{
				ID:             row.PricingTemplate.ID,
				Name:           row.PricingTemplate.Name,
				CurrentVersion: row.PricingTemplate.Version,
			}
		}
		item.Enabled = item.Kind == referenceKindOwned && len(reasons) == 0
		item.InactiveReasons = reasons

		items = append(items, item)
		keys = append(keys, referenceOrderKey{
			Rank:        ownedOrphanRank(item.Kind),
			LowerName:   lowerName,
			ModelID:     modelID,
			ModelConfig: modelConfig,
			Position:    position,
			Connection:  row.ConnectionID,
		})
	}

	// Sort the projection and its cursor key as one pair. Sorting the two
	// slices independently (or sorting items with a comparator that indexes an
	// unswapped key slice) can leave page items and cursor boundaries out of
	// sync, which would skip or duplicate blockers across pages.
	type orderedReference struct {
		item endpointReferenceItem
		key  referenceOrderKey
	}
	ordered := make([]orderedReference, len(items))
	for index := range items {
		ordered[index] = orderedReference{item: items[index], key: keys[index]}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return orderKeyLess(ordered[i].key, ordered[j].key) })
	for index := range ordered {
		items[index] = ordered[index].item
		keys[index] = ordered[index].key
	}

	summary := endpointReferenceSummary{
		DirectReferenceCount:  len(items),
		ReferencingModelCount: len(modelIDs),
		EnabledReferenceCount: enabledCount,
		OrphanReferenceCount:  orphanCount,
	}

	hash := snapshotHash(endpointID, summary, items)
	return canonicalReferenceSet{EndpointID: endpointID, Summary: summary, Items: items, Hash: hash, OrderKeys: keys}
}

func displayNameOrModelID(displayName *string, modelID *string) string {
	if displayName != nil && strings.TrimSpace(*displayName) != "" {
		return *displayName
	}
	if modelID != nil {
		return *modelID
	}
	return ""
}

func ownedOrphanRank(kind string) int {
	if kind == referenceKindOwned {
		return 0
	}
	return 1
}

func orderKeyLess(left referenceOrderKey, right referenceOrderKey) bool {
	if left.Rank != right.Rank {
		return left.Rank < right.Rank
	}
	if compareNullableString(left.LowerName, right.LowerName) != 0 {
		return compareNullableString(left.LowerName, right.LowerName) < 0
	}
	if compareNullableString(left.ModelID, right.ModelID) != 0 {
		return compareNullableString(left.ModelID, right.ModelID) < 0
	}
	if compareNullableInt(left.ModelConfig, right.ModelConfig) != 0 {
		return compareNullableInt(left.ModelConfig, right.ModelConfig) < 0
	}
	if compareNullableInt(left.Position, right.Position) != 0 {
		return compareNullableInt(left.Position, right.Position) < 0
	}
	return left.Connection < right.Connection
}

func compareNullableString(left *string, right *string) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	return strings.Compare(*left, *right)
}

func compareNullableInt(left *int, right *int) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}
	return 0
}

// pricingIntegrityBroken reports a non-null connection pricing identity whose
// template is missing or cross-profile (the LEFT JOIN already scopes to the
// profile, so a missing row means missing/cross-profile). In the current
// schema the template version is the immutable revision identity; the revision
// table projection is a future Pricing-final schema concern.
func pricingIntegrityBroken(row referenceRow) bool {
	return row.PricingTemplateID != nil && row.PricingTemplate == nil
}

// snapshotHash derives the canonical snapshot identity: an ordered encoding of
// every displayed reference projection plus the four summary counts. Secrets
// and volatile non-DTO fields are excluded.
func snapshotHash(endpointID int, summary endpointReferenceSummary, items []endpointReferenceItem) string {
	hasher := sha256.New()
	fmt.Fprintf(hasher, "ep:%d;d:%d;m:%d;e:%d;o:%d\n", endpointID, summary.DirectReferenceCount, summary.ReferencingModelCount, summary.EnabledReferenceCount, summary.OrphanReferenceCount)
	for _, item := range items {
		fmt.Fprintf(hasher, "c:%d;k:%s;a:%d;f:%s;n:%s;", item.ConnectionID, item.Kind, boolInt(item.ConnectionIsActive), item.APIFamily, nullableStringForHash(item.TerminalTargetName))
		if item.AccessTarget != nil {
			fmt.Fprintf(hasher, "t:%d:%d:%d;", item.AccessTarget.ID, item.AccessTarget.Position, boolInt(item.AccessTarget.IsEnabled))
		} else {
			hasher.Write([]byte("t:;"))
		}
		if item.OwnerModel != nil {
			fmt.Fprintf(hasher, "mo:%d:%s:%s:%d:%s;", item.OwnerModel.ID, item.OwnerModel.ModelID, nullableStringForHash(item.OwnerModel.DisplayName), boolInt(item.OwnerModel.IsEnabled), nullableStringForHash(item.OwnerModel.OpenAIAcceptedFormat))
		} else {
			hasher.Write([]byte("mo:;"))
		}
		fmt.Fprintf(hasher, "oc:%s;", nullableStringForHash(item.OpenAITextCapability))
		fmt.Fprintf(hasher, "oi:%s;", nullableStringForHash(item.OpenAIImageCapability))
		if item.PricingTemplate != nil {
			fmt.Fprintf(hasher, "p:%d:%s:%d;", item.PricingTemplate.ID, item.PricingTemplate.Name, item.PricingTemplate.CurrentVersion)
		} else {
			hasher.Write([]byte("p:;"))
		}
		fmt.Fprintf(hasher, "en:%d;r:%s\n", boolInt(item.Enabled), strings.Join(item.InactiveReasons, ","))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableStringForHash(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

// referenceCursor is the signed opaque pagination token. It binds the page
// limit, profile, Endpoint and snapshot hash so a continuation can never mix
// snapshots.
type referenceCursor struct {
	Version      int    `json:"v"`
	ProfileID    int    `json:"profile"`
	EndpointID   int    `json:"endpoint"`
	Limit        int    `json:"limit"`
	SnapshotHash string `json:"hash"`
	LastKey      string `json:"last_key"`
}

func encodeReferenceCursor(cursor referenceCursor, secretEncryptionKey string) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal reference cursor: %w", err)
	}
	signature := hmac.New(sha256.New, []byte(referenceCursorDomain+":"+secretEncryptionKey))
	signature.Write(payload)
	signed := append(payload, signature.Sum(nil)...)
	return base64.URLEncoding.EncodeToString(signed), nil
}

func decodeReferenceCursor(raw string, secretEncryptionKey string) (referenceCursor, error) {
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return referenceCursor{}, fmt.Errorf("decode reference cursor: %w", err)
	}
	if len(decoded) < sha256.Size+1 {
		return referenceCursor{}, fmt.Errorf("reference cursor is too short")
	}
	payload := decoded[:len(decoded)-sha256.Size]
	signature := decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, []byte(referenceCursorDomain+":"+secretEncryptionKey))
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return referenceCursor{}, fmt.Errorf("reference cursor signature is invalid")
	}
	var cursor referenceCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return referenceCursor{}, fmt.Errorf("parse reference cursor: %w", err)
	}
	if cursor.Version != 1 {
		return referenceCursor{}, fmt.Errorf("reference cursor version is unsupported")
	}
	return cursor, nil
}

func referenceIntegrityErrorResponse(endpointIDs []int, connectionIDs []int) *domainError {
	return &domainError{
		StatusCode: 409,
		Detail: map[string]any{
			"code":                    "reference_integrity_error",
			"message":                 "Multiple owners detected for direct references; delete and reference reads fail closed",
			"endpoint_ids":            endpointIDs,
			"affected_connection_ids": connectionIDs,
		},
	}
}

type referenceIntegrityError struct {
	EndpointIDs   []int
	ConnectionIDs []int
}

func (err *referenceIntegrityError) Error() string {
	return fmt.Sprintf("reference integrity error for endpoints %v connections %v", err.EndpointIDs, err.ConnectionIDs)
}

func int32ArrayArg(values []int) []int32 {
	converted := make([]int32, 0, len(values))
	for _, value := range values {
		converted = append(converted, int32(value))
	}
	return converted
}
