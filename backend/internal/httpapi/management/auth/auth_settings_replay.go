package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// resolveAuthSettingsReplay owns the durable operation lookup and exact
// request/password replay proof. It runs after both domain fences and before
// reading the current revision, preserving lost-response recovery semantics.
func resolveAuthSettingsReplay(ctx context.Context, tx pgx.Tx, request putAuthSettingsRequest, outcome *authSettingsMutationOutcome) (bool, error) {
	var operationRow struct {
		RequestHash string
		ResultJSON  []byte
	}
	err := tx.QueryRow(ctx, `SELECT request_hash, result_json FROM settings_mutation_operations
		WHERE resource_kind = 'auth_settings' AND operation_id = $1`, request.OperationID).
		Scan(&operationRow.RequestHash, &operationRow.ResultJSON)
	if err == nil {
		hash := canonicalAuthRequestHash(request)
		if operationRow.RequestHash == hash {
			outcome.replayed = true
			if request.AccountChange.NewPassword != nil {
				var storedHash *string
				if err := tx.QueryRow(ctx, `SELECT password_hash FROM auth_config_versions
						WHERE created_operation_id = $1 ORDER BY id DESC LIMIT 1`, request.OperationID).Scan(&storedHash); err != nil || storedHash == nil || !verifyPassword(*request.AccountChange.NewPassword, *storedHash) {
					return false, authSettingsProblem(http.StatusConflict, "operation_id_conflict", "operation_id_conflict", nil)
				}
			}
			if len(operationRow.ResultJSON) > 0 {
				return true, json.Unmarshal(operationRow.ResultJSON, &outcome.result)
			}
			return true, nil
		}
		return false, authSettingsProblem(http.StatusConflict, "operation_id_conflict", "operation_id_conflict", map[string]any{
			"details": map[string]any{"operation_id": request.OperationID, "recovery": "inspect_operation"},
		})
	}
	if err != pgx.ErrNoRows {
		return false, err
	}
	return false, nil
}

// canonicalAuthRequestHash is the operation replay identity. Password
// contents remain absent from the hash; a stored slow hash proves a supplied
// password separately when an operation includes one.
func canonicalAuthRequestHash(request putAuthSettingsRequest) string {
	account := request.AccountChange.Kind
	username := ""
	if request.AccountChange.Username != nil {
		username = *request.AccountChange.Username
	}
	passwordDiscriminator := "absent"
	if request.AccountChange.NewPassword != nil {
		passwordDiscriminator = "present"
	}
	ack := fmt.Sprintf("%v:%v:%v",
		boolValue(request.Acknowledgements.EnableWithoutActiveProxyKeys),
		boolValue(request.Acknowledgements.DisableToPermissiveAccess),
		boolValue(request.Acknowledgements.InvalidateOperatorSessions))
	readinessGeneration := "null"
	if request.ExpectedProxyKeyReadinessGeneration != nil {
		readinessGeneration = *request.ExpectedProxyKeyReadinessGeneration
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%v|%s|%s|%s",
		request.OperationID, request.ExpectedRevision, request.DesiredAuthEnabled, account, username,
		passwordDiscriminator+"|"+readinessGeneration+"|"+ack)))
	return hex.EncodeToString(sum[:])
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
