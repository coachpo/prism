package auth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

func (s *Service) runtimeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !requiresRuntimeAuthHandling(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authSettings, err := s.loadRuntimeAuthSettings(r.Context())
		if err != nil {
			if isPublishedSnapshotUnavailable(err) {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
				return
			}
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusInternalServerError, "Failed to load authentication settings")
			return
		}
		authEnforced := authSettings.AuthEnabled

		rawKey, _ := extractProxyAPIKey(r.Header)
		if authEnforced && rawKey == "" {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Proxy API key required")
			return
		}
		if rawKey == "" {
			// Permissive mode with no credential: continue as none.
			attribution := requestcontext.RuntimeProxyKeyAttribution{
				State:        requestcontext.RuntimeProxyKeyNone,
				Snapshot:     nil,
				AuthEnforced: false,
			}
			next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
			return
		}

		proxyKey, verifyErr := s.verifyProxyAPIKey(r.Context(), rawKey)
		if verifyErr != nil {
			if authEnforced {
				// Once enforcement is known, any verifier/cache/database
				// failure must fail closed with one typed unavailable response.
				// Do not expose whether a particular key lookup failed.
				if isPublishedSnapshotUnavailable(verifyErr) {
					responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication snapshot is unavailable. Retry later.")
				} else {
					responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime authentication verifier is unavailable. Retry later.")
				}
				return
			}
			// Permissive optional verification failure: fail open for
			// execution, fail closed for identity. The request continues
			// as unknown; lookup details are never disclosed.
			slog.Warn("proxy key optional verification unavailable; attribution unknown", "error", verifyErr)
			attribution := requestcontext.RuntimeProxyKeyAttribution{
				State:        requestcontext.RuntimeProxyKeyUnknown,
				Snapshot:     nil,
				AuthEnforced: false,
			}
			next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
			return
		}
		if proxyKey == nil {
			if authEnforced {
				responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnauthorized, "Invalid proxy API key")
				return
			}
			// Permissive mode with an unrecognized/inactive/expired key:
			// continue as none; the caller is never told why.
			attribution := requestcontext.RuntimeProxyKeyAttribution{
				State:        requestcontext.RuntimeProxyKeyNone,
				Snapshot:     nil,
				AuthEnforced: false,
			}
			next.ServeHTTP(w, r.WithContext(requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)))
			return
		}

		proxyKeySnapshot := requestcontext.RuntimeProxyKeySnapshot{
			ID:         proxyKey.ID,
			Name:       proxyKey.Name,
			LastUsedAt: s.nowUTC(),
			LastUsedIP: requestIP(r),
		}
		attribution := requestcontext.RuntimeProxyKeyAttribution{
			State:        requestcontext.RuntimeProxyKeyIdentified,
			Snapshot:     &proxyKeySnapshot,
			AuthEnforced: authEnforced,
		}
		contextWithProxyKey := requestcontext.WithRuntimeProxyKeyAttribution(r.Context(), attribution)
		if s.runtimeCache == nil {
			if err := s.enqueueProxyAPIKeyUsage(proxyKey.ID, proxyKeySnapshot.LastUsedAt, proxyKeySnapshot.LastUsedIP); err != nil {
				slog.Error("failed to enqueue proxy API key usage", "error", err, "key_id", proxyKey.ID)
			}
		}
		next.ServeHTTP(w, r.WithContext(contextWithProxyKey))
	})
}

func requiresRuntimeAuthHandling(path string) bool {
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/")
}

func extractProxyAPIKey(header http.Header) (string, string) {
	authorization := strings.TrimSpace(header.Get("Authorization"))
	if authorization != "" {
		parts := strings.SplitN(authorization, " ", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1]), "authorization"
		}
	}
	for _, name := range []string{"X-API-Key", "X-Goog-Api-Key"} {
		value := strings.TrimSpace(header.Get(name))
		if value != "" {
			return value, strings.ToLower(name)
		}
	}
	return "", ""
}
