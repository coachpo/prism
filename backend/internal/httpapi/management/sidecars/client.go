package sidecars

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	urlpkg "net/url"
	pathpkg "path"
	"strings"
	"time"
)

var sidecarReservedTestNetPrefixes = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("2001:db8::/32"),
}

const (
	cliProxyManagementPrefixValue         = "/v0/management"
	defaultCLIProxyResponseBodyLimitBytes = int64(4 << 20)
)

type CLIProxyErrorCode string

const (
	CLIProxyErrorInvalidBaseURL        CLIProxyErrorCode = "invalid_base_url"
	CLIProxyErrorPrivateNetworkBlocked CLIProxyErrorCode = "private_network_blocked"
	CLIProxyErrorInsecureHTTPBlocked   CLIProxyErrorCode = "insecure_http_blocked"
	CLIProxyErrorUnsupportedPath       CLIProxyErrorCode = "unsupported_management_path"
	CLIProxyErrorRequestBuild          CLIProxyErrorCode = "request_build"
	CLIProxyErrorRequestFailed         CLIProxyErrorCode = "request_failed"
	CLIProxyErrorTimeout               CLIProxyErrorCode = "timeout"
	CLIProxyErrorInvalidManagementAuth CLIProxyErrorCode = "invalid_management_auth"
	CLIProxyErrorManagementDisabled    CLIProxyErrorCode = "management_disabled"
	CLIProxyErrorUpstreamStatus        CLIProxyErrorCode = "upstream_status"
	CLIProxyErrorOversizedBody         CLIProxyErrorCode = "oversized_body"
	CLIProxyErrorMalformedJSON         CLIProxyErrorCode = "malformed_json"
	CLIProxyErrorReadFailed            CLIProxyErrorCode = "read_failed"
)

type CLIProxyConnectionPolicy struct {
	AllowPrivateNetwork bool
	AllowInsecureHTTP   bool
	SkipTLSVerify       bool
}

type CLIProxyTarget struct {
	BaseURL               string
	ManagementPassword    string
	AllowPrivateNetwork   bool
	AllowInsecureHTTP     bool
	SkipTLSVerify         bool
	RequestTimeoutSeconds int
}

type CLIProxyResponse struct {
	StatusCode int
	Path       string
}

type CLIProxyClientError struct {
	Code       CLIProxyErrorCode
	StatusCode int
	Path       string
	Err        error
}

func (err *CLIProxyClientError) Error() string {
	if err == nil {
		return ""
	}
	if err.Err != nil {
		return fmt.Sprintf("%s %s: %v", err.Code, err.Path, err.Err)
	}
	if err.StatusCode != 0 {
		return fmt.Sprintf("%s %s status=%d", err.Code, err.Path, err.StatusCode)
	}
	return fmt.Sprintf("%s %s", err.Code, err.Path)
}

func (err *CLIProxyClientError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

type dnsResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type CLIProxyClient struct {
	httpClient     *http.Client
	bodyLimitBytes int64
	resolver       dnsResolver
}

func NewCLIProxyClient(httpClient *http.Client) *CLIProxyClient {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &CLIProxyClient{httpClient: httpClient, bodyLimitBytes: defaultCLIProxyResponseBodyLimitBytes, resolver: net.DefaultResolver}
}

var cliProxyManagementPathList = []string{
	"/auth-files",
	"/auth-files/status",
	"/auth-files/fields",
	"/gemini-api-key",
	"/claude-api-key",
	"/codex-api-key",
	"/vertex-api-key",
	"/openai-compatibility",
}

var cliProxyManagementPathAllowlist = map[string]struct{}{
	"/auth-files":           {},
	"/auth-files/status":    {},
	"/auth-files/fields":    {},
	"/gemini-api-key":       {},
	"/claude-api-key":       {},
	"/codex-api-key":        {},
	"/vertex-api-key":       {},
	"/openai-compatibility": {},
}

func SupportedCLIProxyManagementPaths() []string {
	return append([]string(nil), cliProxyManagementPathList...)
}

func NormalizeCLIProxyBaseURL(rawURL string, policy CLIProxyConnectionPolicy) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url must include scheme and host")}
	}
	parsed, err := urlpkg.Parse(trimmed)
	if err != nil {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url must be a valid URL")}
	}
	parsed.Scheme = strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url scheme must be http or https")}
	}
	if parsed.Scheme == "http" && !policy.AllowInsecureHTTP {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInsecureHTTPBlocked, Err: errors.New("allow_insecure_http is required for http sidecar URLs")}
	}
	if parsed.User != nil {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url must not include userinfo")}
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url host is required")}
	}
	if !isASCIIHost(parsed.Hostname()) {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url host must be ASCII")}
	}
	if parsed.Fragment != "" {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url must not include a fragment")}
	}
	if parsed.RawQuery != "" {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: errors.New("base_url must not include a query string")}
	}
	if err := validateSidecarHostPolicy(parsed.Hostname(), policy); err != nil {
		return "", err
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = normalizeCLIProxyBasePath(parsed.Path)
	parsed.RawPath = ""
	parsed.ForceQuery = false
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeCLIProxyBasePath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(trimmed, "/"))
	if cleaned == "/" {
		return ""
	}
	if cleaned == cliProxyManagementPrefixValue {
		return ""
	}
	if strings.HasSuffix(cleaned, cliProxyManagementPrefixValue) {
		cleaned = strings.TrimSuffix(cleaned, cliProxyManagementPrefixValue)
		if cleaned == "" || cleaned == "/" {
			return ""
		}
	}
	return cleaned
}

func (c *CLIProxyClient) FetchJSON(ctx context.Context, target CLIProxyTarget, method string, managementPath string, payload any, responseTarget any) (CLIProxyResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	policy := CLIProxyConnectionPolicy{AllowPrivateNetwork: target.AllowPrivateNetwork, AllowInsecureHTTP: target.AllowInsecureHTTP, SkipTLSVerify: target.SkipTLSVerify}
	baseURL, err := NormalizeCLIProxyBaseURL(target.BaseURL, policy)
	if err != nil {
		return CLIProxyResponse{}, err
	}
	path, err := normalizeCLIProxyManagementPath(managementPath)
	if err != nil {
		return CLIProxyResponse{}, err
	}
	requestURL, err := buildCLIProxyManagementURL(baseURL, path)
	if err != nil {
		return CLIProxyResponse{}, err
	}
	bodyBytes, err := marshalCLIProxyPayload(payload)
	if err != nil {
		return CLIProxyResponse{}, &CLIProxyClientError{Code: CLIProxyErrorRequestBuild, Path: path, Err: err}
	}
	timeout := targetRequestTimeout(target.RequestTimeoutSeconds)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	networkTarget, err := c.resolveNetworkTarget(requestCtx, baseURL, policy)
	if err != nil {
		return CLIProxyResponse{}, err
	}
	requestClient := c.httpClientForTarget(target, networkTarget)
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		result, retry, err := c.fetchJSONAttempt(requestCtx, requestClient, method, requestURL, path, target.ManagementPassword, bodyBytes, responseTarget, attempt == 0)
		if err == nil || !retry {
			return result, err
		}
		lastErr = err
		if attempt == 0 {
			if err := sleepWithContext(requestCtx, retryBackoffDelay(attempt)); err != nil {
				return CLIProxyResponse{}, &CLIProxyClientError{Code: CLIProxyErrorTimeout, Path: path, Err: err}
			}
		}
	}
	if lastErr != nil {
		return CLIProxyResponse{}, lastErr
	}
	return CLIProxyResponse{}, &CLIProxyClientError{Code: CLIProxyErrorRequestFailed, Path: path}
}

func (c *CLIProxyClient) fetchJSONAttempt(ctx context.Context, client *http.Client, method string, requestURL string, managementPath string, managementPassword string, bodyBytes []byte, responseTarget any, allowRetry bool) (CLIProxyResponse, bool, error) {
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorRequestBuild, Path: managementPath, Err: err}
	}
	request.Header.Set("Accept", "application/json")
	if managementPassword != "" {
		request.Header.Set("X-Management-Key", managementPassword)
	}
	if len(bodyBytes) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		if isTimeoutError(ctx, err) {
			return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorTimeout, Path: managementPath, Err: err}
		}
		if allowRetry && isRetryableNetworkError(ctx, err) {
			return CLIProxyResponse{}, true, &CLIProxyClientError{Code: CLIProxyErrorRequestFailed, Path: managementPath, Err: err}
		}
		return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorRequestFailed, Path: managementPath, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusInternalServerError && allowRetry && method == http.MethodGet {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return CLIProxyResponse{StatusCode: response.StatusCode, Path: managementPath}, true, &CLIProxyClientError{Code: CLIProxyErrorUpstreamStatus, StatusCode: response.StatusCode, Path: managementPath}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorInvalidManagementAuth, StatusCode: response.StatusCode, Path: managementPath}
	}
	if response.StatusCode == http.StatusNotFound {
		return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorManagementDisabled, StatusCode: response.StatusCode, Path: managementPath}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorUpstreamStatus, StatusCode: response.StatusCode, Path: managementPath}
	}
	if responseTarget == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return CLIProxyResponse{StatusCode: response.StatusCode, Path: managementPath}, false, nil
	}
	body, err := readCappedBody(response.Body, c.responseBodyLimit())
	if err != nil {
		return CLIProxyResponse{}, false, err
	}
	if err := json.Unmarshal(body, responseTarget); err != nil {
		return CLIProxyResponse{}, false, &CLIProxyClientError{Code: CLIProxyErrorMalformedJSON, Path: managementPath, Err: err}
	}
	return CLIProxyResponse{StatusCode: response.StatusCode, Path: managementPath}, false, nil
}

func (c *CLIProxyClient) responseBodyLimit() int64 {
	if c == nil || c.bodyLimitBytes <= 0 {
		return defaultCLIProxyResponseBodyLimitBytes
	}
	return c.bodyLimitBytes
}

func (c *CLIProxyClient) httpClientForTarget(target CLIProxyTarget, networkTarget resolvedNetworkTarget) *http.Client {
	var client http.Client
	var baseTransport http.RoundTripper
	if c != nil && c.httpClient != nil {
		client = *c.httpClient
		baseTransport = c.httpClient.Transport
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	client.Transport = transportForTarget(target, baseTransport, networkTarget)
	return &client
}

func transportForTarget(target CLIProxyTarget, baseTransport http.RoundTripper, networkTarget resolvedNetworkTarget) http.RoundTripper {
	transport := cloneHTTPTransport(baseTransport)
	transport.Proxy = nil
	transport.DialTLSContext = nil
	transport.DialTLS = nil
	if target.SkipTLSVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if len(networkTarget.addresses) > 0 {
		dialer := &net.Dialer{}
		transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
			if !dialHostMatchesResolvedTarget(address, networkTarget) {
				return nil, fmt.Errorf("sidecar dial host %q did not match validated host %q", address, networkTarget.host)
			}
			return dialResolvedSidecarAddress(ctx, dialer, network, networkTarget)
		}
	}
	return transport
}

func cloneHTTPTransport(baseTransport http.RoundTripper) *http.Transport {
	if transport, ok := baseTransport.(*http.Transport); ok && transport != nil {
		return transport.Clone()
	}
	return http.DefaultTransport.(*http.Transport).Clone()
}

func normalizeCLIProxyManagementPath(rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" || strings.ContainsAny(trimmed, "?#") {
		return "", &CLIProxyClientError{Code: CLIProxyErrorUnsupportedPath, Err: errors.New("management path is not allowlisted")}
	}
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(trimmed, "/"))
	if _, ok := cliProxyManagementPathAllowlist[cleaned]; !ok {
		return "", &CLIProxyClientError{Code: CLIProxyErrorUnsupportedPath, Path: cleaned, Err: errors.New("management path is not allowlisted")}
	}
	return cleaned, nil
}

func buildCLIProxyManagementURL(baseURL string, managementPath string) (string, error) {
	parsed, err := urlpkg.Parse(baseURL)
	if err != nil {
		return "", &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Path: managementPath, Err: err}
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	parsed.Path = basePath + cliProxyManagementPrefixValue + managementPath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func marshalCLIProxyPayload(payload any) ([]byte, error) {
	if payload == nil {
		return nil, nil
	}
	return json.Marshal(payload)
}

func targetRequestTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = DefaultRequestTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func readCappedBody(body io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = defaultCLIProxyResponseBodyLimitBytes
	}
	payload, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, &CLIProxyClientError{Code: CLIProxyErrorReadFailed, Err: err}
	}
	if int64(len(payload)) > limit {
		return nil, &CLIProxyClientError{Code: CLIProxyErrorOversizedBody}
	}
	return payload, nil
}

type resolvedNetworkTarget struct {
	host      string
	port      string
	addresses []netip.Addr
}

func (c *CLIProxyClient) resolveNetworkTarget(ctx context.Context, baseURL string, policy CLIProxyConnectionPolicy) (resolvedNetworkTarget, error) {
	parsed, err := urlpkg.Parse(baseURL)
	if err != nil {
		return resolvedNetworkTarget{}, &CLIProxyClientError{Code: CLIProxyErrorInvalidBaseURL, Err: err}
	}
	host := parsed.Hostname()
	if err := validateSidecarHostPolicy(host, policy); err != nil {
		return resolvedNetworkTarget{}, err
	}
	port := parsed.Port()
	if port == "" {
		port = defaultPortForScheme(parsed.Scheme)
	}
	if addr, ok := parseSidecarHostAddress(host); ok {
		return resolvedNetworkTarget{host: normalizeSidecarDialHost(host), port: port, addresses: []netip.Addr{addr}}, nil
	}

	var resolver dnsResolver = net.DefaultResolver
	if c != nil && c.resolver != nil {
		resolver = c.resolver
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return resolvedNetworkTarget{}, &CLIProxyClientError{Code: CLIProxyErrorRequestFailed, Err: fmt.Errorf("resolve sidecar host %q: %w", host, err)}
	}
	resolved := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		addr, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		if err := sidecarAddressPolicyError(addr, policy); err != nil {
			return resolvedNetworkTarget{}, err
		}
		resolved = append(resolved, addr)
	}
	if len(resolved) == 0 {
		return resolvedNetworkTarget{}, &CLIProxyClientError{Code: CLIProxyErrorRequestFailed, Err: fmt.Errorf("resolve sidecar host %q returned no usable addresses", host)}
	}
	return resolvedNetworkTarget{host: normalizeSidecarDialHost(host), port: port, addresses: resolved}, nil
}

func defaultPortForScheme(scheme string) string {
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func isASCIIHost(host string) bool {
	for _, r := range host {
		if r > 127 {
			return false
		}
	}
	return true
}

func dialHostMatchesResolvedTarget(address string, target resolvedNetworkTarget) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return strings.EqualFold(normalizeSidecarDialHost(host), target.host)
}

func dialResolvedSidecarAddress(ctx context.Context, dialer *net.Dialer, network string, target resolvedNetworkTarget) (net.Conn, error) {
	var lastErr error
	for _, addr := range target.addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), target.port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("sidecar host has no validated addresses")
}

func validateSidecarHostPolicy(host string, policy CLIProxyConnectionPolicy) error {
	normalized := normalizeSidecarDialHost(host)
	if normalized == "" || normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") {
		if !policy.AllowPrivateNetwork {
			return sidecarPrivateNetworkFlagRequiredError()
		}
		return nil
	}
	addr, ok := parseSidecarHostAddress(normalized)
	if !ok {
		return nil
	}
	return sidecarAddressPolicyError(addr, policy)
}

func parseSidecarHostAddress(host string) (netip.Addr, bool) {
	normalized := normalizeSidecarDialHost(host)
	if zone := strings.Index(normalized, "%"); zone >= 0 {
		normalized = normalized[:zone]
	}
	addr, err := netip.ParseAddr(normalized)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func normalizeSidecarDialHost(host string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
}

func sidecarAddressPolicyError(addr netip.Addr, policy CLIProxyConnectionPolicy) error {
	addr = addr.Unmap()
	if isUnsafeSidecarAddress(addr) {
		return &CLIProxyClientError{Code: CLIProxyErrorPrivateNetworkBlocked, Err: errors.New("sidecar URL resolves to an unsafe network address")}
	}
	if !policy.AllowPrivateNetwork && isPrivateOrLoopbackAddress(addr) {
		return sidecarPrivateNetworkFlagRequiredError()
	}
	return nil
}

func sidecarPrivateNetworkFlagRequiredError() error {
	return &CLIProxyClientError{Code: CLIProxyErrorPrivateNetworkBlocked, Err: errors.New("allow_private_network is required for private sidecar URLs")}
}

func isUnsafeSidecarAddress(addr netip.Addr) bool {
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() || addr.IsMulticast() {
		return true
	}
	for _, prefix := range sidecarReservedTestNetPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func isPrivateOrLoopbackAddress(addr netip.Addr) bool {
	return addr.IsPrivate() || addr.IsLoopback()
}

func isTimeoutError(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func retryBackoffDelay(attempt int) time.Duration {
	delay := 250 * time.Millisecond
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= 2*time.Second {
			return 2 * time.Second
		}
	}
	if delay > 2*time.Second {
		return 2 * time.Second
	}
	return delay
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableNetworkError(ctx context.Context, err error) bool {
	if err == nil || (ctx != nil && ctx.Err() != nil) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return true
}
