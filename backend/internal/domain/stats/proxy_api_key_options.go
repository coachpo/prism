package stats

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ListProxyAPIKeyFilterOptions returns the bounded union of configured proxy
// keys and immutable request-log snapshot identities. Cursor pagination is
// applied after the union, so search and time-window filters cannot repeat or
// drop an option between pages.
func ListProxyAPIKeyFilterOptions(ctx context.Context, exec queryExecutor, params ProxyAPIKeyFilterOptionsParams) (ProxyAPIKeyFilterOptionsResponse, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := ""
	if params.Query != nil {
		query = strings.TrimSpace(*params.Query)
	}
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}

	fromTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if params.FromTime != nil {
		fromTime = params.FromTime.UTC()
	}
	toTime := time.Now().UTC().Add(24 * time.Hour)
	if params.ToTime != nil {
		toTime = params.ToTime.UTC()
	}

	args := []any{params.ProfileID, fromTime, toTime, query}
	where := `strpos(lower(coalesce(options.resolved_name, '')), lower($4)) > 0 OR options.option_key_id::text = $4`
	if params.Cursor != nil {
		cursorName, cursorID, err := decodeProxyAPIKeyOptionCursor(strings.TrimSpace(*params.Cursor))
		if err != nil {
			return ProxyAPIKeyFilterOptionsResponse{}, &HTTPError{StatusCode: 400, Code: "proxy_api_key_cursor_invalid", Detail: "Invalid proxy API key filter cursor."}
		}
		args = append(args, cursorName, cursorID)
		where = "(" + where + ") AND (lower(coalesce(options.resolved_name, '')) > lower($5) OR (lower(coalesce(options.resolved_name, '')) = lower($5) AND options.option_key_id > $6))"
	}
	limitPlaceholder := len(args) + 1
	args = append(args, limit+1)
	querySQL := `WITH options AS (
			SELECT id AS option_key_id, name AS resolved_name, TRUE AS configured, key_prefix
			FROM proxy_api_keys
			UNION ALL
			SELECT snapshot.key_id AS option_key_id, snapshot.name_snapshot AS resolved_name, FALSE AS configured, NULL::varchar AS key_prefix
			FROM (
				SELECT DISTINCT ON (proxy_api_key_id_snapshot)
					proxy_api_key_id_snapshot AS key_id, proxy_api_key_name_snapshot AS name_snapshot
				FROM request_logs
				WHERE profile_id = $1 AND proxy_api_key_id_snapshot IS NOT NULL
					AND proxy_api_key_name_snapshot IS NOT NULL AND created_at >= $2 AND created_at <= $3
				ORDER BY proxy_api_key_id_snapshot, created_at DESC, id DESC
			) snapshot
			WHERE NOT EXISTS (SELECT 1 FROM proxy_api_keys current_key WHERE current_key.id = snapshot.key_id)
		), deduped AS (
			SELECT DISTINCT ON (option_key_id) option_key_id, resolved_name, configured, key_prefix
			FROM options
			ORDER BY option_key_id, configured DESC, resolved_name ASC NULLS LAST
		)
		SELECT option_key_id, resolved_name, configured, key_prefix
		FROM deduped AS options
		WHERE ` + where + `
		ORDER BY lower(coalesce(resolved_name, '')), option_key_id
		LIMIT $` + fmt.Sprintf("%d", limitPlaceholder)
	rows, err := exec.Query(ctx, querySQL, args...)
	if err != nil {
		return ProxyAPIKeyFilterOptionsResponse{}, fmt.Errorf("query proxy api key filter options: %w", err)
	}
	defer rows.Close()

	items := make([]ProxyAPIKeyFilterOption, 0, limit+1)
	for rows.Next() {
		var keyID int
		var name sql.NullString
		var configured bool
		var keyPrefix sql.NullString
		if err := rows.Scan(&keyID, &name, &configured, &keyPrefix); err != nil {
			return ProxyAPIKeyFilterOptionsResponse{}, fmt.Errorf("scan proxy api key filter option: %w", err)
		}
		displayName := strings.TrimSpace(stringValue(nullableString(name)))
		if displayName == "" {
			displayName = fmt.Sprintf("#%d", keyID)
		}
		item := ProxyAPIKeyFilterOption{ProxyAPIKeyID: keyID, Name: displayName, Configured: configured}
		if configured && keyPrefix.Valid {
			preview := strings.TrimSpace(keyPrefix.String)
			item.KeyPreview = &preview
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ProxyAPIKeyFilterOptionsResponse{}, fmt.Errorf("iterate proxy api key filter options: %w", err)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	response := ProxyAPIKeyFilterOptionsResponse{Items: items, ResolvedFromTime: &fromTime, ResolvedToTime: &toTime}
	if hasMore && len(items) > 0 {
		cursor := encodeProxyAPIKeyOptionCursor(items[len(items)-1].Name, items[len(items)-1].ProxyAPIKeyID)
		response.NextCursor = &cursor
	}
	if params.SelectedID != nil {
		selected, err := loadProxyAPIKeyFilterOption(ctx, exec, params.ProfileID, *params.SelectedID, fromTime, toTime)
		if err != nil {
			return ProxyAPIKeyFilterOptionsResponse{}, err
		}
		response.Selected = selected
	}
	return response, nil
}

func encodeProxyAPIKeyOptionCursor(name string, keyID int) string {
	raw, _ := json.Marshal([]any{name, keyID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProxyAPIKeyOptionCursor(encoded string) (string, int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", 0, err
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != 2 {
		return "", 0, fmt.Errorf("invalid cursor payload")
	}
	var name string
	var keyID int
	if err := json.Unmarshal(values[0], &name); err != nil || strings.TrimSpace(name) == "" {
		return "", 0, fmt.Errorf("invalid cursor name")
	}
	if err := json.Unmarshal(values[1], &keyID); err != nil || keyID <= 0 {
		return "", 0, fmt.Errorf("invalid cursor id")
	}
	return name, keyID, nil
}

func loadProxyAPIKeyFilterOption(ctx context.Context, exec queryExecutor, profileID int, keyID int, fromTime, toTime time.Time) (*ProxyAPIKeyFilterOption, error) {
	var currentName, currentPrefix sql.NullString
	if err := exec.QueryRow(ctx, `SELECT name, key_prefix FROM proxy_api_keys WHERE id = $1`, keyID).Scan(&currentName, &currentPrefix); err == nil {
		option := &ProxyAPIKeyFilterOption{ProxyAPIKeyID: keyID, Name: strings.TrimSpace(stringValue(nullableString(currentName))), Configured: true}
		if option.Name == "" {
			option.Name = fmt.Sprintf("#%d", keyID)
		}
		if currentPrefix.Valid {
			preview := strings.TrimSpace(currentPrefix.String)
			option.KeyPreview = &preview
		}
		return option, nil
	}
	var snapshotName sql.NullString
	if err := exec.QueryRow(ctx, `SELECT proxy_api_key_name_snapshot FROM request_logs WHERE profile_id = $1 AND proxy_api_key_id_snapshot = $2 AND created_at >= $3 AND created_at <= $4 AND proxy_api_key_name_snapshot IS NOT NULL ORDER BY created_at DESC, id DESC LIMIT 1`, profileID, keyID, fromTime, toTime).Scan(&snapshotName); err == nil {
		name := strings.TrimSpace(stringValue(nullableString(snapshotName)))
		if name == "" {
			name = fmt.Sprintf("#%d", keyID)
		}
		return &ProxyAPIKeyFilterOption{ProxyAPIKeyID: keyID, Name: name, Configured: false}, nil
	}
	return &ProxyAPIKeyFilterOption{ProxyAPIKeyID: keyID, Name: fmt.Sprintf("#%d", keyID), Configured: false}, nil
}
