package contracttest

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func loadRefreshTokens(t *testing.T, harness *contractHarness) []refreshTokenSnapshot {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT id, rotated_from_id, revoked_at, last_used_at FROM refresh_tokens ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query refresh tokens: %v", err)
	}
	defer rows.Close()
	var snapshots []refreshTokenSnapshot
	for rows.Next() {
		var rotatedFromID sqlNullInt32
		var revokedAt sqlNullTime
		var lastUsedAt sqlNullTime
		var snapshot refreshTokenSnapshot
		if err := rows.Scan(&snapshot.ID, &rotatedFromID, &revokedAt, &lastUsedAt); err != nil {
			t.Fatalf("scan refresh token: %v", err)
		}
		snapshot.RotatedFromID = rotatedFromID.ptr()
		snapshot.RevokedAt = revokedAt.ptr()
		snapshot.LastUsedAt = lastUsedAt.ptr()
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate refresh tokens: %v", err)
	}
	return snapshots
}

func loadLoginThrottleEntries(t *testing.T, harness *contractHarness) []loginThrottleSnapshot {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT subject_key, remote_address, failure_count, locked_until FROM login_throttle_ledger ORDER BY subject_key, remote_address`)
	if err != nil {
		t.Fatalf("query login throttle ledger: %v", err)
	}
	defer rows.Close()
	var snapshots []loginThrottleSnapshot
	for rows.Next() {
		var lockedUntil sqlNullTime
		var snapshot loginThrottleSnapshot
		if err := rows.Scan(&snapshot.SubjectKey, &snapshot.RemoteAddress, &snapshot.FailureCount, &lockedUntil); err != nil {
			t.Fatalf("scan login throttle ledger: %v", err)
		}
		snapshot.LockedUntil = lockedUntil.ptr()
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate login throttle ledger: %v", err)
	}
	return snapshots
}

func loadAppAuthSettings(t *testing.T, harness *contractHarness) appAuthSettingsRecord {
	t.Helper()
	var username, passwordHash sqlNullString
	var snapshot appAuthSettingsRecord
	var configMode *string
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT a.id, v.desired_mode, v.username, v.password_hash, v.session_version FROM app_auth_settings AS a
		 LEFT JOIN auth_config_versions AS v ON v.id = a.effective_config_version_id
		 WHERE a.singleton_key = 'app'`,
	).Scan(
		&snapshot.ID,
		&configMode,
		&username,
		&passwordHash,
		&snapshot.TokenVersion,
	); err != nil {
		t.Fatalf("query app auth settings: %v", err)
	}
	if configMode != nil {
		snapshot.AuthEnabled = *configMode == "enabled"
	}
	snapshot.Username = username.ptr()
	snapshot.PasswordHash = passwordHash.ptr()
	return snapshot
}

func loadProxyKeys(t *testing.T, harness *contractHarness) []proxyKeySnapshot {
	t.Helper()
	rows, err := harness.conn.Query(
		context.Background(),
		`SELECT id, name, key_prefix, is_active, expires_at, last_used_at, last_used_ip, created_by_auth_subject_id, notes, rotated_at, rotation_count, created_at FROM proxy_api_keys ORDER BY id ASC`,
	)
	if err != nil {
		t.Fatalf("query proxy keys: %v", err)
	}
	defer rows.Close()
	var snapshots []proxyKeySnapshot
	for rows.Next() {
		var expiresAt, lastUsedAt, rotatedAt sqlNullTime
		var lastUsedIP, notes sqlNullString
		var createdByID sqlNullInt32
		var snapshot proxyKeySnapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.Name, &snapshot.KeyPrefix, &snapshot.IsActive, &expiresAt, &lastUsedAt, &lastUsedIP, &createdByID, &notes, &rotatedAt, &snapshot.RotationCount, &snapshot.CreatedAt); err != nil {
			t.Fatalf("scan proxy key: %v", err)
		}
		snapshot.ExpiresAt = expiresAt.ptr()
		snapshot.LastUsedAt = lastUsedAt.ptr()
		snapshot.LastUsedIP = lastUsedIP.ptr()
		snapshot.CreatedByID = createdByID.ptr()
		snapshot.Notes = notes.ptr()
		snapshot.RotatedAt = rotatedAt.ptr()
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate proxy keys: %v", err)
	}
	return snapshots
}

func loadProxyKeyByPrefix(t *testing.T, harness *contractHarness, keyPrefix string) proxyKeySnapshot {
	t.Helper()
	for _, snapshot := range loadProxyKeys(t, harness) {
		if snapshot.KeyPrefix == keyPrefix {
			return snapshot
		}
	}
	t.Fatalf("query proxy key by prefix: %s not found", keyPrefix)
	return proxyKeySnapshot{}
}

type sqlNullString struct{ sql *string }
type sqlNullTime struct{ time *time.Time }
type sqlNullInt32 struct{ value *int }

func (value *sqlNullString) Scan(src any) error {
	if src == nil {
		value.sql = nil
		return nil
	}
	switch typed := src.(type) {
	case string:
		value.sql = stringPtr(typed)
		return nil
	case []byte:
		value.sql = stringPtr(string(typed))
		return nil
	default:
		return fmt.Errorf("unsupported string scan type %T", src)
	}
}

func (value sqlNullString) ptr() *string { return value.sql }

func (value *sqlNullTime) Scan(src any) error {
	if src == nil {
		value.time = nil
		return nil
	}
	typed, ok := src.(time.Time)
	if !ok {
		return fmt.Errorf("unsupported time scan type %T", src)
	}
	utc := typed.UTC()
	value.time = &utc
	return nil
}

func (value sqlNullTime) ptr() *time.Time { return value.time }

func (value *sqlNullInt32) Scan(src any) error {
	if src == nil {
		value.value = nil
		return nil
	}
	switch typed := src.(type) {
	case int32:
		converted := int(typed)
		value.value = &converted
		return nil
	case int64:
		converted := int(typed)
		value.value = &converted
		return nil
	case int:
		converted := typed
		value.value = &converted
		return nil
	default:
		return fmt.Errorf("unsupported int scan type %T", src)
	}
}

func (value sqlNullInt32) ptr() *int { return value.value }

func stringPtr(value string) *string {
	result := value
	return &result
}

func modelLoadVendorIDByKey(t *testing.T, _ *contractHarness, _ string) int {
	t.Helper()
	return 1
}
