package profiledomain

const (
	DefaultProfileID          = 1
	ProfileIDHeader           = "X-Profile-Id"
	DefaultProfileName        = "Default"
	DefaultProfileDescription = "System default profile"
	MaxNonDeletedProfiles     = 10

	ScopeErrorCodeHeaderMissing     = "profile_scope_header_missing"
	ScopeErrorCodeHeaderInvalid     = "profile_scope_header_invalid"
	ScopeErrorCodeHeaderNonPositive = "profile_scope_header_non_positive"
	ScopeErrorCodeProfileNotFound   = "profile_scope_profile_not_found"
)

type HTTPError struct {
	StatusCode int
	Code       string
	Detail     string
}

type ErrorResponse struct {
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail"`
}

func (err *HTTPError) Error() string {
	return err.Detail
}

func (err *HTTPError) ResponseBody() ErrorResponse {
	if err == nil {
		return ErrorResponse{}
	}
	return ErrorResponse{Code: err.Code, Detail: err.Detail}
}
