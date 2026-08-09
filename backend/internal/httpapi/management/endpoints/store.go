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
	ID                int
	ProfileID         int
	Name              string
	BaseURL           string
	APIKey            string
	APIKeyFingerprint *string
	APIKeyUpdatedAt   *time.Time
	ConfigRevision    int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const endpointSelectColumns = `id, profile_id, name, base_url, api_key, api_key_fingerprint, api_key_updated_at, config_revision, created_at, updated_at`

// listOrderedEndpoints returns all profile endpoints in the deterministic
// display order: lower(name), name, id.
func listOrderedEndpoints(ctx context.Context, exec queryExecutor, profileID int) ([]endpointRecord, error) {
	rows, err := exec.Query(ctx, `SELECT `+endpointSelectColumns+` FROM endpoints WHERE profile_id = $1 ORDER BY lower(name) ASC, name ASC, id ASC`, profileID)
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
	query := `SELECT ` + endpointSelectColumns + ` FROM endpoints WHERE profile_id = $1 AND id = $2`
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

func insertEndpoint(ctx context.Context, exec queryExecutor, record endpointRecord) (endpointRecord, error) {
	created, err := scanEndpointRecord(exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, api_key_fingerprint, api_key_updated_at, config_revision, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+endpointSelectColumns,
		record.ProfileID, record.Name, record.BaseURL, record.APIKey, record.APIKeyFingerprint, record.APIKeyUpdatedAt, record.ConfigRevision, record.CreatedAt, record.UpdatedAt))
	if err != nil {
		return endpointRecord{}, fmt.Errorf("insert endpoint %q: %w", record.Name, err)
	}
	return created, nil
}

func updateEndpointRecord(ctx context.Context, exec queryExecutor, record endpointRecord) (endpointRecord, error) {
	updated, err := scanEndpointRecord(exec.QueryRow(ctx, `UPDATE endpoints SET name = $2, base_url = $3, api_key = $4, api_key_fingerprint = $5, api_key_updated_at = $6, config_revision = $7, updated_at = $8
		WHERE id = $1
		RETURNING `+endpointSelectColumns,
		record.ID, record.Name, record.BaseURL, record.APIKey, record.APIKeyFingerprint, record.APIKeyUpdatedAt, record.ConfigRevision, record.UpdatedAt))
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

func deleteEndpoint(ctx context.Context, exec queryExecutor, endpointID int) error {
	if _, err := exec.Exec(ctx, `DELETE FROM endpoints WHERE id = $1`, endpointID); err != nil {
		return fmt.Errorf("delete endpoint %d: %w", endpointID, err)
	}
	return nil
}

func scanEndpointRecord(scanner interface{ Scan(...any) error }) (endpointRecord, error) {
	record := endpointRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.Name, &record.BaseURL, &record.APIKey, &record.APIKeyFingerprint, &record.APIKeyUpdatedAt, &record.ConfigRevision, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return endpointRecord{}, err
	}
	return record, nil
}

func responseFromRecord(record endpointRecord) endpointResponse {
	return endpointResponse{
		ID:                record.ID,
		ProfileID:         record.ProfileID,
		Name:              record.Name,
		BaseURL:           record.BaseURL,
		HasAPIKey:         endpointdomain.HasAPIKey(record.APIKey),
		APIKeyFingerprint: record.APIKeyFingerprint,
		APIKeyUpdatedAt:   record.APIKeyUpdatedAt,
		ConfigRevision:    record.ConfigRevision,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}
