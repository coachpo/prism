// 本文件的快照在进程启动时从 config.Settings 投影一次,之后只读。
// R2 移除了编辑该配置文件的管理 API,也没有 file watcher——
// 改任何生效字段都需要重启进程。不要在这里加发布/替换路径。
package platformhttp

import (
	"math"
	"net/http"
	"strings"
	"time"

	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

type StartupConfigRuntime struct {
	snapshot *startupConfigRuntimeSnapshot // 构造后只读,进程内不再变更
}

type StartupConfigSnapshot struct {
	snapshot *startupConfigRuntimeSnapshot
}

type startupConfigRuntimeSnapshot struct {
	cors         platformcors.Snapshot
	auth         StartupAuthSnapshot
	runtimeProxy StartupRuntimeProxySnapshot
	admission    StartupAdmissionSnapshot
	alerting     StartupAlertingSnapshot
}

func NewStartupConfigRuntime(settings config.Settings) (*StartupConfigRuntime, error) {
	snapshot, err := buildStartupConfigRuntimeSnapshot(settings)
	if err != nil {
		return nil, err
	}
	return &StartupConfigRuntime{snapshot: snapshot}, nil
}

func (r *StartupConfigRuntime) Snapshot() StartupConfigSnapshot {
	if r == nil {
		return StartupConfigSnapshot{}
	}
	return StartupConfigSnapshot{snapshot: r.snapshot}
}

func (r *StartupConfigRuntime) CORSSnapshot() platformcors.Snapshot {
	return r.Snapshot().CORS()
}

func (r *StartupConfigRuntime) AuthSnapshot() StartupAuthSnapshot {
	return r.Snapshot().Auth()
}

func (r *StartupConfigRuntime) AuthRuntimeConfigSnapshot() managementauth.RuntimeAuthConfigSnapshot {
	return managementauth.RuntimeAuthConfigSnapshot(r.AuthSnapshot())
}

func (r *StartupConfigRuntime) RuntimeProxySnapshot() StartupRuntimeProxySnapshot {
	return r.Snapshot().RuntimeProxy()
}

func (r *StartupConfigRuntime) RuntimeProxyConfigSnapshot() runtimeapi.RuntimeProxyConfigSnapshot {
	runtimeProxy := r.RuntimeProxySnapshot()
	return runtimeapi.RuntimeProxyConfigSnapshot{HTTPClient: runtimeProxy.HTTPClient()}
}

func (r *StartupConfigRuntime) AdmissionSnapshot() StartupAdmissionSnapshot {
	return r.Snapshot().Admission()
}

func (r *StartupConfigRuntime) AlertingSnapshot() StartupAlertingSnapshot {
	return r.Snapshot().Alerting()
}

func (r *StartupConfigRuntime) AlertingWebhookURL() string {
	return r.AlertingSnapshot().WebhookURL()
}

func (s StartupConfigSnapshot) CORS() platformcors.Snapshot {
	if s.snapshot == nil {
		return platformcors.Snapshot{}
	}
	return s.snapshot.cors
}

func (s StartupConfigSnapshot) Auth() StartupAuthSnapshot {
	if s.snapshot == nil {
		return StartupAuthSnapshot{}
	}
	return s.snapshot.auth
}

func (s StartupConfigSnapshot) RuntimeProxy() StartupRuntimeProxySnapshot {
	if s.snapshot == nil {
		return StartupRuntimeProxySnapshot{}
	}
	return s.snapshot.runtimeProxy
}

func (s StartupConfigSnapshot) Admission() StartupAdmissionSnapshot {
	if s.snapshot == nil {
		return StartupAdmissionSnapshot{}
	}
	return s.snapshot.admission
}

func (s StartupConfigSnapshot) Alerting() StartupAlertingSnapshot {
	if s.snapshot == nil {
		return StartupAlertingSnapshot{}
	}
	return s.snapshot.alerting
}

func buildStartupConfigRuntimeSnapshot(settings config.Settings) (*startupConfigRuntimeSnapshot, error) {
	return &startupConfigRuntimeSnapshot{
		cors:         buildStartupCORSSnapshot(settings),
		auth:         buildStartupAuthSnapshot(settings),
		runtimeProxy: buildStartupRuntimeProxySnapshot(),
		admission:    buildStartupAdmissionSnapshot(settings),
		alerting:     buildStartupAlertingSnapshot(settings),
	}, nil
}

func buildStartupCORSSnapshot(settings config.Settings) platformcors.Snapshot {
	return platformcors.NewSnapshot(settings.CORSAllowedOriginsList())
}

type StartupAuthSnapshot = managementauth.RuntimeAuthConfigSnapshot

func buildStartupAuthSnapshot(settings config.Settings) StartupAuthSnapshot {
	return StartupAuthSnapshot{
		AccessTokenTTL:    time.Duration(settings.AuthAccessTokenTTLSeconds) * time.Second,
		RefreshTokenTTL:   time.Duration(settings.AuthRefreshTokenTTLSeconds) * time.Second,
		AccessCookieName:  strings.TrimSpace(settings.AuthCookieName),
		RefreshCookieName: strings.TrimSpace(settings.AuthRefreshCookieName),
		CookieSecure:      settings.AuthCookieSecure,
	}
}

type StartupRuntimeProxySnapshot struct {
	transport    *http.Transport
	roundTripper http.RoundTripper
}

type runtimeRoundTripper struct {
	transport http.RoundTripper
}

// buildStartupRuntimeProxySnapshot builds the outbound upstream transport. All
// connection counts, idle lifetimes, and timeouts were removed with
// runtime.transport: outbound requests are now subject to no connection or
// deadline limits. MaxIdleConnsPerHost is explicitly unlimited instead of
// leaving it at the Go default of 2 idle connections per host.
func buildStartupRuntimeProxySnapshot() StartupRuntimeProxySnapshot {
	transport := &http.Transport{
		DisableCompression:  true,
		MaxIdleConnsPerHost: math.MaxInt32,
	}
	return StartupRuntimeProxySnapshot{
		transport:    transport,
		roundTripper: &runtimeRoundTripper{transport: transport},
	}
}

func (s StartupRuntimeProxySnapshot) HTTPClient() *http.Client {
	if s.roundTripper == nil {
		return nil
	}
	return &http.Client{Transport: s.roundTripper}
}

func (t *runtimeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.transport.RoundTrip(request)
}

type StartupAdmissionSnapshot struct {
	limits     admission.Limits
	controller *admission.Controller
}

func buildStartupAdmissionSnapshot(settings config.Settings) StartupAdmissionSnapshot {
	warnIfManagementAdmissionClamped(settings)
	managementBudget := settings.ManagementAdmissionBudget()
	limits := admission.Limits{
		ManagementM1: managementM1AdmissionBudget(settings, managementBudget),
		ManagementM2: managementBudget.M2MaxConcurrent,
		ManagementM3: managementBudget.M3MaxConcurrent,
	}
	return StartupAdmissionSnapshot{limits: limits, controller: admission.NewController(limits)}
}

func (s StartupAdmissionSnapshot) Limits() admission.Limits {
	return s.limits
}

func (s StartupAdmissionSnapshot) Controller() *admission.Controller {
	return s.controller
}

type StartupAlertingSnapshot struct {
	webhookURL string
}

func buildStartupAlertingSnapshot(settings config.Settings) StartupAlertingSnapshot {
	return StartupAlertingSnapshot{webhookURL: strings.TrimSpace(settings.Alerting.WebhookURL)}
}

func (s StartupAlertingSnapshot) WebhookURL() string {
	return strings.TrimSpace(s.webhookURL)
}
