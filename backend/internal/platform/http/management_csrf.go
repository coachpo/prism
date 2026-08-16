package platformhttp

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

const (
	problemCodeCrossOriginWriteBlocked = "management_cross_origin_write_blocked"
	problemCodeUnsupportedMediaType    = "management_unsupported_media_type"
)

// managementBrowserWriteGuard rejects any /api/* write request initiated from
// a third-party page. It is orthogonal to login authentication: even with
// operator auth off, blind browser writes must fail. It only inspects headers,
// never the body.
type managementBrowserWriteGuard struct {
	originProvider platformcors.OriginProvider
}

func newManagementBrowserWriteGuard(p platformcors.OriginProvider) *managementBrowserWriteGuard {
	return &managementBrowserWriteGuard{originProvider: p}
}

func (g *managementBrowserWriteGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isManagementWriteMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		snapshot := g.originProvider.CORSSnapshot()
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if !originMatchesRequestHost(origin, r.Host) && !snapshot.AllowsOrigin(origin) {
				responseutil.WriteProblem(w, r, snapshot, http.StatusForbidden,
					problemCodeCrossOriginWriteBlocked,
					"Cross-origin management writes are not allowed.", map[string]any{}, nil)
				return
			}
		} else if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site != "" && site != "same-origin" && site != "none" {
			responseutil.WriteProblem(w, r, snapshot, http.StatusForbidden,
				problemCodeCrossOriginWriteBlocked,
				"Cross-origin management writes are not allowed.", map[string]any{}, nil)
			return
		}
		// Writes with a body must be JSON: HTML forms cannot emit
		// application/json, and fetch/XHR sending application/json always
		// triggers a CORS preflight first.
		if r.ContentLength != 0 && !isJSONMediaType(r.Header.Get("Content-Type")) {
			responseutil.WriteProblem(w, r, snapshot, http.StatusUnsupportedMediaType,
				problemCodeUnsupportedMediaType,
				"Management writes require Content-Type: application/json.", map[string]any{}, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isManagementWriteMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func originMatchesRequestHost(origin string, requestHost string) bool {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.EqualFold(parsed.Host, strings.TrimSpace(requestHost))
}

func isJSONMediaType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(mediaType, "application/json")
}
