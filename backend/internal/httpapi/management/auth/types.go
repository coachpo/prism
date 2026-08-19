package auth

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type authStatusResponse struct {
	// Tagged PublicAuthStatus union: state is enabled|disabled|transition_fail_closed;
	// transition_state is null|disabling_enforced|enabling_fail_closed|rollback_required;
	// effective_generation is a canonical positive decimal string.
	State               string `json:"state"`
	TransitionState     any    `json:"transition_state"`
	LoginAvailable      bool   `json:"login_available"`
	EffectiveGeneration string `json:"effective_generation"`
	RetryAfterSeconds   *int64 `json:"retry_after_seconds"`
}

type sessionResponse struct {
	Authenticated bool    `json:"authenticated"`
	AuthEnabled   bool    `json:"auth_enabled"`
	Username      *string `json:"username"`
	SubjectKey    *string `json:"subject_key,omitempty"`
}

type loginRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	SessionDuration string `json:"session_duration"`
}

type proxyAPIKeyResponse struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	KeyPrefix     string     `json:"key_prefix"`
	KeyPreview    string     `json:"key_preview"`
	IsActive      bool       `json:"is_active"`
	ExpiresAt     *time.Time `json:"expires_at"`
	LastUsedAt    *time.Time `json:"last_used_at"`
	LastUsedIP    *string    `json:"last_used_ip"`
	Notes         *string    `json:"notes"`
	RotatedAt     *time.Time `json:"rotated_at"`
	RotationCount int        `json:"rotation_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type proxyAPIKeyCreateRequest struct {
	Name      string     `json:"name"`
	Notes     *string    `json:"notes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// proxyKeyExpiryUpdate is a presence-aware expiry value: JSON null clears the
// expiry, an omitted field preserves it, and an RFC3339 string sets a new
// future instant. The frontend never relies on undefined/null serialization
// accidents; the backend resolves DST/gap issues only through the RFC3339
// instant it receives.
type proxyKeyExpiryUpdate struct {
	present bool
	clear   bool
	value   *time.Time
}

func parseProxyKeyExpiryUpdate(raw json.RawMessage) (proxyKeyExpiryUpdate, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return proxyKeyExpiryUpdate{present: len(raw) > 0, clear: len(raw) > 0}, nil
	}
	var value time.Time
	if err := json.Unmarshal(raw, &value); err != nil {
		return proxyKeyExpiryUpdate{}, err
	}
	return proxyKeyExpiryUpdate{present: true, value: &value}, nil
}

type proxyAPIKeyUpdateRequest struct {
	Name      string               `json:"name"`
	Notes     *string              `json:"notes"`
	IsActive  *bool                `json:"is_active"`
	ExpiresAt proxyKeyExpiryUpdate `json:"expires_at"`
}

func (request *proxyAPIKeyUpdateRequest) UnmarshalJSON(data []byte) error {
	type rawProxyAPIKeyUpdateRequest struct {
		Name      string          `json:"name"`
		Notes     *string         `json:"notes"`
		IsActive  *bool           `json:"is_active"`
		ExpiresAt json.RawMessage `json:"expires_at"`
	}
	var raw rawProxyAPIKeyUpdateRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	expiry, err := parseProxyKeyExpiryUpdate(raw.ExpiresAt)
	if err != nil {
		return err
	}
	request.Name = raw.Name
	request.Notes = raw.Notes
	request.IsActive = raw.IsActive
	request.ExpiresAt = expiry
	return nil
}

// proxyKeyCapacitySnapshot is the single authoritative capacity truth. All
// fields come from one server transaction/clock snapshot; the UI must not
// derive used/remaining from list length.
type proxyKeyCapacitySnapshot struct {
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	Remaining int       `json:"remaining"`
	CountedAt time.Time `json:"counted_at"`
}

type proxyAPIKeyListResponse struct {
	Items    []proxyAPIKeyResponse    `json:"items"`
	Capacity proxyKeyCapacitySnapshot `json:"capacity"`
}

type proxyAPIKeyMutationResponse struct {
	Key      string                   `json:"key,omitempty"`
	Item     proxyAPIKeyResponse      `json:"item"`
	Capacity proxyKeyCapacitySnapshot `json:"capacity"`
}

type proxyAPIKeyUpdateResponse struct {
	Item     proxyAPIKeyResponse      `json:"item"`
	Capacity proxyKeyCapacitySnapshot `json:"capacity"`
}

type deletedResponse struct {
	DeletedID int                      `json:"deleted_id"`
	Capacity  proxyKeyCapacitySnapshot `json:"capacity"`
}

type domainError struct {
	StatusCode int
	Code       string
	Detail     string
	Details    any
	Fields     map[string]any
}

func (err *domainError) Error() string {
	return err.Detail
}

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
