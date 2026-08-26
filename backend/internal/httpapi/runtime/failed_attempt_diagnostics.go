package runtime

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

const (
	errorSourcePrism     = "prism"
	errorSourceUpstream  = "upstream"
	errorSourceTransport = "transport"
	errorSourceClient    = "client"
	errorSourceUnknown   = "unknown"

	failureStageRouting          = "routing"
	failureStageAdmission        = "admission"
	failureStageUpstreamConnect  = "upstream_connect"
	failureStageUpstreamResponse = "upstream_response"
	failureStageStream           = "stream"
	failureStageUnknown          = "unknown"
)

// attemptFailureDiagnostics carries the safe failure projection for one
// attempt. Raw samples never enter these fields.
type attemptFailureDiagnostics struct {
	Source    string
	Stage     string
	Code      string
	Detail    string
	Redacted  bool
	Truncated bool
}

// upstreamFailureClass maps a transport error to a fixed classification
// label that never contains an upstream address. It is the only source of
// client-visible 502 detail: callers never receive host, port, or path.
func upstreamFailureClass(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "client_disconnected"
	case errors.Is(err, context.DeadlineExceeded):
		return "upstream_timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "upstream_timeout"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "upstream_dns_failed"
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return "upstream_tls_failed"
	}
	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return "upstream_tls_failed"
	}
	return "upstream_connect_failed"
}

// safeTransportDiagnostic builds the bounded transport diagnostic from a
// sanitized typed error string. It never includes raw provider bytes.
func safeTransportDiagnostic(err error) attemptFailureDiagnostics {
	if err == nil {
		return attemptFailureDiagnostics{}
	}
	message := err.Error()
	scrubbed := safediag.ScrubValue(message, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
	return attemptFailureDiagnostics{
		Source:    errorSourceTransport,
		Stage:     failureStageUpstreamConnect,
		Code:      safediag.CodeTransportError,
		Detail:    scrubbed.Value,
		Redacted:  scrubbed.Redacted,
		Truncated: scrubbed.Truncated,
	}
}

// safeStreamDiagnostic builds the bounded stream diagnostic. kind is the
// typed stream_error_kind (may be empty); detail is the raw stream error text
// which is scrubbed here before persistence.
func safeStreamDiagnostic(source string, stage string, kind string, outcome string, rawDetail string) attemptFailureDiagnostics {
	code := ""
	if strings.TrimSpace(kind) != "" {
		code = safediag.StreamKindFallbackCode(strings.TrimSpace(kind))
	} else if strings.TrimSpace(outcome) != "" {
		code = safediag.StreamOutcomeFallbackCode(strings.TrimSpace(outcome))
	}
	scrubbed := safediag.ScrubValue(rawDetail, safediag.ScrubOptions{MaxBytes: safediag.MaxStreamErrorDetailBytes})
	return attemptFailureDiagnostics{
		Source:    source,
		Stage:     stage,
		Code:      code,
		Detail:    scrubbed.Value,
		Redacted:  scrubbed.Redacted,
		Truncated: scrubbed.Truncated,
	}
}

// stableFallbackCode returns the stable error code for an HTTP failure.
func stableHTTPErrorCode(statusCode int, providerCode string) string {
	if code := safediag.AdoptProviderCode(providerCode); code != "" {
		return code
	}
	return safediag.HTTPFallbackCode(statusCode)
}
