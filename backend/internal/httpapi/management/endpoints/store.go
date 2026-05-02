package endpoints

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type endpointRecord struct {
	ID        int
	ProfileID int
	Name      string
	BaseURL   string
	APIKey    string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func listOrderedEndpoints(ctx context.Context, exec queryExecutor, profileID int) ([]endpointRecord, error) {
	rows, err := exec.Query(ctx, `SELECT id, profile_id, name, base_url, api_key, position, created_at, updated_at FROM endpoints WHERE profile_id = $1 ORDER BY position ASC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query endpoints for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]endpointRecord, 0)
	for rows.Next() {
		record, scanErr := scanEndpointRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoints for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadEndpointRecord(ctx context.Context, exec queryExecutor, profileID int, endpointID int, forUpdate bool) (endpointRecord, bool, error) {
	query := `SELECT id, profile_id, name, base_url, api_key, position, created_at, updated_at FROM endpoints WHERE profile_id = $1 AND id = $2`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	record, err := scanEndpointRecord(exec.QueryRow(ctx, query, profileID, endpointID))
	if err == pgx.ErrNoRows {
		return endpointRecord{}, false, nil
	}
	if err != nil {
		return endpointRecord{}, false, fmt.Errorf("load endpoint %d in profile %d: %w", endpointID, profileID, err)
	}
	return record, true, nil
}

func lockProfileRow(ctx context.Context, exec queryExecutor, profileID int) error {
	if err := exec.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 FOR UPDATE`, profileID).Scan(new(int)); err != nil {
		return fmt.Errorf("lock profile %d: %w", profileID, err)
	}
	return nil
}

func ensureUniqueEndpointName(ctx context.Context, exec queryExecutor, profileID int, endpointName string, excludeID *int) error {
	query := `SELECT id FROM endpoints WHERE profile_id = $1 AND name = $2`
	args := []any{profileID, endpointName}
	if excludeID != nil {
		query += ` AND id <> $3`
		args = append(args, *excludeID)
	}
	query += ` LIMIT 1`
	var existingID int
	err := exec.QueryRow(ctx, query, args...).Scan(&existingID)
	if err == nil {
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Endpoint name '%s' already exists", endpointName)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query endpoint name availability for %q: %w", endpointName, err)
}

func nextEndpointPosition(ctx context.Context, exec queryExecutor, profileID int) (int, error) {
	var maxPosition sql.NullInt32
	if err := exec.QueryRow(ctx, `SELECT MAX(position) FROM endpoints WHERE profile_id = $1`, profileID).Scan(&maxPosition); err != nil {
		return 0, fmt.Errorf("query max endpoint position for profile %d: %w", profileID, err)
	}
	if !maxPosition.Valid {
		return 0, nil
	}
	return int(maxPosition.Int32) + 1, nil
}

func insertEndpoint(ctx context.Context, exec queryExecutor, record endpointRecord) (endpointRecord, error) {
	created, err := scanEndpointRecord(exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, profile_id, name, base_url, api_key, position, created_at, updated_at`, record.ProfileID, record.Name, record.BaseURL, record.APIKey, record.Position, record.CreatedAt, record.UpdatedAt))
	if err != nil {
		return endpointRecord{}, fmt.Errorf("insert endpoint %q: %w", record.Name, err)
	}
	return created, nil
}

func updateEndpointRecord(ctx context.Context, exec queryExecutor, record endpointRecord) (endpointRecord, error) {
	updated, err := scanEndpointRecord(exec.QueryRow(ctx, `UPDATE endpoints SET name = $2, base_url = $3, api_key = $4, position = $5, updated_at = $6 WHERE id = $1 RETURNING id, profile_id, name, base_url, api_key, position, created_at, updated_at`, record.ID, record.Name, record.BaseURL, record.APIKey, record.Position, record.UpdatedAt))
	if err != nil {
		return endpointRecord{}, fmt.Errorf("update endpoint %d: %w", record.ID, err)
	}
	return updated, nil
}

func listEndpointNames(ctx context.Context, exec queryExecutor, profileID int) (map[string]struct{}, error) {
	rows, err := exec.Query(ctx, `SELECT name FROM endpoints WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query endpoint names for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan endpoint name: %w", err)
		}
		items[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint names for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listConnectionDropdownItems(ctx context.Context, exec queryExecutor, profileID int) ([]connectionDropdownItem, error) {
	rows, err := exec.Query(ctx, `SELECT id, endpoint_id, name FROM connections WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query endpoint connection dropdown for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]connectionDropdownItem, 0)
	for rows.Next() {
		var item connectionDropdownItem
		var name sql.NullString
		if err := rows.Scan(&item.ID, &item.EndpointID, &name); err != nil {
			return nil, fmt.Errorf("scan connection dropdown item: %w", err)
		}
		item.Name = nullableStringValue(name)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection dropdown items: %w", err)
	}
	return items, nil
}

func listEndpointUsageRows(ctx context.Context, exec queryExecutor, profileID int, endpointID int) ([]endpointUsageConnection, error) {
	rows, err := exec.Query(ctx, `SELECT connections.id, connections.model_config_id, model_configs.model_id, connections.name FROM connections LEFT JOIN model_configs ON model_configs.id = connections.model_config_id WHERE connections.profile_id = $1 AND connections.endpoint_id = $2 ORDER BY connections.id ASC`, profileID, endpointID)
	if err != nil {
		return nil, fmt.Errorf("query endpoint usage rows for endpoint %d: %w", endpointID, err)
	}
	defer rows.Close()

	items := make([]endpointUsageConnection, 0)
	for rows.Next() {
		var item endpointUsageConnection
		var modelID sql.NullString
		var name sql.NullString
		if err := rows.Scan(&item.ConnectionID, &item.ModelConfigID, &modelID, &name); err != nil {
			return nil, fmt.Errorf("scan endpoint usage row: %w", err)
		}
		item.ModelID = nullableStringValue(modelID)
		item.Name = nullableStringValue(name)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint usage rows: %w", err)
	}
	return items, nil
}

func deleteEndpoint(ctx context.Context, exec queryExecutor, endpointID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM endpoints WHERE id = $1`, endpointID); err != nil {
		return fmt.Errorf("delete endpoint %d: %w", endpointID, err)
	}
	return nil
}

func persistEndpointPositions(ctx context.Context, exec queryExecutor, records []endpointRecord, currentTime time.Time) error {
	for _, record := range records {
		if _, err := exec.Exec(ctx, `UPDATE endpoints SET position = $2, updated_at = $3 WHERE id = $1`, record.ID, record.Position, record.UpdatedAt); err != nil {
			return fmt.Errorf("persist endpoint %d position: %w", record.ID, err)
		}
	}
	_ = currentTime
	return nil
}

func responseFromRecord(record endpointRecord) endpointResponse {
	return endpointResponse{
		ID:           record.ID,
		ProfileID:    record.ProfileID,
		Name:         record.Name,
		BaseURL:      record.BaseURL,
		HasAPIKey:    endpointdomain.HasAPIKey(record.APIKey),
		MaskedAPIKey: endpointdomain.MaskedAPIKey(record.APIKey),
		Position:     record.Position,
		CreatedAt:    record.CreatedAt,
		UpdatedAt:    record.UpdatedAt,
	}
}

func scanEndpointRecord(scanner interface{ Scan(...any) error }) (endpointRecord, error) {
	record := endpointRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.Name, &record.BaseURL, &record.APIKey, &record.Position, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return endpointRecord{}, err
	}
	return record, nil
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}
