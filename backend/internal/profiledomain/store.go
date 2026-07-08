package profiledomain

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type QueryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Profile struct {
	ID          int
	Name        string
	Description *string
	IsActive    bool
	IsDefault   bool
	IsEditable  bool
	Version     int
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func CountNonDeletedProfiles(ctx context.Context, exec QueryExecutor) (int, error) {
	var count int
	if err := exec.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM profiles WHERE deleted_at IS NULL`,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count non-deleted profiles: %w", err)
	}
	return count, nil
}

func ListNonDeletedProfiles(ctx context.Context, exec QueryExecutor) ([]Profile, error) {
	rows, err := exec.Query(
		ctx,
		`SELECT id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at
		FROM profiles
		WHERE deleted_at IS NULL
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query non-deleted profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]Profile, 0)
	for rows.Next() {
		profile, scanErr := scanProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate non-deleted profiles: %w", err)
	}
	return profiles, nil
}

func LoadNonDeletedProfile(ctx context.Context, exec QueryExecutor, profileID int) (Profile, bool, error) {
	return loadProfileByID(ctx, exec, profileID, false)
}

func LoadNonDeletedProfileForUpdate(ctx context.Context, exec QueryExecutor, profileID int) (Profile, bool, error) {
	return loadProfileByID(ctx, exec, profileID, true)
}

func LoadActiveProfile(ctx context.Context, exec QueryExecutor) (Profile, bool, error) {
	return loadActiveProfile(ctx, exec, false)
}

func LoadActiveProfileForUpdate(ctx context.Context, exec QueryExecutor) (Profile, bool, error) {
	return loadActiveProfile(ctx, exec, true)
}

func EnsureProfileNameAvailable(ctx context.Context, exec QueryExecutor, profileName string, excludeID *int) error {
	query := `SELECT id FROM profiles WHERE name = $1`
	args := []any{profileName}
	if excludeID != nil {
		query += ` AND id <> $2`
		args = append(args, *excludeID)
	}
	query += ` LIMIT 1`

	var existingID int
	err := exec.QueryRow(ctx, query, args...).Scan(&existingID)
	if err == nil {
		return &HTTPError{StatusCode: 409, Detail: fmt.Sprintf("Profile with name '%s' already exists", profileName)}
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return fmt.Errorf("query profile name availability: %w", err)
}

func EnsureInvariants(ctx context.Context, exec QueryExecutor, now func() time.Time) (Profile, error) {
	currentTime := time.Now().UTC()
	if now != nil {
		currentTime = now().UTC()
	}

	defaultProfile, defaultFound, err := loadDefaultProfile(ctx, exec)
	if err != nil {
		return Profile{}, err
	}
	if defaultFound {
		changed := false
		if defaultProfile.DeletedAt != nil {
			defaultProfile.DeletedAt = nil
			changed = true
		}
		if defaultProfile.Name != DefaultProfileName {
			defaultProfile.Name = DefaultProfileName
			changed = true
		}
		if !defaultProfile.IsDefault {
			defaultProfile.IsDefault = true
			changed = true
		}
		if !defaultProfile.IsEditable {
			defaultProfile.IsEditable = true
			changed = true
		}
		if changed {
			defaultProfile.Version++
			if _, err := exec.Exec(
				ctx,
				`UPDATE profiles
				SET name = $2, is_default = $3, is_editable = $4, deleted_at = $5, version = $6, updated_at = $7
				WHERE id = $1`,
				defaultProfile.ID,
				defaultProfile.Name,
				defaultProfile.IsDefault,
				defaultProfile.IsEditable,
				nil,
				defaultProfile.Version,
				currentTime,
			); err != nil {
				return Profile{}, fmt.Errorf("repair default profile: %w", err)
			}
		}
	} else {
		defaultProfile, err = insertDefaultProfile(ctx, exec, currentTime)
		if err != nil {
			return Profile{}, err
		}
	}

	activeProfile, activeFound, err := loadActiveProfile(ctx, exec, false)
	if err != nil {
		return Profile{}, err
	}
	if !activeFound {
		if !defaultProfile.IsActive {
			defaultProfile.Version++
			if _, err := exec.Exec(
				ctx,
				`UPDATE profiles
				SET is_active = $2, version = $3, updated_at = $4
				WHERE id = $1`,
				defaultProfile.ID,
				true,
				defaultProfile.Version,
				currentTime,
			); err != nil {
				return Profile{}, fmt.Errorf("activate default profile: %w", err)
			}
			defaultProfile.IsActive = true
			defaultProfile.UpdatedAt = currentTime
		}
		return defaultProfile, nil
	}

	if activeProfile.ID != defaultProfile.ID && defaultProfile.IsActive {
		defaultProfile.Version++
		if _, err := exec.Exec(
			ctx,
			`UPDATE profiles
			SET is_active = $2, version = $3, updated_at = $4
			WHERE id = $1`,
			defaultProfile.ID,
			false,
			defaultProfile.Version,
			currentTime,
		); err != nil {
			return Profile{}, fmt.Errorf("deactivate default profile: %w", err)
		}
	}

	return activeProfile, nil
}

func ModelExists(ctx context.Context, exec QueryExecutor, profileID int, modelID string) (bool, error) {
	var existingID int
	err := exec.QueryRow(
		ctx,
		`SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2 AND is_enabled = TRUE ORDER BY id ASC LIMIT 1`,
		profileID,
		modelID,
	).Scan(&existingID)
	if err == nil {
		return true, nil
	}
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("query profile model %q: %w", modelID, err)
}

func loadProfileByID(ctx context.Context, exec QueryExecutor, profileID int, forUpdate bool) (Profile, bool, error) {
	query := `SELECT id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at
		FROM profiles
		WHERE id = $1 AND deleted_at IS NULL`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`

	profile, err := scanProfile(exec.QueryRow(ctx, query, profileID))
	if err == pgx.ErrNoRows {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("load profile %d: %w", profileID, err)
	}
	return profile, true, nil
}

func loadActiveProfile(ctx context.Context, exec QueryExecutor, forUpdate bool) (Profile, bool, error) {
	query := `SELECT id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at
		FROM profiles
		WHERE is_active = TRUE AND deleted_at IS NULL
		ORDER BY id ASC`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	query += ` LIMIT 1`

	profile, err := scanProfile(exec.QueryRow(ctx, query))
	if err == pgx.ErrNoRows {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("load current profile: %w", err)
	}
	return profile, true, nil
}

func loadDefaultProfile(ctx context.Context, exec QueryExecutor) (Profile, bool, error) {
	profile, err := scanProfile(exec.QueryRow(
		ctx,
		`SELECT id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at
		FROM profiles
		WHERE is_default = TRUE
		ORDER BY id ASC
		LIMIT 1`,
	))
	if err == pgx.ErrNoRows {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, fmt.Errorf("load default profile: %w", err)
	}
	return profile, true, nil
}

func insertDefaultProfile(ctx context.Context, exec QueryExecutor, currentTime time.Time) (Profile, error) {
	profile, err := scanProfile(exec.QueryRow(
		ctx,
		`INSERT INTO profiles (
			name,
			description,
			is_active,
			is_default,
			is_editable,
			version,
			deleted_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at`,
		DefaultProfileName,
		DefaultProfileDescription,
		false,
		true,
		true,
		0,
		nil,
		currentTime,
		currentTime,
	))
	if err != nil {
		return Profile{}, fmt.Errorf("insert default profile: %w", err)
	}
	return profile, nil
}

func scanProfile(scanner interface{ Scan(...any) error }) (Profile, error) {
	var description sql.NullString
	var deletedAt sql.NullTime
	profile := Profile{}
	if err := scanner.Scan(
		&profile.ID,
		&profile.Name,
		&description,
		&profile.IsActive,
		&profile.IsDefault,
		&profile.IsEditable,
		&profile.Version,
		&deletedAt,
		&profile.CreatedAt,
		&profile.UpdatedAt,
	); err != nil {
		return Profile{}, err
	}
	profile.Description = nullableString(description)
	profile.DeletedAt = nullableTime(deletedAt)
	return profile, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
