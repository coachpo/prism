package responseutil

import (
	"net/http"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

// SetPrivateNoStoreHeaders protects profile-scoped responses that may contain
// retained request or audit data. Vary covers the contract's auth/profile cache
// dimensions, including cookie-backed sessions, and preserves CORS-owned fields.
func SetPrivateNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	platformcors.MergeVary(w.Header(), "Authorization", "Cookie", profiledomain.ProfileIDHeader)
}
