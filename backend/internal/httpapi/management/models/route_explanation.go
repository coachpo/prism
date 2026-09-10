package models

import (
	"context"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"net/http"
)

func (s *Service) SetRouteExplainer(explainer interface {
	ExplainRoute(context.Context, int, string) (runtime.RouteExplanation, error)
}) { s.routeExplainer = explainer }
func (s *Service) handleRouteExplanation(w http.ResponseWriter, r *http.Request) {
	id, err := routeInt(r, "model_config_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if s.routeExplainer == nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusServiceUnavailable, "Runtime observation unavailable")
		return
	}
	result, err := s.routeExplainer.ExplainRoute(r.Context(), id, r.URL.Query().Get("operation"))
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusUnprocessableEntity, err.Error())
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, result)
}
