package cors

import (
	"net/http"
	"strings"
)

type OriginProvider interface {
	CORSSnapshot() Snapshot
}

type Snapshot struct {
	allowedOrigins   []string
	allowedOriginSet map[string]struct{}
}

func NewSnapshot(origins []string) Snapshot {
	allowedOrigins := make([]string, 0, len(origins))
	allowedOriginSet := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		allowedOrigins = append(allowedOrigins, trimmed)
		allowedOriginSet[trimmed] = struct{}{}
	}
	return Snapshot{allowedOrigins: allowedOrigins, allowedOriginSet: allowedOriginSet}
}

func (s Snapshot) AllowedOrigins() []string {
	return append([]string(nil), s.allowedOrigins...)
}

func (s Snapshot) AllowedOriginSet() map[string]struct{} {
	result := make(map[string]struct{}, len(s.allowedOriginSet))
	for origin := range s.allowedOriginSet {
		result[origin] = struct{}{}
	}
	return result
}

func (s Snapshot) AllowsOrigin(origin string) bool {
	_, ok := s.allowedOriginSet[strings.TrimSpace(origin)]
	return ok
}

type StaticOriginProvider struct {
	snapshot Snapshot
}

func NewStaticOriginProvider(origins []string) StaticOriginProvider {
	return StaticOriginProvider{snapshot: NewSnapshot(origins)}
}

func (p StaticOriginProvider) CORSSnapshot() Snapshot {
	return p.snapshot
}

func ApplyAllowOriginHeaders(w http.ResponseWriter, r *http.Request, snapshot Snapshot) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || !snapshot.AllowsOrigin(origin) {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Vary", "Origin")
	return true
}
