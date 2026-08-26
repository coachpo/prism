package models

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

func routeIntOrBadRequest(w http.ResponseWriter, r *http.Request, cors platformcors.Snapshot) (int, bool) {
	modelConfigID, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, cors, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return modelConfigID, true
}

func newCatalogDomainError(statusCode int, detail string, fields map[string]any) error {
	return &domainError{StatusCode: statusCode, Detail: detail, Fields: fields}
}

func (s *Service) writeCatalogDomainError(w http.ResponseWriter, r *http.Request, err error) {
	writeDomainError(w, r, s.corsSnapshot(), err)
}

func (s *Service) requireCatalogClient(w http.ResponseWriter, r *http.Request) bool {
	if s.catalog == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "models_dev_catalog_client_missing: catalog client is not configured")
		return false
	}
	return true
}
