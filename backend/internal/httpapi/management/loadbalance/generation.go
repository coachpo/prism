package loadbalance

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// readProfilePlanningGeneration returns the profile-scoped runtime-planning
// configuration generation. It is used to bind cursors (impact list and other
// read models) so a stale cursor cannot be reused across a changed
// configuration scope.
func readProfilePlanningGeneration(ctx context.Context, tx pgx.Tx, profileID int) (int64, error) {
	scope := runtimeapi.ProfileRuntimeGenerationScope(runtimeapi.RuntimeGenerationDomainRuntimePlanning, profileID)
	vector, err := runtimeapi.ReadRuntimeGenerationVector(ctx, tx, []runtimeapi.RuntimeGenerationScope{scope})
	if err != nil {
		return 0, fmt.Errorf("read planning generation for profile %d: %w", profileID, err)
	}
	key := scope.Domain + ":" + scope.ScopeType + ":" + scope.ScopeID
	generation, ok := vector[key]
	if !ok {
		return 0, nil
	}
	return generation, nil
}
