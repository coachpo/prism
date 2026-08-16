package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/background"
	"github.com/coachpo/prism/backend/internal/profiledomain"
)

var ErrPublishedRuntimeSnapshotUnavailable = errors.New("published runtime snapshot unavailable")
var ErrRuntimeSnapshotRefreshRequired = errors.New("runtime snapshot refresh required")

const defaultSharedCacheRefreshTimeout = 30 * time.Second

type RefreshRequest struct {
	Auth               bool
	ActiveProfile      bool
	PlanningAll        bool
	PlanningProfileIDs []int
}

type SharedCacheOptions struct {
	RefreshPool         *pgxpool.Pool
	SecretEncryptionKey string
	Now                 func() time.Time
	BeforePublish       func(RefreshRequest) error
	Scheduler           *background.Scheduler
}

type SharedCache struct {
	refreshPool         *pgxpool.Pool
	secretEncryptionKey string
	now                 func() time.Time
	beforePublish       func(RefreshRequest) error

	published atomic.Pointer[publishedRuntimeSnapshot]

	schedulerMu sync.Mutex
	pending     RefreshRequest
	scheduler   *background.Scheduler

	refreshMu sync.Mutex
}

type RuntimeAuthSettingsSnapshot struct {
	AuthEnabled bool
}

type RuntimeProxyKeyRecord struct {
	KeyID     int
	KeyName   string
	KeyHash   string
	ExpiresAt *time.Time
}

type publishedRuntimeSnapshot struct {
	Generation          uint64
	PublishedAt         time.Time
	GenerationVector    RuntimeGenerationVector
	ActiveProfile       profiledomain.Profile
	PlanningByProfileID map[int]*planningSnapshot
	Auth                publishedRuntimeAuthSnapshot
}

type publishedRuntimeAuthSnapshot struct {
	Settings          RuntimeAuthSettingsSnapshot
	ProxyKeysByPrefix map[string]RuntimeProxyKeyRecord
}

func NewSharedCache() *SharedCache {
	return NewSharedCacheWithOptions(SharedCacheOptions{})
}

func NewSharedCacheWithOptions(options SharedCacheOptions) *SharedCache {
	cache := &SharedCache{}
	cache.Configure(options)
	return cache
}

func (c *SharedCache) Configure(options SharedCacheOptions) {
	if c == nil {
		return
	}
	if options.RefreshPool != nil {
		c.refreshPool = options.RefreshPool
	}
	if trimmedSecretKey := strings.TrimSpace(options.SecretEncryptionKey); trimmedSecretKey != "" {
		c.secretEncryptionKey = trimmedSecretKey
	}
	if options.Now != nil {
		c.now = options.Now
	}
	if options.BeforePublish != nil {
		c.beforePublish = options.BeforePublish
	}
	if options.Scheduler != nil {
		c.scheduler = options.Scheduler
	}
	if c.now == nil {
		c.now = time.Now
	}
}

func (c *SharedCache) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if c == nil || scheduler == nil {
		return nil
	}
	c.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             background.WorkerName("runtime_shared_cache_refresh"),
		Priority:         background.PriorityNormalBackground,
		MaxPriority:      background.PriorityNormalBackground,
		QueueLimit:       64,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainFinishRunning,
		CoalescePolicy:   background.CoalesceMerge,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: 250 * time.Millisecond},
		Timeout:          defaultSharedCacheRefreshTimeout,
		Merge: func(existing any, incoming any) any {
			return mergeRefreshRequests(refreshRequestPayload(existing), refreshRequestPayload(incoming))
		},
	}, c.handleScheduledRefresh)
}

func (c *SharedCache) Bootstrap(ctx context.Context) error {
	return c.RefreshNow(ctx, RefreshRequest{Auth: true, ActiveProfile: true, PlanningProfileIDs: []int{profiledomain.DefaultProfileID}})
}

func (c *SharedCache) RefreshNow(ctx context.Context, request RefreshRequest) error {
	if c == nil {
		return ErrPublishedRuntimeSnapshotUnavailable
	}
	if c.now == nil {
		c.now = time.Now
	}
	request = request.normalized()
	if c.published.Load() == nil {
		request = mergeRefreshRequests(request, RefreshRequest{Auth: true, ActiveProfile: true, PlanningProfileIDs: []int{profiledomain.DefaultProfileID}})
	}
	if request.isEmpty() {
		return nil
	}

	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	next, err := c.buildPublishedSnapshot(ctx, request)
	if errors.Is(err, ErrRuntimeSnapshotGenerationChanged) {
		next, err = c.buildPublishedSnapshot(ctx, request)
	}
	if err != nil {
		return err
	}
	if c.beforePublish != nil {
		if err := c.beforePublish(request); err != nil {
			return err
		}
	}
	c.published.Store(next)
	return nil
}

func (c *SharedCache) ScheduleRefresh(request RefreshRequest) {
	if c == nil {
		return
	}
	request = request.normalized()
	if request.isEmpty() {
		return
	}
	c.schedulerMu.Lock()
	c.pending = mergeRefreshRequests(c.pending, request)
	pending := c.pending
	c.schedulerMu.Unlock()
	if c.scheduler == nil {
		ctx, cancel := context.WithTimeout(context.Background(), defaultSharedCacheRefreshTimeout)
		defer cancel()
		if err := c.RefreshNow(ctx, pending); err != nil {
			slog.Error("failed to refresh published runtime snapshot", "error", err)
		}
		c.schedulerMu.Lock()
		c.pending = RefreshRequest{}
		c.schedulerMu.Unlock()
		return
	}
	_ = c.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("runtime_shared_cache_refresh"), Payload: pending, CoalesceKey: "runtime_shared_cache_refresh"})
}

func (c *SharedCache) InvalidateActiveProfile() {
	c.ScheduleRefresh(RefreshRequest{ActiveProfile: true})
}

func (c *SharedCache) InvalidatePlanningProfile(profileID int) {
	c.ScheduleRefresh(RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func (c *SharedCache) InvalidateAllPlanning() {
	c.ScheduleRefresh(RefreshRequest{PlanningProfileIDs: []int{profiledomain.DefaultProfileID}})
}

func (c *SharedCache) PublishedGeneration() uint64 {
	if c == nil {
		return 0
	}
	snapshot := c.published.Load()
	if snapshot == nil {
		return 0
	}
	return snapshot.Generation
}

func (c *SharedCache) PublishedReady() bool {
	return c != nil && c.published.Load() != nil
}

func (c *SharedCache) LoadPublishedDefaultProfile() (profiledomain.Profile, error) {
	var zero profiledomain.Profile
	snapshot, err := c.requirePublishedSnapshot()
	if err != nil {
		return zero, err
	}
	return cloneProfile(snapshot.ActiveProfile), nil
}

func (c *SharedCache) LoadFreshDefaultRuntimePlan(ctx context.Context) (profiledomain.Profile, *planningSnapshot, error) {
	defaultProfile, planning, _, err := c.loadFreshDefaultRuntimePlanWithGenerationToken(ctx)
	return defaultProfile, planning, err
}

func (c *SharedCache) loadFreshDefaultRuntimePlanWithGenerationToken(ctx context.Context) (profiledomain.Profile, *planningSnapshot, string, error) {
	var zero profiledomain.Profile
	snapshot, err := c.requireFreshPublishedSnapshot(ctx, RefreshRequest{ActiveProfile: true, PlanningProfileIDs: []int{profiledomain.DefaultProfileID}})
	if err != nil {
		return zero, nil, "", err
	}
	defaultProfile := cloneProfile(snapshot.ActiveProfile)
	planning, ok := snapshot.PlanningByProfileID[defaultProfile.ID]
	if !ok || planning == nil {
		return zero, nil, "", fmt.Errorf("%w: planning snapshot missing for profile %d", ErrPublishedRuntimeSnapshotUnavailable, defaultProfile.ID)
	}
	return defaultProfile, planning, runtimeGenerationVectorToken(snapshot.GenerationVector), nil
}

func (c *SharedCache) LoadPublishedPlanningSnapshot(profileID int) (*planningSnapshot, error) {
	snapshot, err := c.requirePublishedSnapshot()
	if err != nil {
		return nil, err
	}
	planning, ok := snapshot.PlanningByProfileID[profileID]
	if !ok {
		return nil, fmt.Errorf("%w: planning snapshot missing for profile %d", ErrPublishedRuntimeSnapshotUnavailable, profileID)
	}
	return planning, nil
}

func (c *SharedCache) LoadRuntimeAuthSettings() (RuntimeAuthSettingsSnapshot, error) {
	snapshot, err := c.requirePublishedSnapshot()
	if err != nil {
		return RuntimeAuthSettingsSnapshot{}, err
	}
	return snapshot.Auth.Settings, nil
}

func (c *SharedCache) LoadFreshRuntimeAuthSettings(ctx context.Context) (RuntimeAuthSettingsSnapshot, error) {
	snapshot, err := c.requireFreshPublishedSnapshot(ctx, RefreshRequest{Auth: true})
	if err != nil {
		return RuntimeAuthSettingsSnapshot{}, err
	}
	return snapshot.Auth.Settings, nil
}

func (c *SharedCache) LoadRuntimeProxyKeyRecord(keyPrefix string) (RuntimeProxyKeyRecord, bool, error) {
	snapshot, err := c.requirePublishedSnapshot()
	if err != nil {
		return RuntimeProxyKeyRecord{}, false, err
	}
	record, ok := snapshot.Auth.ProxyKeysByPrefix[keyPrefix]
	if !ok {
		return RuntimeProxyKeyRecord{}, false, nil
	}
	return cloneRuntimeProxyKeyRecord(record), true, nil
}

func (c *SharedCache) LoadFreshRuntimeProxyKeyRecord(ctx context.Context, keyPrefix string) (RuntimeProxyKeyRecord, bool, error) {
	snapshot, err := c.requireFreshPublishedSnapshot(ctx, RefreshRequest{Auth: true})
	if err != nil {
		return RuntimeProxyKeyRecord{}, false, err
	}
	record, ok := snapshot.Auth.ProxyKeysByPrefix[keyPrefix]
	if !ok {
		return RuntimeProxyKeyRecord{}, false, nil
	}
	return cloneRuntimeProxyKeyRecord(record), true, nil
}

func (c *SharedCache) requirePublishedSnapshot() (*publishedRuntimeSnapshot, error) {
	if c == nil {
		return nil, ErrPublishedRuntimeSnapshotUnavailable
	}
	snapshot := c.published.Load()
	if snapshot == nil {
		return nil, ErrPublishedRuntimeSnapshotUnavailable
	}
	return snapshot, nil
}

func (c *SharedCache) requireFreshPublishedSnapshot(ctx context.Context, refresh RefreshRequest) (*publishedRuntimeSnapshot, error) {
	if c == nil {
		return nil, ErrPublishedRuntimeSnapshotUnavailable
	}
	if c.refreshPool == nil {
		snapshot := c.published.Load()
		if snapshot != nil {
			return snapshot, nil
		}
	}
	currentVector, err := c.readCurrentGenerationVector(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPublishedRuntimeSnapshotUnavailable, err)
	}
	snapshot := c.published.Load()
	if snapshot != nil && runtimeGenerationVectorsEqual(snapshot.GenerationVector, currentVector, DefaultRuntimeGenerationScopes()) {
		return snapshot, nil
	}
	if err := c.RefreshNow(ctx, refresh); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuntimeSnapshotRefreshRequired, err)
	}
	refreshed := c.published.Load()
	if refreshed == nil {
		log.Printf("requireFreshPublishedSnapshot: published nil after RefreshNow (err=%v)", err)
		return nil, ErrPublishedRuntimeSnapshotUnavailable
	}
	currentVector, err = c.readCurrentGenerationVector(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPublishedRuntimeSnapshotUnavailable, err)
	}
	if !runtimeGenerationVectorsEqual(refreshed.GenerationVector, currentVector, DefaultRuntimeGenerationScopes()) {
		return nil, ErrRuntimeSnapshotRefreshRequired
	}
	return refreshed, nil
}

func refreshPublishedPlanningSnapshots(ctx context.Context, tx pgx.Tx, current *publishedRuntimeSnapshot, request RefreshRequest, secretEncryptionKey string) (map[int]*planningSnapshot, error) {
	planningByProfileID := copyPublishedPlanningSnapshots(current)
	if current == nil || request.PlanningAll || len(request.PlanningProfileIDs) > 0 {
		profileIDs, err := listPublishedPlanningProfileIDs(ctx, tx)
		if err != nil {
			return nil, err
		}
		if planningByProfileID == nil {
			planningByProfileID = make(map[int]*planningSnapshot, len(profileIDs))
		}
		for _, profileID := range profileIDs {
			snapshot, err := buildPlanningSnapshot(ctx, tx, profileID, secretEncryptionKey)
			if err != nil {
				return nil, err
			}
			planningByProfileID[profileID] = snapshot
		}
	}
	return planningByProfileID, nil
}

func (c *SharedCache) handleScheduledRefresh(ctx context.Context, job background.Job) background.JobResult {
	c.schedulerMu.Lock()
	request := mergeRefreshRequests(c.pending, refreshRequestPayload(job.Payload))
	c.pending = RefreshRequest{}
	c.schedulerMu.Unlock()
	if request.isEmpty() {
		return background.JobResult{Status: background.JobSucceeded}
	}
	if err := c.RefreshNow(ctx, request); err != nil {
		slog.Error("failed to refresh published runtime snapshot", "error", err, "auth", request.Auth, "active_profile", request.ActiveProfile, "planning_all", request.PlanningAll, "planning_profile_ids", request.PlanningProfileIDs)
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func refreshRequestPayload(payload any) RefreshRequest {
	request, _ := payload.(RefreshRequest)
	return request.normalized()
}

func (c *SharedCache) buildPublishedSnapshot(ctx context.Context, request RefreshRequest) (*publishedRuntimeSnapshot, error) {
	if c.refreshPool == nil {
		return nil, fmt.Errorf("published runtime snapshot refresh pool is not configured")
	}
	return pgxutil.InTxValue(ctx, c.refreshPool, "runtime_snapshot", func(tx pgx.Tx) (*publishedRuntimeSnapshot, error) {
		current := c.published.Load()
		request = request.normalized()
		if current == nil {
			request = mergeRefreshRequests(request, RefreshRequest{Auth: true, ActiveProfile: true, PlanningProfileIDs: []int{profiledomain.DefaultProfileID}})
		}
		if !request.isEmpty() {
			request = RefreshRequest{Auth: true, ActiveProfile: true, PlanningProfileIDs: []int{profiledomain.DefaultProfileID}}
		}
		beforeVector, err := ReadRuntimeGenerationVector(ctx, tx, DefaultRuntimeGenerationScopes())
		if err != nil {
			return nil, err
		}

		next := &publishedRuntimeSnapshot{PublishedAt: c.nowUTC(), GenerationVector: cloneRuntimeGenerationVector(beforeVector)}
		if current != nil {
			next.Generation = current.Generation + 1
		} else {
			next.Generation = 1
		}

		// ponytail: runtime planning stays frozen on Default profile id=1.
		defaultProfile, found, err := profiledomain.LoadNonDeletedProfile(ctx, tx, profiledomain.DefaultProfileID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: default profile %d not found", ErrPublishedRuntimeSnapshotUnavailable, profiledomain.DefaultProfileID)
		}
		next.ActiveProfile = cloneProfile(defaultProfile)

		planningByProfileID, err := refreshPublishedPlanningSnapshots(ctx, tx, current, request, c.secretEncryptionKey)
		if err != nil {
			return nil, err
		}
		if planningByProfileID == nil {
			planningByProfileID = map[int]*planningSnapshot{}
		}
		if _, ok := planningByProfileID[defaultProfile.ID]; !ok {
			snapshot, err := buildPlanningSnapshot(ctx, tx, defaultProfile.ID, c.secretEncryptionKey)
			if err != nil {
				return nil, err
			}
			planningByProfileID[defaultProfile.ID] = snapshot
		}
		next.PlanningByProfileID = planningByProfileID

		authSnapshot := publishedRuntimeAuthSnapshot{}
		if current != nil {
			authSnapshot = current.Auth
		}
		if current == nil || request.Auth {
			builtAuthSnapshot, err := buildPublishedRuntimeAuthSnapshot(ctx, tx, c.nowUTC())
			if err != nil {
				return nil, err
			}
			authSnapshot = builtAuthSnapshot
		}
		next.Auth = authSnapshot

		afterVector, err := ReadRuntimeGenerationVector(ctx, tx, DefaultRuntimeGenerationScopes())
		if err != nil {
			return nil, err
		}
		if !runtimeGenerationVectorsEqual(beforeVector, afterVector, DefaultRuntimeGenerationScopes()) {
			return nil, ErrRuntimeSnapshotGenerationChanged
		}
		next.GenerationVector = cloneRuntimeGenerationVector(afterVector)
		return next, nil
	})
}

func (c *SharedCache) readCurrentGenerationVector(ctx context.Context) (RuntimeGenerationVector, error) {
	if c == nil || c.refreshPool == nil {
		return nil, fmt.Errorf("published runtime snapshot refresh pool is not configured")
	}
	return pgxutil.InTxValue(ctx, c.refreshPool, "runtime_generation_read", func(tx pgx.Tx) (RuntimeGenerationVector, error) {
		return ReadRuntimeGenerationVector(ctx, tx, DefaultRuntimeGenerationScopes())
	})
}

func (c *SharedCache) nowUTC() time.Time {
	if c.now == nil {
		c.now = time.Now
	}
	return c.now().UTC()
}

func (r RefreshRequest) normalized() RefreshRequest {
	normalized := RefreshRequest{
		Auth:          r.Auth,
		ActiveProfile: r.ActiveProfile,
		PlanningAll:   r.PlanningAll,
	}
	if normalized.PlanningAll {
		return normalized
	}
	seen := map[int]struct{}{}
	for _, profileID := range r.PlanningProfileIDs {
		if profileID <= 0 {
			continue
		}
		if _, ok := seen[profileID]; ok {
			continue
		}
		seen[profileID] = struct{}{}
		normalized.PlanningProfileIDs = append(normalized.PlanningProfileIDs, profileID)
	}
	sort.Ints(normalized.PlanningProfileIDs)
	return normalized
}

func (r RefreshRequest) HasWork() bool {
	return !r.normalized().isEmpty()
}

func (r RefreshRequest) isEmpty() bool {
	return !r.Auth && !r.ActiveProfile && !r.PlanningAll && len(r.PlanningProfileIDs) == 0
}

func mergeRefreshRequests(current RefreshRequest, next RefreshRequest) RefreshRequest {
	current = current.normalized()
	next = next.normalized()
	merged := RefreshRequest{
		Auth:          current.Auth || next.Auth,
		ActiveProfile: current.ActiveProfile || next.ActiveProfile,
		PlanningAll:   current.PlanningAll || next.PlanningAll,
	}
	if merged.PlanningAll {
		return merged
	}
	merged.PlanningProfileIDs = append(merged.PlanningProfileIDs, current.PlanningProfileIDs...)
	merged.PlanningProfileIDs = append(merged.PlanningProfileIDs, next.PlanningProfileIDs...)
	return merged.normalized()
}

func buildPublishedRuntimeAuthSnapshot(ctx context.Context, tx pgx.Tx, referenceNow time.Time) (publishedRuntimeAuthSnapshot, error) {
	settings, err := loadPublishedRuntimeAuthSettings(ctx, tx)
	if err != nil {
		return publishedRuntimeAuthSnapshot{}, err
	}
	proxyKeysByPrefix, err := listPublishedRuntimeProxyKeys(ctx, tx, referenceNow)
	if err != nil {
		return publishedRuntimeAuthSnapshot{}, err
	}
	return publishedRuntimeAuthSnapshot{
		Settings:          settings,
		ProxyKeysByPrefix: proxyKeysByPrefix,
	}, nil
}

func loadPublishedRuntimeAuthSettings(ctx context.Context, tx pgx.Tx) (RuntimeAuthSettingsSnapshot, error) {
	// Schema-aware read (Settings SPEC §14.1 item 9): post-000015 the runtime
	// enforcement mode comes from the immutable effective config version; the
	// finalizer drops the transitional in-place columns afterwards.
	var pointerSchema bool
	if err := tx.QueryRow(ctx, `SELECT to_regclass('public.auth_config_versions') IS NOT NULL`).Scan(&pointerSchema); err != nil {
		return RuntimeAuthSettingsSnapshot{}, fmt.Errorf("check auth config versions table: %w", err)
	}
	if pointerSchema {
		var authEnabled bool
		err := tx.QueryRow(
			ctx,
			`SELECT COALESCE(v.desired_mode = 'enabled', FALSE)
			FROM app_auth_settings AS a
			LEFT JOIN auth_config_versions AS v ON v.id = a.effective_config_version_id
			WHERE a.singleton_key = $1 ORDER BY a.id ASC LIMIT 1`,
			"app",
		).Scan(&authEnabled)
		if err == nil {
			return RuntimeAuthSettingsSnapshot{AuthEnabled: authEnabled}, nil
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return RuntimeAuthSettingsSnapshot{}, nil
		}
		return RuntimeAuthSettingsSnapshot{}, fmt.Errorf("load published runtime auth settings: %w", err)
	}
	var authEnabled bool
	err := tx.QueryRow(
		ctx,
		`SELECT auth_enabled FROM app_auth_settings WHERE singleton_key = $1 ORDER BY id ASC LIMIT 1`,
		"app",
	).Scan(&authEnabled)
	if err == nil {
		return RuntimeAuthSettingsSnapshot{AuthEnabled: authEnabled}, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeAuthSettingsSnapshot{}, nil
	}
	return RuntimeAuthSettingsSnapshot{}, fmt.Errorf("load published runtime auth settings: %w", err)
}

func listPublishedRuntimeProxyKeys(ctx context.Context, tx pgx.Tx, referenceNow time.Time) (map[string]RuntimeProxyKeyRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id, name, key_prefix, key_hash, is_active, expires_at
        FROM proxy_api_keys
        ORDER BY id ASC`,
	)

	if err != nil {
		return nil, fmt.Errorf("query published runtime proxy keys: %w", err)
	}
	defer rows.Close()

	items := map[string]RuntimeProxyKeyRecord{}
	for rows.Next() {
		var (
			id        int
			name      string
			keyPrefix string
			keyHash   string
			isActive  bool
			expiresAt sql.NullTime
		)
		if err := rows.Scan(&id, &name, &keyPrefix, &keyHash, &isActive, &expiresAt); err != nil {
			return nil, fmt.Errorf("scan published runtime proxy key: %w", err)
		}
		if !isActive {
			continue
		}
		if expiresAt.Valid && !expiresAt.Time.UTC().After(referenceNow.UTC()) {
			continue
		}
		record := RuntimeProxyKeyRecord{
			KeyID:   id,
			KeyName: name,
			KeyHash: keyHash,
		}
		if expiresAt.Valid {
			resolvedExpiresAt := expiresAt.Time.UTC()
			record.ExpiresAt = &resolvedExpiresAt
		}
		items[keyPrefix] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published runtime proxy keys: %w", err)
	}
	return items, nil
}

func copyPublishedPlanningSnapshots(snapshot *publishedRuntimeSnapshot) map[int]*planningSnapshot {
	if snapshot == nil || len(snapshot.PlanningByProfileID) == 0 {
		return nil
	}
	cloned := make(map[int]*planningSnapshot, len(snapshot.PlanningByProfileID))
	maps.Copy(cloned, snapshot.PlanningByProfileID)
	return cloned
}

func cloneRuntimeProxyKeyRecord(record RuntimeProxyKeyRecord) RuntimeProxyKeyRecord {
	cloned := RuntimeProxyKeyRecord{
		KeyID:   record.KeyID,
		KeyName: record.KeyName,
		KeyHash: record.KeyHash,
	}
	if record.ExpiresAt != nil {
		expiresAt := record.ExpiresAt.UTC()
		cloned.ExpiresAt = &expiresAt
	}
	return cloned
}

func cloneProfile(profile profiledomain.Profile) profiledomain.Profile {
	cloned := profile
	if profile.Description != nil {
		description := *profile.Description
		cloned.Description = &description
	}
	if profile.DeletedAt != nil {
		deletedAt := profile.DeletedAt.UTC()
		cloned.DeletedAt = &deletedAt
	}
	return cloned
}
