package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/logretention"
)

type LogPartitionEnsurer interface {
	EnsurePartitionForTime(context.Context, string, time.Time) error
}

type runtimeLogPartitionCache struct {
	ensurer LogPartitionEnsurer

	mu    sync.RWMutex
	known map[string]map[time.Time]struct{}
}

func newRuntimeLogPartitionCache(ensurer LogPartitionEnsurer, now func() time.Time, assumeHorizonPrepared bool) *runtimeLogPartitionCache {
	if ensurer == nil {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	cache := &runtimeLogPartitionCache{
		ensurer: ensurer,
		known:   make(map[string]map[time.Time]struct{}),
	}
	if assumeHorizonPrepared {
		cache.rememberHorizon(now())
	}
	return cache
}

func (cache *runtimeLogPartitionCache) EnsurePartitionForTime(ctx context.Context, tableName string, timestamp time.Time) error {
	if cache == nil || cache.ensurer == nil {
		return fmt.Errorf("log partition ensurer unavailable")
	}
	day := runtimeLogPartitionDay(timestamp)
	if cache.isKnown(tableName, day) {
		return nil
	}
	if err := cache.ensurer.EnsurePartitionForTime(ctx, tableName, day); err != nil {
		return fmt.Errorf("ensure %s partition for %s: %w", tableName, day.Format("2006-01-02"), err)
	}
	cache.remember(tableName, day)
	return nil
}

func (cache *runtimeLogPartitionCache) rememberHorizon(reference time.Time) {
	start := runtimeLogPartitionDay(reference)
	for _, tableName := range logretention.ManagedTables() {
		for offset := 0; offset < logretention.HorizonDays(); offset++ {
			cache.remember(tableName, start.AddDate(0, 0, offset))
		}
	}
}

func (cache *runtimeLogPartitionCache) isKnown(tableName string, day time.Time) bool {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	days, ok := cache.known[tableName]
	if !ok {
		return false
	}
	_, ok = days[day]
	return ok
}

func (cache *runtimeLogPartitionCache) remember(tableName string, day time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	days, ok := cache.known[tableName]
	if !ok {
		days = make(map[time.Time]struct{})
		cache.known[tableName] = days
	}
	days[day] = struct{}{}
}

func runtimeLogPartitionDay(timestamp time.Time) time.Time {
	utc := timestamp.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
