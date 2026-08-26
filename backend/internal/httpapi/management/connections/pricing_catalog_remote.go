package connections

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

func (s *Service) requireCatalogClient(w http.ResponseWriter, r *http.Request) bool {
	if s.catalog == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "models_dev_catalog_client_missing: catalog client is not configured")
		return false
	}
	return true
}

func isCatalogUnavailableErr(err error) bool {
	return errors.Is(err, modelsdev.ErrCatalogUnavailable)
}

func catalogFetchDomainError(err error) error {
	return &domainError{
		StatusCode: http.StatusBadGateway,
		Detail:     fmt.Sprintf("models_dev_catalog_unavailable: %v", err),
	}
}

func catalogStaleDomainError(expected, current string) error {
	return &domainError{
		StatusCode: http.StatusConflict,
		Detail:     "models_dev_catalog_stale: the previewed catalog revision no longer matches current data",
		Fields: map[string]any{
			"expected_catalog_revision": expected,
			"current_catalog_revision":  current,
		},
	}
}

// fetchCatalogOutsideTx performs the network round trip strictly before any
// database transaction begins.
func (s *Service) fetchCatalogOutsideTx(ctx context.Context) (*modelsdev.Catalog, error) {
	catalog, err := s.catalog.Fetch(ctx)
	if err != nil {
		if isCatalogUnavailableErr(err) {
			return nil, catalogFetchDomainError(err)
		}
		return nil, err
	}
	return catalog, nil
}
