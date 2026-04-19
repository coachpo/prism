package vendors

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type vendorRecord struct {
	ID                 int
	Key                string
	Name               string
	Description        *string
	IconKey            *string
	AuditEnabled       bool
	AuditCaptureBodies bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type vendorIdentityConflict struct {
	ID   int
	Key  string
	Name string
}

func listVendors(ctx context.Context, exec queryExecutor) ([]vendorRecord, error) {
	rows, err := exec.Query(ctx, `SELECT id, key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at FROM vendors ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query vendors: %w", err)
	}
	defer rows.Close()

	items := make([]vendorRecord, 0)
	for rows.Next() {
		record, scanErr := scanVendor(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vendors: %w", err)
	}
	return items, nil
}

func loadVendor(ctx context.Context, exec queryExecutor, vendorID int, forUpdate bool) (vendorRecord, bool, error) {
	query := `SELECT id, key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at FROM vendors WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`
	record, err := scanVendor(exec.QueryRow(ctx, query, vendorID))
	if err == pgx.ErrNoRows {
		return vendorRecord{}, false, nil
	}
	if err != nil {
		return vendorRecord{}, false, fmt.Errorf("load vendor %d: %w", vendorID, err)
	}
	return record, true, nil
}

func ensureVendorUniqueness(ctx context.Context, exec queryExecutor, key *string, name *string, excludeVendorID *int) error {
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 3)
	if key != nil {
		conditions = append(conditions, fmt.Sprintf("key = $%d", len(args)+1))
		args = append(args, *key)
	}
	if name != nil {
		conditions = append(conditions, fmt.Sprintf("name = $%d", len(args)+1))
		args = append(args, *name)
	}
	if len(conditions) == 0 {
		return nil
	}

	query := `SELECT id, key, name FROM vendors WHERE (` + strings.Join(conditions, ` OR `) + `)`
	if excludeVendorID != nil {
		query += fmt.Sprintf(" AND id <> $%d", len(args)+1)
		args = append(args, *excludeVendorID)
	}
	query += ` ORDER BY id ASC LIMIT 1`

	conflict, err := scanVendorIdentityConflict(exec.QueryRow(ctx, query, args...))
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query vendor uniqueness: %w", err)
	}
	if key != nil && conflict.Key == *key {
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Vendor key '%s' already exists", *key)}
	}
	if name != nil && conflict.Name == *name {
		return &domainError{StatusCode: 409, Detail: fmt.Sprintf("Vendor name '%s' already exists", *name)}
	}
	return nil
}

func listVendorModelUsage(ctx context.Context, exec queryExecutor, vendorID int) ([]vendorModelUsageItem, error) {
	rows, err := exec.Query(ctx, `SELECT model_configs.id, profiles.id, profiles.name, model_configs.model_id, model_configs.display_name, model_configs.model_type, model_configs.api_family, model_configs.is_enabled FROM model_configs JOIN profiles ON profiles.id = model_configs.profile_id WHERE model_configs.vendor_id = $1 ORDER BY profiles.id ASC, model_configs.id ASC`, vendorID)
	if err != nil {
		return nil, fmt.Errorf("query vendor model usage for vendor %d: %w", vendorID, err)
	}
	defer rows.Close()

	items := make([]vendorModelUsageItem, 0)
	for rows.Next() {
		var item vendorModelUsageItem
		var displayName sql.NullString
		if err := rows.Scan(&item.ModelConfigID, &item.ProfileID, &item.ProfileName, &item.ModelID, &displayName, &item.ModelType, &item.APIFamily, &item.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan vendor model usage: %w", err)
		}
		if displayName.Valid {
			value := displayName.String
			item.DisplayName = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vendor model usage: %w", err)
	}
	return items, nil
}

func insertVendor(ctx context.Context, tx pgx.Tx, requestBody vendorCreateRequest, now time.Time) (vendorRecord, error) {
	record, err := scanVendor(tx.QueryRow(ctx, `INSERT INTO vendors (key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at`, requestBody.Key, requestBody.Name, nullableString(requestBody.Description), nullableString(requestBody.IconKey), false, true, now, now))
	if err != nil {
		if isUniqueViolation(err, "vendors_key_key") {
			return vendorRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Vendor key '%s' already exists", requestBody.Key)}
		}
		if isUniqueViolation(err, "vendors_name_key") {
			return vendorRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Vendor name '%s' already exists", requestBody.Name)}
		}
		return vendorRecord{}, fmt.Errorf("insert vendor %q: %w", requestBody.Key, err)
	}
	return record, nil
}

func updateVendor(ctx context.Context, tx pgx.Tx, vendorID int, key any, name any, description any, iconKey any, auditEnabled bool, auditCaptureBodies bool, updatedAt time.Time) (vendorRecord, error) {
	record, err := scanVendor(tx.QueryRow(ctx, `UPDATE vendors SET key = $2, name = $3, description = $4, icon_key = $5, audit_enabled = $6, audit_capture_bodies = $7, updated_at = $8 WHERE id = $1 RETURNING id, key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at`, vendorID, key, name, description, iconKey, auditEnabled, auditCaptureBodies, updatedAt))
	if err != nil {
		if isUniqueViolation(err, "vendors_key_key") {
			if typedKey, ok := key.(string); ok {
				return vendorRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Vendor key '%s' already exists", typedKey)}
			}
		}
		if isUniqueViolation(err, "vendors_name_key") {
			if typedName, ok := name.(string); ok {
				return vendorRecord{}, &domainError{StatusCode: 409, Detail: fmt.Sprintf("Vendor name '%s' already exists", typedName)}
			}
		}
		return vendorRecord{}, fmt.Errorf("update vendor %d: %w", vendorID, err)
	}
	return record, nil
}

func deleteVendor(ctx context.Context, tx pgx.Tx, vendorID int) error {
	if _, err := tx.Exec(ctx, `DELETE FROM vendors WHERE id = $1`, vendorID); err != nil {
		return fmt.Errorf("delete vendor %d: %w", vendorID, err)
	}
	return nil
}

func scanVendor(scanner interface{ Scan(...any) error }) (vendorRecord, error) {
	var description sql.NullString
	var iconKey sql.NullString
	record := vendorRecord{}
	if err := scanner.Scan(&record.ID, &record.Key, &record.Name, &description, &iconKey, &record.AuditEnabled, &record.AuditCaptureBodies, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return vendorRecord{}, err
	}
	if description.Valid {
		value := description.String
		record.Description = &value
	}
	if iconKey.Valid {
		value := iconKey.String
		record.IconKey = &value
	}
	return record, nil
}

func scanVendorIdentityConflict(scanner interface{ Scan(...any) error }) (vendorIdentityConflict, error) {
	conflict := vendorIdentityConflict{}
	if err := scanner.Scan(&conflict.ID, &conflict.Key, &conflict.Name); err != nil {
		return vendorIdentityConflict{}, err
	}
	return conflict, nil
}
