package stats

type HTTPError struct {
	StatusCode int
	Detail     string
	Code       string
	Details    map[string]any
}

func (err *HTTPError) Error() string {
	return err.Detail
}
