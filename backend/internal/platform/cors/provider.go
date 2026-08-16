package cors

import (
	"net/http"
	"strings"
)

// IngressRequestIDHeader is exposed to configured CORS origins so standalone
// frontends can read the runtime ingress correlation ID from responses.
const IngressRequestIDHeader = "X-Prism-Ingress-Request-Id"

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
	w.Header().Set("Access-Control-Expose-Headers", IngressRequestIDHeader)
	MergeVary(w.Header(), "Origin")
	return true
}

// MergeVary appends field names to Vary without dropping fields installed by
// earlier middleware or emitting case-insensitive duplicates.
func MergeVary(header http.Header, fields ...string) {
	values := make([]string, 0, len(fields)+1)
	seen := make(map[string]struct{}, len(fields)+1)
	for _, line := range header.Values("Vary") {
		for token := range strings.SplitSeq(line, ",") {
			appendVaryToken(&values, seen, token)
		}
	}
	for _, field := range fields {
		appendVaryToken(&values, seen, field)
	}
	header.Del("Vary")
	if len(values) > 0 {
		header.Set("Vary", strings.Join(values, ", "))
	}
}

func appendVaryToken(values *[]string, seen map[string]struct{}, raw string) {
	token := strings.TrimSpace(raw)
	key := strings.ToLower(token)
	if token == "" {
		return
	}
	if key == "*" {
		*values = []string{"*"}
		clear(seen)
		seen[key] = struct{}{}
		return
	}
	if _, wildcard := seen["*"]; wildcard {
		return
	}
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*values = append(*values, token)
}
