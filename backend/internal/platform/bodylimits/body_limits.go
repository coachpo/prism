package bodylimits

import (
	"encoding/json"
	"errors"
	"net/http"
)

const (
	RequestBodyTooLargeCode = "request_body_too_large"

	AuthRequestBodyLimitBytes           int64 = 64 * 1024
	ManagementJSONRequestBodyLimitBytes int64 = 1024 * 1024
	BootstrapRequestBodyLimitBytes      int64 = 512 * 1024
	RuntimeJSONRequestBodyLimitBytes    int64 = 20 * 1024 * 1024
	RuntimeMediaRequestBodyLimitBytes   int64 = 100 * 1024 * 1024
)

const requestBodyTooLargeMessage = "Request body exceeds the maximum allowed size."

type requestBodyTooLargeResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message"`
	LimitBytes int64  `json:"limit_bytes"`
}

func LimitRequestBody(w http.ResponseWriter, r *http.Request, limitBytes int64) {
	if r == nil || r.Body == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limitBytes)
}

func MaxBytesError(err error) (*http.MaxBytesError, bool) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return maxBytesErr, true
	}
	return nil, false
}

func IsRequestBodyTooLarge(err error) bool {
	_, ok := MaxBytesError(err)
	return ok
}

func WriteRequestBodyTooLarge(w http.ResponseWriter, limitBytes int64) {
	if limitBytes < 0 {
		limitBytes = 0
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_ = json.NewEncoder(w).Encode(requestBodyTooLargeResponse{
		Error:      RequestBodyTooLargeCode,
		Message:    requestBodyTooLargeMessage,
		LimitBytes: limitBytes,
	})
}

func WriteMaxBytesError(w http.ResponseWriter, err error, fallbackLimitBytes int64) bool {
	maxBytesErr, ok := MaxBytesError(err)
	if !ok {
		return false
	}
	limitBytes := maxBytesErr.Limit
	if limitBytes <= 0 {
		limitBytes = fallbackLimitBytes
	}
	WriteRequestBodyTooLarge(w, limitBytes)
	return true
}
