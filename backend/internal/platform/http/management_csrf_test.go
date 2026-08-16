package platformhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

func TestManagementBrowserWriteGuard(t *testing.T) {
	provider := platformcors.NewStaticOriginProvider([]string{"http://allowed.example"})
	guard := newManagementBrowserWriteGuard(provider)

	passThrough := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name          string
		method        string
		origin        string
		secFetchSite  string
		contentType   string
		contentLength int64
		wantStatus    int
	}{
		{"same-origin write passes", http.MethodPost, "http://prism.local", "", "application/json", 10, http.StatusOK},
		{"allowed CORS origin write passes", http.MethodPut, "http://allowed.example", "", "application/json", 10, http.StatusOK},
		{"cross-origin write blocked", http.MethodPost, "http://evil.example", "", "application/json", 10, http.StatusForbidden},
		{"cross-origin write without Origin uses Sec-Fetch-Site", http.MethodPost, "", "cross-site", "application/json", 10, http.StatusForbidden},
		{"same-site Sec-Fetch-Site passes", http.MethodPost, "", "same-origin", "application/json", 10, http.StatusOK},
		{"no origin and no sec-fetch-site passes", http.MethodPost, "", "", "application/json", 10, http.StatusOK},
		{"form content type rejected", http.MethodPost, "", "", "application/x-www-form-urlencoded", 10, http.StatusUnsupportedMediaType},
		{"bodyless write without content type passes", http.MethodDelete, "", "", "", 0, http.StatusOK},
		{"GET is not guarded", http.MethodGet, "http://evil.example", "", "", 0, http.StatusOK},
		{"OPTIONS is not guarded", http.MethodOptions, "http://evil.example", "", "", 0, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "http://prism.local/api/models/1", nil)
			request.Host = "prism.local"
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.secFetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", tc.secFetchSite)
			}
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			if tc.contentLength != 0 {
				request.ContentLength = tc.contentLength
			}
			recorder := httptest.NewRecorder()
			guard.Middleware(passThrough).ServeHTTP(recorder, request)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d (body %s)", tc.wantStatus, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestOriginMatchesRequestHost(t *testing.T) {
	cases := []struct {
		origin      string
		requestHost string
		want        bool
	}{
		{"http://prism.local", "prism.local", true},
		{"https://prism.local", "prism.local", true},
		{"http://PRISM.LOCAL:8080", "prism.local:8080", true},
		{"http://evil.example", "prism.local", false},
		{"not a url", "prism.local", false},
		{"ftp://prism.local", "prism.local", false},
		{"http://prism.local:8080", "prism.local:9090", false},
	}
	for _, tc := range cases {
		if got := originMatchesRequestHost(tc.origin, tc.requestHost); got != tc.want {
			t.Fatalf("originMatchesRequestHost(%q, %q) = %v, want %v", tc.origin, tc.requestHost, got, tc.want)
		}
	}
}

func TestIsJSONMediaType(t *testing.T) {
	for _, valid := range []string{"application/json", "application/json; charset=utf-8", " Application/JSON "} {
		if !isJSONMediaType(valid) {
			t.Fatalf("expected %q to be accepted as JSON", valid)
		}
	}
	for _, invalid := range []string{"", "text/plain", "application/x-www-form-urlencoded", "garbage"} {
		if isJSONMediaType(invalid) {
			t.Fatalf("expected %q to be rejected as non-JSON", invalid)
		}
	}
}
