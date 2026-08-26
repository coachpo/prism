package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

func isCatalogUnavailable(err error) bool {
	return errors.Is(err, modelsdev.ErrCatalogUnavailable)
}

func catalogFetchFailed(err error) error {
	return &domainError{
		StatusCode: http.StatusBadGateway,
		Detail:     fmt.Sprintf("models_dev_catalog_unavailable: %v", err),
	}
}

func catalogStaleError(expected, current string) error {
	return &domainError{
		StatusCode: http.StatusConflict,
		Detail:     "models_dev_catalog_stale: the previewed catalog revision no longer matches current data",
		Fields: map[string]any{
			"expected_catalog_revision": expected,
			"current_catalog_revision":  current,
		},
	}
}

// fetchValidatedCatalog performs the network round trip strictly outside any
// database transaction and fails closed on transport or schema problems.
func (s *Service) fetchValidatedCatalog(ctx context.Context) (*modelsdev.Catalog, error) {
	catalog, err := s.catalog.Fetch(ctx)
	if err != nil {
		if isCatalogUnavailable(err) {
			return nil, catalogFetchFailed(err)
		}
		return nil, err
	}
	return catalog, nil
}

// currentCatalog returns the cached snapshot, fetching lazily when the process
// has not loaded one yet. The fetch stays outside database transactions.
func (s *Service) currentCatalog(ctx context.Context) (*modelsdev.Catalog, error) {
	if snapshot := s.catalog.Snapshot(); snapshot != nil {
		return snapshot, nil
	}
	return s.fetchValidatedCatalog(ctx)
}
