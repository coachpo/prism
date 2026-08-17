package settings

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
)

type corsSnapshot = platformcors.Snapshot

// retentionService implements the retention policy, destructive preflight,
// owner-drift archive and manual job surfaces (Settings SPEC §5/§6).
type retentionService struct {
	pool *pgxpool.Pool
	now  func() time.Time
	jobs *managementjobs.Store
}

// ---- GET /api/settings/log-retention ----

// ---- PUT /api/settings/log-retention ----

// ---- owner-drift archive ----

// ---- preflight ----

// ---- manual job creation (sealed intent) ----

// ---- operation outcome store helpers ----

// ---- error helpers ----
