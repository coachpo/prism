package models

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/jackc/pgx/v5"
)

type openCodeStoredMetadata struct {
	Source   map[string]json.RawMessage
	Override map[string]json.RawMessage
}

// Only persisted, target-supported leaves cross this boundary. Catalog
// coordinates, timestamps, Pi bindings and live catalog state are not export
// authority and cannot make an otherwise identical source digest stale.
func loadOpenCodeMetadata(ctx context.Context, tx pgx.Tx, profileID int, ids []int) (map[int]openCodeStoredMetadata, error) {
	rows, err := tx.Query(ctx, `SELECT b.model_config_id,
		jsonb_strip_nulls(jsonb_build_object(
			'name', b.source_name, 'family', b.source_family, 'release_date', b.source_release_date,
			'attachment', b.source_attachment, 'reasoning', b.source_reasoning,
			'tool_call', b.source_tool_call, 'temperature', b.source_temperature,
			'modalities_input', b.source_modalities_input, 'modalities_output', b.source_modalities_output,
			'limit_context', b.source_limit_context, 'limit_input', b.source_limit_input, 'limit_output', b.source_limit_output)),
		jsonb_strip_nulls(jsonb_build_object(
			'name', b.override_name, 'family', b.override_family, 'release_date', b.override_release_date,
			'attachment', b.override_attachment, 'reasoning', b.override_reasoning,
			'tool_call', b.override_tool_call, 'temperature', b.override_temperature,
			'modalities_input', b.override_modalities_input, 'modalities_output', b.override_modalities_output,
			'limit_context', b.override_limit_context, 'limit_input', b.override_limit_input, 'limit_output', b.override_limit_output))
		FROM model_catalog_bindings b JOIN model_configs m ON m.id = b.model_config_id
		WHERE m.profile_id = $1 AND b.model_config_id = ANY($2::integer[])
		ORDER BY b.model_config_id`, profileID, ids)
	if err != nil {
		return nil, fmt.Errorf("query OpenCode metadata: %w", err)
	}
	defer rows.Close()
	bindings := map[int]openCodeStoredMetadata{}
	for rows.Next() {
		var id int
		var source, override []byte
		if err := rows.Scan(&id, &source, &override); err != nil {
			return nil, fmt.Errorf("scan OpenCode metadata: %w", err)
		}
		var metadata openCodeStoredMetadata
		if err := json.Unmarshal(source, &metadata.Source); err != nil {
			return nil, fmt.Errorf("decode OpenCode source metadata: %w", err)
		}
		if err := json.Unmarshal(override, &metadata.Override); err != nil {
			return nil, fmt.Errorf("decode OpenCode override metadata: %w", err)
		}
		bindings[id] = metadata
	}
	return bindings, rows.Err()
}

func loadOpenCodeSourceFacts(ctx context.Context, tx pgx.Tx, profileID int) (modelexport.OpenCodeSourceFacts, error) {
	models, targets, graph, err := loadExportSnapshot(ctx, tx, profileID)
	if err != nil {
		return modelexport.OpenCodeSourceFacts{}, err
	}
	metadata, err := loadOpenCodeMetadata(ctx, tx, profileID, exportModelConfigIDs(models))
	if err != nil {
		return modelexport.OpenCodeSourceFacts{}, err
	}
	return buildOpenCodeSourceFacts(models, sortTargetRowsByModel(targets), graph, metadata), nil
}
