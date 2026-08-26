package connections

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const ownerScopedConnectionMutationDetail = "terminal target mutations must use owner-scoped routes under /api/models/{model_config_id}/connections"

func (s *Service) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) handleRejectModelConnectionLegacyMutation(w http.ResponseWriter, r *http.Request) {
	s.writeConnectionMutationRouteError(w, r)
}

func (s *Service) writeConnectionMutationRouteError(w http.ResponseWriter, r *http.Request) {
	responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, ownerScopedConnectionMutationDetail)
}

func decodeJSONBody(request *http.Request, target any) error {
	defer func() { _ = request.Body.Close() }()
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return responseutil.SanitizeDecodeError(decoder.Decode(target))
}

func decodeJSONRawBody(request *http.Request) ([]byte, error) {
	defer func() { _ = request.Body.Close() }()
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	if detail, ok := pricingTemplateGuardError(err); ok {
		responseutil.WriteError(w, r, corsSnapshot, http.StatusUnprocessableEntity, detail)
		return
	}
	var connectionErr *DomainError
	if errors.As(err, &connectionErr) {
		responseutil.WriteErrorFields(w, r, corsSnapshot, connectionErr.StatusCode, connectionErr.Detail, connectionErr.Fields)
		return
	}
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
	fmt.Fprintf(os.Stderr, "connections writeDomainError unhandled: %v\n", err)
}

func pricingTemplateGuardError(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	if strings.HasPrefix(pgErr.Message, "pricing template shape guard:") || pgErr.Message == "pricing template child rows are append-only" {
		return pgErr.Message, true
	}
	return "", false
}

func routeInt(request *http.Request, name string) (int, error) {
	value := strings.TrimSpace(chi.URLParam(request, name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}
