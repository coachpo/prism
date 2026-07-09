package auth

import "time"

type authStatusResponse struct {
	AuthEnabled bool `json:"auth_enabled"`
}

type sessionResponse struct {
	Authenticated bool    `json:"authenticated"`
	AuthEnabled   bool    `json:"auth_enabled"`
	Username      *string `json:"username"`
}

type loginRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	SessionDuration string `json:"session_duration"`
}

type authSettingsResponse struct {
	AuthEnabled   bool    `json:"auth_enabled"`
	Username      *string `json:"username"`
	HasPassword   bool    `json:"has_password"`
	ProxyKeyLimit int     `json:"proxy_key_limit"`
}

type authSettingsUpdateRequest struct {
	AuthEnabled bool    `json:"auth_enabled"`
	Username    *string `json:"username"`
	Password    *string `json:"password"`
}

type successResponse struct {
	Success bool `json:"success"`
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
	RotatedFromID *int       `json:"rotated_from_id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type proxyAPIKeyCreateRequest struct {
	Name      string     `json:"name"`
	Notes     *string    `json:"notes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type proxyAPIKeyUpdateRequest struct {
	Name      string     `json:"name"`
	Notes     *string    `json:"notes"`
	IsActive  *bool      `json:"is_active"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type proxyAPIKeyMutationResponse struct {
	Key  string              `json:"key"`
	Item proxyAPIKeyResponse `json:"item"`
}

type deletedResponse struct {
	Deleted bool `json:"deleted"`
}
