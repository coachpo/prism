package profiledomain

const (
	ProfileIDHeader           = "X-Profile-Id"
	DefaultProfileName        = "Default"
	DefaultProfileDescription = "System default profile"
	MaxNonDeletedProfiles     = 10
)

type HTTPError struct {
	StatusCode int
	Detail     string
}

func (err *HTTPError) Error() string {
	return err.Detail
}
