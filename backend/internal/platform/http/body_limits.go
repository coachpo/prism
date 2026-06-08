package platformhttp

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
)

const (
	RequestBodyTooLargeCode = bodylimits.RequestBodyTooLargeCode

	AuthRequestBodyLimitBytes           = bodylimits.AuthRequestBodyLimitBytes
	ManagementJSONRequestBodyLimitBytes = bodylimits.ManagementJSONRequestBodyLimitBytes
	BootstrapRequestBodyLimitBytes      = bodylimits.BootstrapRequestBodyLimitBytes
	ConfigBundleRequestBodyLimitBytes   = bodylimits.ConfigBundleRequestBodyLimitBytes
	RuntimeJSONRequestBodyLimitBytes    = bodylimits.RuntimeJSONRequestBodyLimitBytes
	RuntimeMediaRequestBodyLimitBytes   = bodylimits.RuntimeMediaRequestBodyLimitBytes
)

func LimitRequestBody(w http.ResponseWriter, r *http.Request, limitBytes int64) {
	bodylimits.LimitRequestBody(w, r, limitBytes)
}

func MaxBytesError(err error) (*http.MaxBytesError, bool) {
	return bodylimits.MaxBytesError(err)
}

func IsRequestBodyTooLarge(err error) bool {
	return bodylimits.IsRequestBodyTooLarge(err)
}

func WriteRequestBodyTooLarge(w http.ResponseWriter, limitBytes int64) {
	bodylimits.WriteRequestBodyTooLarge(w, limitBytes)
}

func WriteMaxBytesError(w http.ResponseWriter, err error, fallbackLimitBytes int64) bool {
	return bodylimits.WriteMaxBytesError(w, err, fallbackLimitBytes)
}
