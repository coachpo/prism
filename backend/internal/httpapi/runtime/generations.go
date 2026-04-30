package runtime

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	RuntimeGenerationDomainAuth            = "auth"
	RuntimeGenerationDomainRuntimePlanning = "runtime_planning"
	RuntimeGenerationDomainProfileRuntime  = "profile_runtime"
	RuntimeGenerationDomainModelCatalog    = "model_catalog"
)

const (
	runtimeGenerationScopeGlobal  = "global"
	runtimeGenerationScopeProfile = "profile"
	runtimeGenerationGlobalID     = "*"
)

var ErrRuntimeSnapshotGenerationChanged = errors.New("runtime snapshot generation changed during build")

type RuntimeGenerationScope struct {
	Domain    string
	ScopeType string
	ScopeID   string
}

type RuntimeGenerationBump struct {
	Scope     RuntimeGenerationScope
	UpdatedBy string
	Reason    string
}

type RuntimeGenerationVector map[string]int64

func DefaultRuntimeGenerationScopes() []RuntimeGenerationScope {
	return []RuntimeGenerationScope{
		GlobalRuntimeGenerationScope(RuntimeGenerationDomainAuth),
		GlobalRuntimeGenerationScope(RuntimeGenerationDomainRuntimePlanning),
		GlobalRuntimeGenerationScope(RuntimeGenerationDomainProfileRuntime),
		GlobalRuntimeGenerationScope(RuntimeGenerationDomainModelCatalog),
	}
}

func GlobalRuntimeGenerationScope(domain string) RuntimeGenerationScope {
	return RuntimeGenerationScope{Domain: domain, ScopeType: runtimeGenerationScopeGlobal, ScopeID: runtimeGenerationGlobalID}.normalized()
}

func ProfileRuntimeGenerationScope(domain string, profileID int) RuntimeGenerationScope {
	return RuntimeGenerationScope{Domain: domain, ScopeType: runtimeGenerationScopeProfile, ScopeID: strconv.Itoa(profileID)}.normalized()
}

func RuntimeGenerationBumpsForRefresh(request RefreshRequest, reason string) []RuntimeGenerationBump {
	request = request.normalized()
	bumpSet := map[string]RuntimeGenerationBump{}
	add := func(scope RuntimeGenerationScope) {
		scope = scope.normalized()
		bumpSet[scope.key()] = RuntimeGenerationBump{Scope: scope, Reason: reason}
	}
	if request.Auth {
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainAuth))
	}
	if request.ActiveProfile {
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainProfileRuntime))
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainRuntimePlanning))
	}
	if request.PlanningAll {
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainRuntimePlanning))
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainProfileRuntime))
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainModelCatalog))
	}
	for _, profileID := range request.PlanningProfileIDs {
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainRuntimePlanning))
		add(GlobalRuntimeGenerationScope(RuntimeGenerationDomainProfileRuntime))
		add(ProfileRuntimeGenerationScope(RuntimeGenerationDomainRuntimePlanning, profileID))
		add(ProfileRuntimeGenerationScope(RuntimeGenerationDomainProfileRuntime, profileID))
	}
	bumps := make([]RuntimeGenerationBump, 0, len(bumpSet))
	for _, bump := range bumpSet {
		bumps = append(bumps, bump)
	}
	sort.Slice(bumps, func(i int, j int) bool { return bumps[i].Scope.key() < bumps[j].Scope.key() })
	return bumps
}

func AdvanceRuntimeCacheGenerations(ctx context.Context, tx pgx.Tx, bumps []RuntimeGenerationBump) error {
	if len(bumps) == 0 {
		return nil
	}
	for _, bump := range bumps {
		scope := bump.Scope.normalized()
		if scope.Domain == "" {
			continue
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO runtime_cache_generations (domain, scope_type, scope_id, version, updated_at, updated_by, reason)
			VALUES ($1, $2, $3, 1, $4, NULLIF($5, ''), NULLIF($6, ''))
			ON CONFLICT (domain, scope_type, scope_id) DO UPDATE
			SET version = runtime_cache_generations.version + 1,
			    updated_at = EXCLUDED.updated_at,
			    updated_by = EXCLUDED.updated_by,
			    reason = EXCLUDED.reason`,
			scope.Domain, scope.ScopeType, scope.ScopeID, time.Now().UTC(), strings.TrimSpace(bump.UpdatedBy), strings.TrimSpace(bump.Reason),
		)
		if err != nil {
			return fmt.Errorf("advance runtime cache generation %s: %w", scope.key(), err)
		}
	}
	return nil
}

func ReadRuntimeGenerationVector(ctx context.Context, tx pgx.Tx, scopes []RuntimeGenerationScope) (RuntimeGenerationVector, error) {
	scopes = normalizeRuntimeGenerationScopes(scopes)
	for _, scope := range scopes {
		_, err := tx.Exec(ctx, `
			INSERT INTO runtime_cache_generations (domain, scope_type, scope_id, version, reason)
			VALUES ($1, $2, $3, 0, 'lazy_init')
			ON CONFLICT (domain, scope_type, scope_id) DO NOTHING`, scope.Domain, scope.ScopeType, scope.ScopeID)
		if err != nil {
			return nil, fmt.Errorf("ensure runtime cache generation %s: %w", scope.key(), err)
		}
	}
	vector := make(RuntimeGenerationVector, len(scopes))
	for _, scope := range scopes {
		var version int64
		if err := tx.QueryRow(ctx, `
			SELECT version
			FROM runtime_cache_generations
			WHERE domain = $1 AND scope_type = $2 AND scope_id = $3`, scope.Domain, scope.ScopeType, scope.ScopeID).Scan(&version); err != nil {
			return nil, fmt.Errorf("read runtime cache generation %s: %w", scope.key(), err)
		}
		vector[scope.key()] = version
	}
	return vector, nil
}

func normalizeRuntimeGenerationScopes(scopes []RuntimeGenerationScope) []RuntimeGenerationScope {
	if len(scopes) == 0 {
		scopes = DefaultRuntimeGenerationScopes()
	}
	seen := map[string]RuntimeGenerationScope{}
	for _, scope := range scopes {
		scope = scope.normalized()
		if scope.Domain == "" {
			continue
		}
		seen[scope.key()] = scope
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	normalized := make([]RuntimeGenerationScope, 0, len(keys))
	for _, key := range keys {
		normalized = append(normalized, seen[key])
	}
	return normalized
}

func (scope RuntimeGenerationScope) normalized() RuntimeGenerationScope {
	scope.Domain = strings.ToLower(strings.TrimSpace(scope.Domain))
	scope.ScopeType = strings.ToLower(strings.TrimSpace(scope.ScopeType))
	scope.ScopeID = strings.TrimSpace(scope.ScopeID)
	if scope.ScopeType == "" {
		scope.ScopeType = runtimeGenerationScopeGlobal
	}
	if scope.ScopeID == "" {
		scope.ScopeID = runtimeGenerationGlobalID
	}
	return scope
}

func (scope RuntimeGenerationScope) key() string {
	scope = scope.normalized()
	return scope.Domain + ":" + scope.ScopeType + ":" + scope.ScopeID
}

func runtimeGenerationVectorsEqual(left RuntimeGenerationVector, right RuntimeGenerationVector, scopes []RuntimeGenerationScope) bool {
	for _, scope := range normalizeRuntimeGenerationScopes(scopes) {
		key := scope.key()
		leftVersion, leftOK := left[key]
		rightVersion, rightOK := right[key]
		if !leftOK || !rightOK || leftVersion != rightVersion {
			return false
		}
	}
	return true
}

func cloneRuntimeGenerationVector(source RuntimeGenerationVector) RuntimeGenerationVector {
	if len(source) == 0 {
		return RuntimeGenerationVector{}
	}
	cloned := make(RuntimeGenerationVector, len(source))
	maps.Copy(cloned, source)
	return cloned
}
