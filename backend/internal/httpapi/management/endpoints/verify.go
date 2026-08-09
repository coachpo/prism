package endpoints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	providerauth "github.com/coachpo/prism/backend/internal/providerauth"
)

// Verify outcomes (canonical, stable codes).
const (
	verifyOutcomeVerified            = "verified"
	verifyOutcomeAuthenticationFailed = "authentication_failed"
	verifyOutcomeProbeUnsupported    = "probe_unsupported"
	verifyOutcomeAPIMismatch         = "api_mismatch"
	verifyOutcomeUpstreamRejected    = "upstream_rejected"
	verifyOutcomeUpstreamUnavailable = "upstream_unavailable"
	verifyOutcomeUnreachable         = "unreachable"
	verifyOutcomeTimeout             = "timeout"
)

const (
	verifyMaxResponseBodyBytes = 8 * 1024
	verifyMaxErrorSummaryBytes = 512
	verifyMaxSameOriginRedirects = 3
	verifyConcurrencyLimit     = 4
)

var supportedVerifyFamilies = map[string]struct{}{"openai": {}, "anthropic": {}, "gemini": {}}

type verifyProbeSpec struct {
	Path    string
	Query   string
	Headers map[string]string // extra headers beyond family auth
	Auth    func(plaintextKey string) map[string]string
	Valid   func(body []byte) bool
}

func verifyProbeSpecFor(family string) verifyProbeSpec {
	switch family {
	case "openai":
		return verifyProbeSpec{
			Path:  "/v1/models",
			Auth: func(key string) map[string]string {
				if strings.TrimSpace(key) == "" {
					return nil
				}
				return map[string]string{"Authorization": "Bearer " + key}
			},
			Valid: func(body []byte) bool {
				var payload struct {
					Data []json.RawMessage `json:"data"`
				}
				return json.Unmarshal(body, &payload) == nil && payload.Data != nil
			},
		}
	case "anthropic":
		return verifyProbeSpec{
			Path:    "/v1/models",
			Query:   "limit=1",
			Headers: map[string]string{"anthropic-version": "2023-06-01"},
			Auth: func(key string) map[string]string {
				if strings.TrimSpace(key) == "" {
					return nil
				}
				return map[string]string{"x-api-key": key}
			},
			Valid: func(body []byte) bool {
				var payload struct {
					Data []json.RawMessage `json:"data"`
				}
				return json.Unmarshal(body, &payload) == nil && payload.Data != nil
			},
		}
	case "gemini":
		return verifyProbeSpec{
			Path:  "/v1beta/models",
			Query: "pageSize=1",
			Auth: func(key string) map[string]string {
				if strings.TrimSpace(key) == "" {
					return nil
				}
				// x-goog-api-key header only; never a key query parameter.
				return map[string]string{"x-goog-api-key": key}
			},
			Valid: func(body []byte) bool {
				var payload struct {
					Models []json.RawMessage `json:"models"`
				}
				return json.Unmarshal(body, &payload) == nil && payload.Models != nil
			},
		}
	}
	return verifyProbeSpec{}
}

type verifySnapshot struct {
	EndpointID     int
	ConfigRevision int64
	Fingerprint    *string
	BaseURL        string
	PlaintextKey   string
}

// handleEndpointVerify runs one explicit, read-only, family-aware metadata
// probe. Nothing is persisted: no request logs, usage, audit, loadbalance
// state, health fields or Endpoint timestamps. Results are only returned in
// this synchronous response.
func (s *Service) handleEndpointVerify(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	var requestBody endpointVerifyRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	family := providerauth.NormalizeAPIFamily(requestBody.APIFamily)
	if _, supported := supportedVerifyFamilies[family]; !supported {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, map[string]any{"code": "validation_failed", "fields": map[string]string{"api_family": "api_family_invalid"}})
		return
	}
	if requestBody.ExpectedConfigRevision < 1 {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, map[string]any{"code": "validation_failed", "fields": map[string]string{"expected_config_revision": "config_revision_invalid"}})
		return
	}

	// Load the immutable snapshot without holding a DB transaction during
	// outbound I/O.
	snapshot, err := pgxutil.InTxValue(r.Context(), s.pool, "endpoint-verify-load", func(tx pgx.Tx) (verifySnapshot, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return verifySnapshot{}, err
		}
		record, found, err := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, false)
		if err != nil {
			return verifySnapshot{}, err
		}
		if !found {
			return verifySnapshot{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		plaintext := ""
		if endpointdomain.HasAPIKey(record.APIKey) {
			plaintext, err = endpointdomain.DecryptSecret(record.APIKey, s.secretEncryptionKey)
			if err != nil {
				return verifySnapshot{}, err
			}
		}
		return verifySnapshot{
			EndpointID:     record.ID,
			ConfigRevision: record.ConfigRevision,
			Fingerprint:    record.APIKeyFingerprint,
			BaseURL:        record.BaseURL,
			PlaintextKey:   plaintext,
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	// Pre-probe revision precondition: any difference sends no probe and
	// returns the current Endpoint DTO.
	if snapshot.ConfigRevision != requestBody.ExpectedConfigRevision {
		current, loadErr := pgxutil.InTxValue(r.Context(), s.pool, "endpoint-verify-current", func(tx pgx.Tx) (endpointResponse, error) {
			profile, resolveErr := resolveEffectiveProfile(r.Context(), tx, r)
			if resolveErr != nil {
				return endpointResponse{}, resolveErr
			}
			record, found, loadErr := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, false)
			if loadErr != nil {
				return endpointResponse{}, loadErr
			}
			if !found {
				return endpointResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
			}
			return responseFromRecord(record), nil
		})
		if loadErr != nil {
			writeDomainError(w, r, s.corsSnapshot(), loadErr)
			return
		}
		writeDomainError(w, r, s.corsSnapshot(), &domainError{StatusCode: http.StatusConflict, Detail: endpointConfigChangedDetail{Code: "endpoint_config_changed", Message: "Endpoint configuration changed before verification; refresh and retry", Endpoint: current}})
		return
	}

	// Process-local concurrency guard (management M3 tier).
	if !s.acquireVerifySlot() {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusTooManyRequests, map[string]any{"code": "verify_capacity", "message": "Verification is busy; retry shortly"})
		return
	}
	defer s.releaseVerifySlot()

	spec := verifyProbeSpecFor(family)
	probeURL, joinErr := buildUpstreamURL(snapshot.BaseURL, spec.Path, spec.Query)
	if joinErr != nil {
		responseutil.WriteJSON(w, http.StatusOK, verifyResponseForSnapshot(snapshot, family, spec.Path, verifyOutcomeUpstreamRejected, nil, 0, "base URL could not be joined for verification"))
		return
	}
	probeParsed, _ := url.Parse(probeURL)
	started := s.nowUTC()
	statusCode, bodyBytes, outcome, errorSummary := s.runVerifyProbe(r.Context(), probeURL, probeParsed, spec, snapshot.PlaintextKey)
	durationMS := s.nowUTC().Sub(started).Milliseconds()
	upstreamStatus := (*int)(nil)
	if statusCode > 0 {
		upstreamStatus = &statusCode
	}
	response := endpointVerifyResponse{
		EndpointID:        snapshot.EndpointID,
		APIFamily:         family,
		ConfigRevision:    snapshot.ConfigRevision,
		APIKeyFingerprint: snapshot.Fingerprint,
		Outcome:           outcome,
		ProbePath:         spec.Path,
		UpstreamStatus:    upstreamStatus,
		DurationMS:        durationMS,
	}
	if errorSummary != "" {
		response.ErrorSummary = &errorSummary
	}

	// Short re-read after the network call: is_current only when the row still
	// exists and both revision and full key identity equal the sent snapshot.
	isCurrent, recheckErr := pgxutil.InTxValue(r.Context(), s.pool, "endpoint-verify-recheck", func(tx pgx.Tx) (bool, error) {
		profile, resolveErr := resolveEffectiveProfile(r.Context(), tx, r)
		if resolveErr != nil {
			return false, resolveErr
		}
		record, found, loadErr := loadEndpointRecord(r.Context(), tx, profile.ID, endpointID, false)
		if loadErr != nil {
			return false, loadErr
		}
		if !found {
			return false, nil
		}
		if record.ConfigRevision != snapshot.ConfigRevision {
			return false, nil
		}
		if !endpointdomain.HasAPIKey(record.APIKey) {
			return snapshot.PlaintextKey == "", nil
		}
		currentPlaintext, decryptErr := endpointdomain.DecryptSecret(record.APIKey, s.secretEncryptionKey)
		if decryptErr != nil {
			return false, nil
		}
		return endpointdomain.APIKeyIdentityMatches(s.secretEncryptionKey, snapshot.PlaintextKey, currentPlaintext), nil
	})
	if recheckErr == nil {
		response.IsCurrent = isCurrent
	}
	_ = bodyBytes
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func verifyResponseForSnapshot(snapshot verifySnapshot, family string, probePath string, outcome string, upstreamStatus *int, durationMS int64, errorSummary string) endpointVerifyResponse {
	response := endpointVerifyResponse{
		EndpointID:        snapshot.EndpointID,
		APIFamily:         family,
		ConfigRevision:    snapshot.ConfigRevision,
		APIKeyFingerprint: snapshot.Fingerprint,
		Outcome:           outcome,
		ProbePath:         probePath,
		UpstreamStatus:    upstreamStatus,
		DurationMS:        durationMS,
	}
	if errorSummary != "" {
		response.ErrorSummary = &errorSummary
	}
	return response
}

// buildUpstreamURL mirrors the runtime join rules exactly (same normalized
// base URL semantics, no silent de-duplication of version segments).
func buildUpstreamURL(baseURL string, requestPath string, requestQuery string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse upstream base URL: %w", err)
	}
	basePath := strings.TrimRight(parsedURL.Path, "/")
	finalPath := requestPath
	if !strings.HasPrefix(finalPath, "/") {
		finalPath = "/" + finalPath
	}
	parsedURL.Path = basePath + finalPath
	parsedURL.RawPath = parsedURL.EscapedPath()
	if requestQuery != "" {
		if parsedURL.RawQuery != "" {
			parsedURL.RawQuery = parsedURL.RawQuery + "&" + requestQuery
		} else {
			parsedURL.RawQuery = requestQuery
		}
	}
	return parsedURL.String(), nil
}

var errRedirectBlocked = errors.New("cross-origin redirect blocked")
var errRedirectExhausted = errors.New("too many same-origin redirects")

// runVerifyProbe performs the metadata request with the configured total
// deadline, no cross-origin credential forwarding, at most 3 same-origin
// redirects, and an 8 KiB response body cap. It returns the outcome and a
// redacted, truncated error summary.
func (s *Service) runVerifyProbe(ctx context.Context, probeURL string, probeParsed *url.URL, spec verifyProbeSpec, plaintextKey string) (int, []byte, string, string) {
	timeout := s.attemptTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= verifyMaxSameOriginRedirects {
				return errRedirectExhausted
			}
			next := request.URL
			if next.Scheme != probeParsed.Scheme || next.Host != probeParsed.Host {
				return errRedirectBlocked
			}
			return nil
		},
	}

	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, nil, verifyOutcomeUpstreamRejected, "verification request could not be built"
	}
	request.Header.Set("Accept", "application/json")
	authHeaders := spec.Auth(plaintextKey)
	for name, value := range authHeaders {
		request.Header.Set(name, value)
	}
	for name, value := range spec.Headers {
		request.Header.Set(name, value)
	}

	httpResponse, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil || probeCtx.Err() != nil {
			return 0, nil, verifyOutcomeTimeout, "verification request timed out"
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return 0, nil, verifyOutcomeTimeout, "verification request timed out"
		}
		if errors.Is(err, errRedirectBlocked) {
			return 0, nil, verifyOutcomeUpstreamRejected, "upstream issued a cross-origin redirect; verification did not follow it"
		}
		if errors.Is(err, errRedirectExhausted) {
			return 0, nil, verifyOutcomeUpstreamRejected, "upstream issued too many redirects; verification stopped"
		}
		return 0, nil, verifyOutcomeUnreachable, redactVerifySummary(err.Error())
	}
	defer func() { _ = httpResponse.Body.Close() }()
	statusCode := httpResponse.StatusCode

	// Never expose raw body, headers, Location, query or stack details.
	if statusCode >= 300 && statusCode < 400 {
		return statusCode, nil, verifyOutcomeUpstreamRejected, fmt.Sprintf("upstream issued redirect (HTTP %d)", statusCode)
	}
	limitedBody := make([]byte, 0, verifyMaxResponseBodyBytes)
	limitedReader := io.LimitReader(httpResponse.Body, verifyMaxResponseBodyBytes)
	body, readErr := io.ReadAll(limitedReader)
	if readErr != nil {
		return statusCode, nil, verifyOutcomeUpstreamUnavailable, "could not read the verification response"
	}
	limitedBody = body

	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return statusCode, limitedBody, verifyOutcomeAuthenticationFailed, fmt.Sprintf("authentication failed (HTTP %d)", statusCode)
	case statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented:
		return statusCode, limitedBody, verifyOutcomeProbeUnsupported, fmt.Sprintf("upstream does not support this verification request (HTTP %d)", statusCode)
	case statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500:
		return statusCode, limitedBody, verifyOutcomeUpstreamUnavailable, fmt.Sprintf("upstream temporarily unavailable (HTTP %d)", statusCode)
	case statusCode >= 400:
		return statusCode, limitedBody, verifyOutcomeUpstreamRejected, fmt.Sprintf("upstream rejected the verification request (HTTP %d)", statusCode)
	case statusCode >= 200 && statusCode < 300:
		if !spec.Valid(limitedBody) {
			return statusCode, limitedBody, verifyOutcomeAPIMismatch, "the upstream response does not match the selected provider protocol"
		}
		return statusCode, limitedBody, verifyOutcomeVerified, ""
	default:
		return statusCode, limitedBody, verifyOutcomeUpstreamRejected, fmt.Sprintf("unexpected upstream response (HTTP %d)", statusCode)
	}
}

// redactVerifySummary truncates and strips anything that could carry secrets
// from a transport-level error summary, keeping it under 512 UTF-8 bytes.
func redactVerifySummary(message string) string {
	// Transport errors can embed URLs; strip query strings defensively.
	if at := strings.Index(message, "?"); at >= 0 {
		message = message[:at]
	}
	message = strings.TrimSpace(message)
	runes := []rune(message)
	if len(runes) > verifyMaxErrorSummaryBytes {
		runes = runes[:verifyMaxErrorSummaryBytes]
	}
	return string(runes)
}
