package platformhttp

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type HotBootstrapConfigRuntime struct {
	snapshot atomic.Pointer[hotBootstrapConfigRuntimeSnapshot]
}

type HotBootstrapConfigSnapshot struct {
	snapshot *hotBootstrapConfigRuntimeSnapshot
}

type HotBootstrapConfigRetiredResources struct {
	snapshot *hotBootstrapConfigRuntimeSnapshot
}

type hotBootstrapConfigRuntimeSnapshot struct {
	cors         platformcors.Snapshot
	auth         HotAuthRuntimeSnapshot
	runtimeProxy HotRuntimeProxySnapshot
	admission    HotAdmissionSnapshot
	alerting     HotAlertingSnapshot
}

func NewHotBootstrapConfigRuntime(settings config.Settings) (*HotBootstrapConfigRuntime, error) {
	snapshot, err := buildHotBootstrapConfigRuntimeSnapshot(settings)
	if err != nil {
		return nil, err
	}
	runtime := &HotBootstrapConfigRuntime{}
	runtime.snapshot.Store(snapshot)
	return runtime, nil
}

func (r *HotBootstrapConfigRuntime) Snapshot() HotBootstrapConfigSnapshot {
	if r == nil {
		return HotBootstrapConfigSnapshot{}
	}
	return HotBootstrapConfigSnapshot{snapshot: r.snapshot.Load()}
}

func (r *HotBootstrapConfigRuntime) Validate(settings config.Settings) error {
	candidate, err := buildHotBootstrapConfigRuntimeSnapshot(settings)
	if err != nil {
		return err
	}
	candidate.closeIdleConnections()
	return nil
}

// ponytail: publish has no management API caller after R2; keep it until the hot runtime interface is pruned.
func (r *HotBootstrapConfigRuntime) Publish(settings config.Settings) (config.BootstrapConfigHotApplyRetiredResources, error) {
	if r == nil {
		return HotBootstrapConfigRetiredResources{}, fmt.Errorf("hot bootstrap config runtime is nil")
	}
	nextSnapshot, err := buildHotBootstrapConfigRuntimeSnapshot(settings)
	if err != nil {
		return HotBootstrapConfigRetiredResources{}, err
	}
	previous := r.snapshot.Swap(nextSnapshot)
	return HotBootstrapConfigRetiredResources{snapshot: previous}, nil
}

func (r *HotBootstrapConfigRuntime) CORSSnapshot() platformcors.Snapshot {
	return r.Snapshot().CORS()
}

func (r *HotBootstrapConfigRuntime) AuthSnapshot() HotAuthRuntimeSnapshot {
	return r.Snapshot().Auth()
}

func (r *HotBootstrapConfigRuntime) AuthRuntimeConfigSnapshot() managementauth.RuntimeAuthConfigSnapshot {
	return managementauth.RuntimeAuthConfigSnapshot(r.AuthSnapshot())
}

func (r *HotBootstrapConfigRuntime) RuntimeProxySnapshot() HotRuntimeProxySnapshot {
	return r.Snapshot().RuntimeProxy()
}

func (r *HotBootstrapConfigRuntime) RuntimeProxyConfigSnapshot() runtimeapi.RuntimeProxyConfigSnapshot {
	runtimeProxy := r.RuntimeProxySnapshot()
	return runtimeapi.RuntimeProxyConfigSnapshot{HTTPClient: runtimeProxy.HTTPClient()}
}

func (r *HotBootstrapConfigRuntime) AdmissionSnapshot() HotAdmissionSnapshot {
	return r.Snapshot().Admission()
}

func (r *HotBootstrapConfigRuntime) AlertingSnapshot() HotAlertingSnapshot {
	return r.Snapshot().Alerting()
}

func (r *HotBootstrapConfigRuntime) AlertingWebhookURL() string {
	return r.AlertingSnapshot().WebhookURL()
}

func (r HotBootstrapConfigRetiredResources) CloseIdleConnections() {
	if r.snapshot == nil {
		return
	}
	r.snapshot.closeIdleConnections()
}

func (s HotBootstrapConfigSnapshot) CORS() platformcors.Snapshot {
	if s.snapshot == nil {
		return platformcors.Snapshot{}
	}
	return s.snapshot.cors
}

func (s HotBootstrapConfigSnapshot) Auth() HotAuthRuntimeSnapshot {
	if s.snapshot == nil {
		return HotAuthRuntimeSnapshot{}
	}
	return s.snapshot.auth
}

func (s HotBootstrapConfigSnapshot) RuntimeProxy() HotRuntimeProxySnapshot {
	if s.snapshot == nil {
		return HotRuntimeProxySnapshot{}
	}
	return s.snapshot.runtimeProxy
}

func (s HotBootstrapConfigSnapshot) Admission() HotAdmissionSnapshot {
	if s.snapshot == nil {
		return HotAdmissionSnapshot{}
	}
	return s.snapshot.admission
}

func (s HotBootstrapConfigSnapshot) Alerting() HotAlertingSnapshot {
	if s.snapshot == nil {
		return HotAlertingSnapshot{}
	}
	return s.snapshot.alerting
}

func (s *hotBootstrapConfigRuntimeSnapshot) closeIdleConnections() {
	if s == nil {
		return
	}
	s.runtimeProxy.closeIdleConnections()
}

func buildHotBootstrapConfigRuntimeSnapshot(settings config.Settings) (*hotBootstrapConfigRuntimeSnapshot, error) {
	return &hotBootstrapConfigRuntimeSnapshot{
		cors:         buildHotCORSSnapshot(settings),
		auth:         buildHotAuthRuntimeSnapshot(settings),
		runtimeProxy: buildHotRuntimeProxySnapshot(settings),
		admission:    buildHotAdmissionSnapshot(settings),
		alerting:     buildHotAlertingSnapshot(settings),
	}, nil
}

type HotCORSSnapshot = platformcors.Snapshot

func buildHotCORSSnapshot(settings config.Settings) platformcors.Snapshot {
	return platformcors.NewSnapshot(settings.CORSAllowedOriginsList())
}

type HotAuthRuntimeSnapshot = managementauth.RuntimeAuthConfigSnapshot

func buildHotAuthRuntimeSnapshot(settings config.Settings) HotAuthRuntimeSnapshot {
	return HotAuthRuntimeSnapshot{
		AccessTokenTTL:    time.Duration(settings.AuthAccessTokenTTLSeconds) * time.Second,
		RefreshTokenTTL:   time.Duration(settings.AuthRefreshTokenTTLSeconds) * time.Second,
		AccessCookieName:  strings.TrimSpace(settings.AuthCookieName),
		RefreshCookieName: strings.TrimSpace(settings.AuthRefreshCookieName),
		CookieSecure:      settings.AuthCookieSecure,
	}
}

type HotRuntimeProxySnapshot struct {
	transportConfig config.RuntimeTransportConfig
	transport       *http.Transport
	roundTripper    http.RoundTripper
}

type hotRuntimeRoundTripper struct {
	transport http.RoundTripper
}

func buildHotRuntimeProxySnapshot(settings config.Settings) HotRuntimeProxySnapshot {
	transportConfig := settings.RuntimeTransport()
	transport := &http.Transport{
		DisableCompression:    true,
		MaxIdleConns:          transportConfig.MaxIdleConns,
		MaxIdleConnsPerHost:   transportConfig.MaxIdleConnsPerHost,
		MaxConnsPerHost:       transportConfig.MaxConnsPerHost,
		IdleConnTimeout:       transportConfig.IdleConnTimeout,
		ResponseHeaderTimeout: transportConfig.ResponseHeaderTimeout,
		TLSHandshakeTimeout:   transportConfig.TLSHandshakeTimeout,
		ExpectContinueTimeout: transportConfig.ExpectContinueTimeout,
	}
	return HotRuntimeProxySnapshot{
		transportConfig: transportConfig,
		transport:       transport,
		roundTripper:    &hotRuntimeRoundTripper{transport: transport},
	}
}

func (s HotRuntimeProxySnapshot) TransportConfig() config.RuntimeTransportConfig {
	return s.transportConfig
}

func (s HotRuntimeProxySnapshot) HTTPClient() *http.Client {
	if s.roundTripper == nil {
		return nil
	}
	return &http.Client{Timeout: s.transportConfig.RequestTimeout, Transport: s.roundTripper}
}

func (s HotRuntimeProxySnapshot) CloseIdleConnections() {
	s.closeIdleConnections()
}

func (s HotRuntimeProxySnapshot) closeIdleConnections() {
	if s.transport == nil {
		return
	}
	s.transport.CloseIdleConnections()
}

func (t *hotRuntimeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.transport.RoundTrip(request)
}

func (t *hotRuntimeRoundTripper) CloseIdleConnections() {
	if closer, ok := t.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type HotAdmissionSnapshot struct {
	limits     admission.Limits
	controller *admission.Controller
}

func buildHotAdmissionSnapshot(settings config.Settings) HotAdmissionSnapshot {
	warnIfManagementAdmissionClamped(settings)
	managementBudget := settings.ManagementAdmissionBudget()
	limits := admission.Limits{
		ManagementM1: managementM1AdmissionBudget(settings, managementBudget),
		ManagementM2: managementBudget.M2MaxConcurrent,
		ManagementM3: managementBudget.M3MaxConcurrent,
	}
	return HotAdmissionSnapshot{limits: limits, controller: admission.NewController(limits)}
}

func (s HotAdmissionSnapshot) Limits() admission.Limits {
	return s.limits
}

func (s HotAdmissionSnapshot) Controller() *admission.Controller {
	return s.controller
}

type HotAlertingSnapshot struct {
	webhookURL string
}

func buildHotAlertingSnapshot(settings config.Settings) HotAlertingSnapshot {
	return HotAlertingSnapshot{webhookURL: strings.TrimSpace(settings.Alerting.WebhookURL)}
}

func (s HotAlertingSnapshot) WebhookURL() string {
	return strings.TrimSpace(s.webhookURL)
}
