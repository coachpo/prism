package platformhttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	managementaudit "github.com/coachpo/prism/backend/internal/httpapi/management/audit"
	managementauth "github.com/coachpo/prism/backend/internal/httpapi/management/auth"
	managementbootstrapconfig "github.com/coachpo/prism/backend/internal/httpapi/management/bootstrapconfig"
	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	managementconfigrules "github.com/coachpo/prism/backend/internal/httpapi/management/configrules"
	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementendpoints "github.com/coachpo/prism/backend/internal/httpapi/management/endpoints"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	managementmodels "github.com/coachpo/prism/backend/internal/httpapi/management/models"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	managementsettings "github.com/coachpo/prism/backend/internal/httpapi/management/settings"
	managementstats "github.com/coachpo/prism/backend/internal/httpapi/management/stats"
	managementvendors "github.com/coachpo/prism/backend/internal/httpapi/management/vendors"
	"github.com/coachpo/prism/backend/internal/httpapi/openapi"
	realtimeapi "github.com/coachpo/prism/backend/internal/httpapi/realtime"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/admission"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformdb "github.com/coachpo/prism/backend/internal/platform/db"
	"github.com/coachpo/prism/backend/internal/platform/email"
	"github.com/coachpo/prism/backend/internal/platform/priority"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

func TestManagementRouteSpecClassification(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		method string
		path   string
		want   priority.ManagementTier
		ok     bool
	}{
		{name: "protected auth route", method: http.MethodGet, path: "/api/auth/status", want: priority.ManagementTierM1, ok: true},
		{name: "protected profile activation route", method: http.MethodPost, path: "/api/profiles/42/activate", want: priority.ManagementTierM1, ok: true},
		{name: "general management route has explicit m2 tier", method: http.MethodGet, path: "/api/settings/auth/proxy-keys", want: priority.ManagementTierM2, ok: true},
		{name: "connection batch read uses m2 tier", method: http.MethodPost, path: "/api/models/connections/batch", want: priority.ManagementTierM2, ok: true},
		{name: "first shed stats route", method: http.MethodGet, path: "/api/stats/summary", want: priority.ManagementTierM3, ok: true},
		{name: "trimmed mounted path still matches", method: http.MethodGet, path: "/realtime/ws", want: priority.ManagementTierM3, ok: true},
		{name: "head maps to get", method: http.MethodHead, path: "/api/profiles/active", want: priority.ManagementTierM1, ok: true},
		{name: "options bypasses admission", method: http.MethodOptions, path: "/api/profiles", ok: false},
		{name: "unknown management path stays unadmitted for router 404", method: http.MethodGet, path: "/api/not-mounted", ok: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := matchManagementRouteSpec(testCase.method, testCase.path)
			if ok != testCase.ok {
				t.Fatalf("matchManagementRouteSpec(%q, %q) ok = %v, want %v", testCase.method, testCase.path, ok, testCase.ok)
			}
			if ok && got.tier != testCase.want {
				t.Fatalf("matchManagementRouteSpec(%q, %q) tier = %v, want %v", testCase.method, testCase.path, got.tier, testCase.want)
			}
		})
	}
}

func TestManagementAdmissionControllerFastFailsLowerPriorityRoutes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		method   string
		path     string
		m2Budget int64
		m3Budget int64
		holdM2   int64
		holdM3   int64
	}{
		{name: "m2 rejects when shared management lane is full", method: http.MethodGet, path: "/api/models", m2Budget: 1, m3Budget: 1, holdM2: 1},
		{name: "m3 rejects when first shed lane is full", method: http.MethodGet, path: "/api/stats/summary", m2Budget: 2, m3Budget: 1, holdM3: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: testCase.m2Budget, ManagementM3: testCase.m3Budget})}

			var releases []func()
			defer func() {
				for idx := len(releases) - 1; idx >= 0; idx-- {
					releases[idx]()
				}
			}()
			for range testCase.holdM2 {
				_, release, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M2", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2}, Timeout: time.Second})
				if err != nil {
					t.Fatalf("expected to pre-acquire the shared M2 lane: %v", err)
				}
				releases = append(releases, release)
			}
			for range testCase.holdM3 {
				_, release, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
				if err != nil {
					t.Fatalf("expected to pre-acquire the M3 first-shed lane: %v", err)
				}
				releases = append(releases, release)
			}

			handlerCalled := false
			handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusNoContent)
			}))

			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			response := httptest.NewRecorder()
			startedAt := time.Now()
			handler.ServeHTTP(response, request)

			if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
				t.Fatalf("expected fast-fail in <= 500ms, got %s", elapsed)
			}
			if handlerCalled {
				t.Fatal("expected saturated lower-priority route to reject before hitting the handler")
			}
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503 overload response, got %d", response.Code)
			}
			if retryAfter := response.Header().Get("Retry-After"); retryAfter != "1" {
				t.Fatalf("expected Retry-After header to be 1, got %q", retryAfter)
			}
		})
	}
}

func TestConnectionBatchAdmissionBypassesM3Saturation(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})}
	_, releaseM3, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected to pre-acquire the M3 first-shed lane: %v", err)
	}
	defer releaseM3()

	handlerCalled := false
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		metadata, err := priority.RequireMetadata(r.Context())
		if err != nil {
			t.Fatalf("expected priority metadata in request context: %v", err)
		}
		if metadata.ManagementTier != priority.ManagementTierM2 {
			t.Fatalf("expected connection batch to use M2, got %+v", metadata)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/models/connections/batch", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled {
		t.Fatal("expected connection batch read to bypass saturated M3 admission")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected connection batch read to reach handler, got %d", response.Code)
	}
}

func TestManagementAdmissionControllerKeepsProtectedRoutesIsolated(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})}
	_, releaseM3, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M3", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM3}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected to pre-acquire the M3 first-shed lane: %v", err)
	}
	defer releaseM3()
	_, releaseM2, err := controller.controller.Admit(context.Background(), admission.Spec{Name: "held M2", Metadata: priority.Metadata{Priority: priority.PriorityManagement, ManagementTier: priority.ManagementTierM2}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("expected to pre-acquire the shared M2 lane: %v", err)
	}
	defer releaseM2()

	handlerCalled := false
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/profiles/active", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if !handlerCalled {
		t.Fatal("expected protected M1 route to use capacity isolated from lower-priority admission caps")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected protected route to reach handler, got %d", response.Code)
	}
}

func TestManagementAdmissionUsesPublishedHotLimitsWithoutBlockingInflightRelease(t *testing.T) {
	settings := config.Settings{
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 3},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 1, M3MaxConcurrent: 1},
	}
	runtime, err := NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}
	controller := &managementAdmissionController{controller: newHTTPAdmissionController(settings), provider: runtime}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Request") == "first" {
			close(firstEntered)
			<-releaseFirst
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	firstDone := make(chan int, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/api/models", nil)
		request.Header.Set("X-Test-Request", "first")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		firstDone <- response.Code
	}()
	<-firstEntered

	updated := settings
	updated.ManagementAdmissionControlBudget = config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 1}
	retired, err := runtime.Publish(updated)
	if err != nil {
		t.Fatalf("publish updated admission limits: %v", err)
	}
	retired.CloseIdleConnections()

	secondRequest := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusNoContent {
		t.Fatalf("expected request under published admission limits to proceed, got %d", secondResponse.Code)
	}

	close(releaseFirst)
	if firstCode := <-firstDone; firstCode != http.StatusNoContent {
		t.Fatalf("expected in-flight request to release through old controller, got %d", firstCode)
	}
}

func TestAdmissionAttachesServerSideWorkloadAndIgnoresPriorityHeaders(t *testing.T) {
	t.Parallel()

	controller := &managementAdmissionController{controller: admission.NewController(admission.Limits{ManagementM1: 1, ManagementM2: 2, ManagementM3: 1})}
	handler := controller.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata, err := priority.RequireMetadata(r.Context())
		if err != nil {
			t.Fatalf("expected priority metadata in request context: %v", err)
		}
		if metadata.Priority != priority.PriorityManagement || metadata.ManagementTier != priority.ManagementTierM3 {
			t.Fatalf("expected server-side M3 management metadata, got %+v", metadata)
		}
		workload, err := admission.RequireWorkload(r.Context())
		if err != nil {
			t.Fatalf("expected admitted workload context: %v", err)
		}
		if workload.Name != "stats summary" {
			t.Fatalf("expected route spec workload name, got %q", workload.Name)
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("expected admitted management request to have a deadline")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/stats/summary", nil)
	request.Header.Set("X-Prism-Priority", "proxy")
	request.Header.Set("X-Management-Tier", "M1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected admitted request to reach handler, got %d", response.Code)
	}
}

func TestProxyAdmissionAttachesServerSideWorkload(t *testing.T) {
	t.Parallel()

	controller := admission.NewController(admission.Limits{Proxy: 1})
	handler := proxyAdmissionMiddleware(controller, time.Minute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata, err := priority.RequireMetadata(r.Context())
		if err != nil {
			t.Fatalf("expected proxy priority metadata: %v", err)
		}
		if metadata.Priority != priority.PriorityProxy {
			t.Fatalf("expected proxy priority, got %+v", metadata)
		}
		workload, err := admission.RequireWorkload(r.Context())
		if err != nil {
			t.Fatalf("expected proxy workload context: %v", err)
		}
		if workload.Name != "runtime proxy" {
			t.Fatalf("expected runtime proxy workload, got %q", workload.Name)
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("expected proxy request deadline")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("X-Prism-Priority", "background")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected proxy admission to reach handler, got %d", response.Code)
	}
}

func TestNewHandlerWithDependenciesMountsBaselineRoutes(t *testing.T) {
	t.Parallel()

	openAPIDocument, err := openapi.Load()
	if err != nil {
		t.Fatalf("load OpenAPI document: %v", err)
	}
	settings := config.Settings{
		Host:                             "127.0.0.1",
		Port:                             18000,
		AppEnv:                           config.EnvironmentDevelopment,
		RuntimeTransportConfig:           config.RuntimeTransportConfig{RequestTimeout: time.Second},
		ManagementDatabasePoolBudget:     config.DatabasePoolBudget{MaxConns: 4},
		ManagementAdmissionControlBudget: config.ManagementAdmissionBudget{M2MaxConcurrent: 2, M3MaxConcurrent: 1},
	}
	handler, err := NewHandlerWithDependencies(settings, Dependencies{
		Version:                "route-assembly-test",
		OpenAPI:                openAPIDocument,
		DatabasePools:          &platformdb.DatabasePools{},
		AuthService:            &managementauth.Service{},
		BootstrapConfigService: &managementbootstrapconfig.Service{},
		ProfilesService:        &managementprofiles.Service{},
		RealtimeService:        &realtimeapi.Service{},
		RuntimeService:         &runtimeapi.Service{},
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	router, ok := handler.(*chi.Mux)
	if !ok {
		t.Fatalf("expected handler to be chi mux, got %T", handler)
	}

	for _, route := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/health"},
		{method: http.MethodGet, path: "/metrics"},
		{method: http.MethodGet, path: "/openapi.json"},
		{method: http.MethodGet, path: "/docs"},
		{method: http.MethodGet, path: "/redoc"},
		{method: http.MethodGet, path: "/api/auth/status"},
		{method: http.MethodGet, path: "/api/profiles/active"},
		{method: http.MethodGet, path: "/api/config/bootstrap"},
		{method: http.MethodGet, path: "/api/realtime/ws"},
		{method: http.MethodPost, path: "/v1/chat/completions"},
		{method: http.MethodPost, path: "/v1/messages"},
		{method: http.MethodPost, path: "/v1beta/models/gemini-pro:generateContent"},
	} {
		assertRouteMounted(t, router, route.method, route.path)
	}
}

func assertRouteMounted(t *testing.T, router *chi.Mux, method string, path string) {
	t.Helper()
	routeContext := chi.NewRouteContext()
	if !router.Match(routeContext, method, path) {
		t.Fatalf("expected route %s %s to be mounted", method, path)
	}
}

func TestManagementRouteSpecsCoverMountedRoutes(t *testing.T) {
	t.Parallel()

	managementRouter, ok := NewManagementRouter(
		&managementaudit.Service{},
		&managementauth.Service{},
		&managementbootstrapconfig.Service{},
		&managementconfigbundle.Service{},
		&managementconfigrules.Service{},
		&managementconnections.Service{},
		&managementendpoints.Service{},
		&managementloadbalance.Service{},
		&managementmodels.Service{},
		&managementprofiles.Service{},
		&realtimeapi.Service{},
		&managementsettings.Service{},
		&managementstats.Service{},
		&managementvendors.Service{},
	).(*chi.Mux)
	if !ok {
		t.Fatal("expected management router to be a chi mux")
	}

	specs := make(map[string]struct{}, len(managementRouteSpecs))
	for _, routeSpec := range managementRouteSpecs {
		if routeSpec.tier == "" {
			t.Fatalf("route spec %q has no tier", routeSpec.name)
		}
		specs[routeKey(routeSpec.method, routeSpec.pattern)] = struct{}{}
	}

	walkErr := chi.Walk(managementRouter, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodHead || method == http.MethodOptions {
			return nil
		}
		key := routeKey(method, route)
		if _, ok := specs[key]; !ok {
			t.Fatalf("mounted management route %s %s has no admission route spec", method, route)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk management routes: %v", walkErr)
	}
}

func routeKey(method string, route string) string {
	return strings.ToUpper(method) + " " + normalizeManagementRoutePath(route)
}

func TestClassifyRuntimeCacheInvalidation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		method    string
		path      string
		profileID string
		want      runtimeCacheInvalidationAction
	}{
		{name: "auth settings write invalidates runtime auth", method: http.MethodPut, path: "/api/settings/auth", want: runtimeCacheInvalidationAction{auth: true}},
		{name: "proxy key patch invalidates runtime auth", method: http.MethodPatch, path: "/api/settings/auth/proxy-keys/7", want: runtimeCacheInvalidationAction{auth: true}},
		{name: "profile activation invalidates active profile", method: http.MethodPost, path: "/api/profiles/7/activate", want: runtimeCacheInvalidationAction{activeProfile: true}},
		{name: "costing write invalidates one planning snapshot", method: http.MethodPut, path: "/api/settings/costing", profileID: "42", want: runtimeCacheInvalidationAction{planningIDs: []int{42}}},
		{name: "vendor write invalidates all planning snapshots", method: http.MethodPatch, path: "/api/vendors/9", want: runtimeCacheInvalidationAction{planningAll: true}},
		{name: "preview route stays read only", method: http.MethodPost, path: "/api/config/profile/import/preview", profileID: "42", want: runtimeCacheInvalidationAction{}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			if testCase.profileID != "" {
				header.Set(profiledomain.ProfileIDHeader, testCase.profileID)
			}
			got := classifyRuntimeCacheInvalidation(testCase.method, testCase.path, header)
			if got.auth != testCase.want.auth || got.activeProfile != testCase.want.activeProfile || got.planningAll != testCase.want.planningAll || !reflect.DeepEqual(got.planningIDs, testCase.want.planningIDs) {
				t.Fatalf("classifyRuntimeCacheInvalidation(%q, %q) = %+v, want %+v", testCase.method, testCase.path, got, testCase.want)
			}
		})
	}
}

func newAuthMailer(settings config.Settings) (managementauth.Mailer, error) {
	mailer, _, err := email.NewMailer(settings.Mail)
	if err != nil {
		return nil, fmt.Errorf("create auth mailer: %w", err)
	}
	return mailer, nil
}

func TestNewAuthMailerMissingAndDisabledMailUseNoopCompatibleMailer(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		settings config.Settings
	}{
		{name: "missing mail config", settings: config.Settings{}},
		{name: "disabled mail ignores stale smtp fields", settings: config.Settings{Mail: config.MailConfig{
			Enabled: false,
			From:    "not an address",
			SMTP: config.MailSMTPConfig{
				Host:     "192.0.2.1",
				Port:     587,
				Mode:     config.MailSMTPModeStartTLSRequired,
				Auth:     config.MailSMTPAuthPlain,
				Username: "smtp-user",
				Password: "disabled-smtp-password",
				Timeout:  0,
			},
		}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mailer, err := newAuthMailer(testCase.settings)
			if err != nil {
				t.Fatalf("create auth mailer: %v", err)
			}
			if _, ok := mailer.(email.DisabledMailer); !ok {
				t.Fatalf("expected disabled auth mailer, got %T", mailer)
			}
			if err := mailer.SendEmailVerificationOTP(context.Background(), "operator@example.com", "123456"); err != nil {
				t.Fatalf("disabled verification send returned error: %v", err)
			}
			if err := mailer.SendPasswordResetEmail(context.Background(), "operator@example.com", "654321"); err != nil {
				t.Fatalf("disabled password reset send returned error: %v", err)
			}
		})
	}
}

func TestNewAuthMailerEnabledSMTPConstructsWithoutDialing(t *testing.T) {
	t.Parallel()

	mailer, err := newAuthMailer(config.Settings{Mail: validSMTPMailConfig()})
	if err != nil {
		t.Fatalf("create auth mailer: %v", err)
	}
	if _, ok := mailer.(*email.SMTPMailer); !ok {
		t.Fatalf("expected SMTP auth mailer, got %T", mailer)
	}
}

func TestNewAuthMailerEnabledSMTPConstructionErrorIsRedacted(t *testing.T) {
	t.Parallel()

	mailConfig := validSMTPMailConfig()
	mailConfig.SMTP.Auth = config.MailSMTPAuthPlain
	mailConfig.SMTP.Username = "smtp-user"
	mailConfig.SMTP.Password = "super-secret-smtp-password"
	mailConfig.SMTP.Timeout = 0

	_, err := newAuthMailer(config.Settings{Mail: mailConfig})
	if err == nil {
		t.Fatal("expected invalid enabled SMTP settings to fail auth mailer construction")
	}
	errorText := err.Error()
	if !strings.Contains(errorText, "create auth mailer") {
		t.Fatalf("expected startup context in error, got %q", errorText)
	}
	for _, forbidden := range []string{
		"super-secret-smtp-password",
		"123456",
		"https://prism.example/reset?token=reset-secret",
		"Use this code to reset your Prism password",
	} {
		if strings.Contains(errorText, forbidden) {
			t.Fatalf("expected auth mailer error to redact %q, got %q", forbidden, errorText)
		}
	}
}

func validSMTPMailConfig() config.MailConfig {
	return config.MailConfig{
		Enabled: true,
		From:    "Prism <noreply@example.com>",
		ReplyTo: "Support <support@example.com>",
		SMTP: config.MailSMTPConfig{
			Host:          "127.0.0.1",
			Port:          2525,
			Mode:          config.MailSMTPModePlaintextLocalOnly,
			EHLOHostname:  "prism.test",
			Auth:          config.MailSMTPAuthNone,
			Timeout:       2 * time.Second,
			TLSServerName: "localhost",
		},
	}
}

func TestCORSMiddlewareUsesPublishedRuntimeOrigins(t *testing.T) {
	settings := config.Settings{
		AppEnv:             config.EnvironmentProduction,
		CORSAllowedOrigins: "https://old.example.test",
	}
	runtime, err := NewHotBootstrapConfigRuntime(settings)
	if err != nil {
		t.Fatalf("create hot bootstrap runtime: %v", err)
	}
	handler, err := NewHandlerWithDependencies(settings, Dependencies{Version: "cors-runtime-test", HotBootstrapConfigRuntime: runtime})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	assertCORSOrigin(t, handler, "https://old.example.test", "https://old.example.test")

	updated := settings
	updated.CORSAllowedOrigins = "https://new.example.test"
	retired, err := runtime.Publish(updated)
	if err != nil {
		t.Fatalf("publish CORS runtime update: %v", err)
	}
	retired.CloseIdleConnections()

	assertCORSOrigin(t, handler, "https://new.example.test", "https://new.example.test")
	assertCORSOrigin(t, handler, "https://old.example.test", "")
}

func assertCORSOrigin(t *testing.T, handler http.Handler, origin string, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.Header.Set("Origin", origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected health response status 200, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != want {
		t.Fatalf("Access-Control-Allow-Origin for %q = %q, want %q", origin, got, want)
	}
}
