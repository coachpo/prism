package models

import (
	"context"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

// piCatalogForRead resolves the catalog a read-only management surface may
// publish: the full export source, the bounded directory search, and the
// single-model Pi read all answer through this one resolver. A successful
// fetch (including a 304 revalidation) is reported as fresh. A failed fetch
// may still answer from last-known-good, but only ever labelled stale: stale
// evidence is display material; bind/refresh still fetch fresh to write.
func (s *Service) piCatalogForRead(ctx context.Context) (*pidev.Catalog, string) {
	if s.piCatalog == nil {
		return nil, "unavailable"
	}
	// Fetch owns singleflight, ETag revalidation, and timeout handling; callers
	// invoke this resolver before opening any database transaction.
	cat, err := s.piCatalog.Fetch(ctx)
	if err == nil {
		return cat, "fresh"
	}
	if snap := s.piCatalog.Snapshot(); snap != nil {
		return snap, "stale"
	}
	return nil, "unavailable"
}
