package connections

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

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

func loadProfileEndpointRecord(ctx context.Context, exec queryExecutor, profileID int, endpointID int) (endpointRecord, bool, error) {
	record, err := scanEndpointRecord(exec.QueryRow(ctx, `SELECT id, profile_id, name, base_url, api_key, api_key_fingerprint, api_key_updated_at, config_revision, created_at, updated_at FROM endpoints WHERE profile_id = $1 AND id = $2 LIMIT 1`, profileID, endpointID))
	if err == pgx.ErrNoRows {
		return endpointRecord{}, false, nil
	}
	if err != nil {
		return endpointRecord{}, false, fmt.Errorf("load endpoint %d in profile %d: %w", endpointID, profileID, err)
	}
	return record, true, nil
}

func ensureUniqueEndpointName(ctx context.Context, exec queryExecutor, profileID int, endpointName string) error {
	var existingID int
	err := exec.QueryRow(ctx, `SELECT id FROM endpoints WHERE profile_id = $1 AND name = $2 LIMIT 1`, profileID, endpointName).Scan(&existingID)
	if err == nil {
		return &DomainError{StatusCode: 409, Detail: fmt.Sprintf("Endpoint name '%s' already exists", endpointName)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query endpoint name availability for %q: %w", endpointName, err)
}

func insertEndpoint(ctx context.Context, exec queryExecutor, record endpointRecord) (endpointRecord, error) {
	created, err := scanEndpointRecord(exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, api_key_fingerprint, api_key_updated_at, config_revision, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, profile_id, name, base_url, api_key, api_key_fingerprint, api_key_updated_at, config_revision, created_at, updated_at`, record.ProfileID, record.Name, record.BaseURL, record.APIKey, record.APIKeyFingerprint, record.APIKeyUpdatedAt, record.ConfigRevision, record.CreatedAt, record.UpdatedAt))
	if err != nil {
		return endpointRecord{}, fmt.Errorf("insert inline endpoint %q: %w", record.Name, err)
	}
	return created, nil
}

func scanEndpointRecord(scanner interface{ Scan(...any) error }) (endpointRecord, error) {
	record := endpointRecord{}
	if err := scanner.Scan(&record.ID, &record.ProfileID, &record.Name, &record.BaseURL, &record.APIKey, &record.APIKeyFingerprint, &record.APIKeyUpdatedAt, &record.ConfigRevision, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return endpointRecord{}, err
	}
	return record, nil
}
